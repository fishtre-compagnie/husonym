package accessgate

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	// assertionHeader is where Access puts the token of the request it let through.
	assertionHeader = "Cf-Access-Jwt-Assertion"
	emailClaim      = "email"
	// clockSkew is how far the clock of the issuer and ours may differ.
	clockSkew = 30 * time.Second

	reasonMissing = "missing"
	reasonInvalid = "invalid"
)

// Config says which Access application guards the backoffice. Every field is required.
type Config struct {
	// TeamDomain is the domain of the Access team, a bare host: the issuer is https://<TeamDomain>.
	TeamDomain string
	// Audience is the tag of the Access application of the backoffice.
	Audience string
	// Now is the clock the validity of a token is judged against.
	Now func() time.Time
	// Logger receives the line of a refused request and of a failed refresh of the keys.
	Logger *slog.Logger
}

// Gate lets a request through only when it carries a token Access issued for this application.
//
// Reaching the origin proves nothing: an ingress rule that drifts, or a route to the origin that
// does not go through Access, would otherwise hand out the console. And the keys of a team sign
// the tokens of all its applications, so a sound signature proves nothing either without the
// audience: it is the audience that keeps out a token issued for a neighboring application.
type Gate struct {
	issuer   string
	audience string
	keys     keySource
	now      func() time.Time
	logger   *slog.Logger
}

// New returns the gate of an Access application, holding the keys of its team. They are fetched
// here, and an error is returned when they cannot be: a server must not start behind a gate that
// holds no key. They are then refreshed in the background until ctx ends, and a refresh that
// fails keeps the last keys.
//
// The team domain is trimmed and lower-cased, the audience trimmed. A team domain that is not a
// bare host name is refused, as is a config that lacks a field.
func New(ctx context.Context, cfg Config) (*Gate, error) {
	checked, err := cfg.checked()
	if err != nil {
		return nil, err
	}
	return newRemote(ctx, checked, &http.Client{}, refreshInterval)
}

// checked gives the config as the gate uses it, or says what it lacks. No value is echoed: the
// error of a start ends in a log.
func (c Config) checked() (Config, error) {
	if c.Now == nil {
		return Config{}, errors.New("the Access gate needs a clock")
	}
	if c.Logger == nil {
		return Config{}, errors.New("the Access gate needs a logger")
	}
	c.Audience = strings.TrimSpace(c.Audience)
	if c.Audience == "" {
		return Config{}, errors.New("the Access gate needs the audience of its application")
	}
	c.TeamDomain = strings.ToLower(strings.TrimSpace(c.TeamDomain))
	if !bareHost(c.TeamDomain) {
		return Config{}, errors.New(
			"the Access gate needs the domain of its team as a bare host name: no scheme, no port, no path")
	}
	return c, nil
}

// newRemote starts a gate from a config already checked. The client is the one the keys are
// fetched with, see newKeyClient for what is changed of it.
func newRemote(ctx context.Context, cfg Config, client *http.Client, interval time.Duration) (*Gate, error) {
	issuer := "https://" + cfg.TeamDomain
	keys := &remoteKeys{
		url:     issuer + certsPath,
		client:  newKeyClient(client),
		now:     cfg.Now,
		logger:  cfg.Logger,
		stopped: make(chan struct{}),
	}
	if err := keys.fetch(ctx); err != nil {
		return nil, err
	}
	go keys.refreshEvery(ctx, interval)

	return &Gate{issuer: issuer, audience: cfg.Audience, keys: keys, now: cfg.Now, logger: cfg.Logger}, nil
}

// NewWithKeys returns a gate over a set of keys that never changes. It is for tests.
func NewWithKeys(issuer, audience string, keys jwk.Set, now func() time.Time, logger *slog.Logger) *Gate {
	return &Gate{issuer: issuer, audience: audience, keys: fixedKeys{set: keys}, now: now, logger: logger}
}

// Wrap puts the gate in front of next. A request passes when its token is signed by a key of the
// team with RS256, names the issuer and, among its audiences, the application, is within its
// validity on the clock of the gate, and says who the operator is. The operator is then in the
// context of the request next receives, see Operator.
//
// Any other request is answered 401 with no body, and one line is logged in fixed words: never
// the token, a claim, the path or an address.
func (g *Gate) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(assertionHeader)
		if len(values) == 0 || (len(values) == 1 && values[0] == "") {
			g.refuse(w, reasonMissing)
			return
		}
		// Access sets the header once. Several are not its doing, and choosing one of them
		// would be guessing.
		if len(values) > 1 {
			g.refuse(w, reasonInvalid)
			return
		}
		email, ok := g.operator(r.Context(), values[0])
		if !ok {
			g.refuse(w, reasonInvalid)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), operatorKey{}, email)))
	})
}

// operator checks a token and gives the email it was issued to. Why a token fails is not told
// apart: the text of the error may quote the token.
func (g *Gate) operator(ctx context.Context, token string) (string, bool) {
	parsed, err := jwt.Parse([]byte(token),
		jwt.WithKeyProvider(g.keyProvider(ctx)),
		jwt.WithValidate(true),
		jwt.WithClock(jwt.ClockFunc(g.now)),
		jwt.WithAcceptableSkew(clockSkew),
		jwt.WithIssuer(g.issuer),
		jwt.WithAudience(g.audience),
		// A token that never expires is not one Access issues.
		jwt.WithRequiredClaim(jwt.ExpirationKey),
	)
	if err != nil {
		return "", false
	}
	// A service token has no email: the console is for people.
	var email string
	if err := parsed.Get(emailClaim, &email); err != nil || strings.TrimSpace(email) == "" {
		return "", false
	}
	return email, true
}

// keyProvider gives the verification the one key a signature may be checked with. The algorithm
// is decided here, not by the token: only a signature that says RS256 gets a key, and the key is
// used as an RS256 key whatever else it could do. A token that says "none", or HS256 hoping the
// public key is taken as a shared secret, gets no key and so cannot verify.
func (g *Gate) keyProvider(ctx context.Context) jws.KeyProvider {
	return jws.KeyProviderFunc(func(_ context.Context, sink jws.KeySink, sig *jws.Signature, _ *jws.Message) error {
		headers := sig.ProtectedHeaders()
		if headers == nil {
			return errors.New("no protected header")
		}
		if alg, ok := headers.Algorithm(); !ok || alg != jwa.RS256() {
			return errors.New("the algorithm is not RS256")
		}
		kid, ok := headers.KeyID()
		if !ok || kid == "" {
			return errors.New("no key id")
		}
		key, ok := g.keys.lookup(ctx, kid)
		if !ok {
			return errors.New("unknown key id")
		}
		if key.KeyType() != jwa.RSA() {
			return errors.New("the key is not an RSA key")
		}
		sink.Key(jwa.RS256(), key)
		return nil
	})
}

func (g *Gate) refuse(w http.ResponseWriter, reason string) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	g.logger.Warn("request to the backoffice refused by the Access gate",
		"status", http.StatusUnauthorized, "reason", reason)
}

type operatorKey struct{}

// Operator gives the email of the operator a request was let through for. It is false for a
// context that did not pass the gate.
func Operator(ctx context.Context) (email string, ok bool) {
	email, ok = ctx.Value(operatorKey{}).(string)
	return email, ok
}
