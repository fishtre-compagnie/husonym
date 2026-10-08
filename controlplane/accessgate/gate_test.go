package accessgate_test

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
)

const (
	testIssuer   = "https://team.example.com"
	testAudience = "aud-of-the-console"
	testEmail    = "operator@example.com"
	testPath     = "/customers/a-path-the-caller-wrote"
)

var testNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// signer is one RSA key of a team, as Access would hold it.
type signer struct {
	kid     string
	private *rsa.PrivateKey
	key     jwk.Key
	public  jwk.Key
}

func newSigner(t *testing.T, kid string) *signer {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := jwk.Import(private)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, kid))
	public, err := jwk.PublicKeyOf(key)
	require.NoError(t, err)
	return &signer{kid: kid, private: private, key: key, public: public}
}

func setOf(t *testing.T, signers ...*signer) jwk.Set {
	t.Helper()
	set := jwk.NewSet()
	for _, s := range signers {
		require.NoError(t, set.AddKey(s.public))
	}
	return set
}

// claims is what a test token says. The zero value of a field leaves the claim out.
type claims struct {
	issuer    string
	audience  []string
	email     any
	expiry    time.Time
	notBefore time.Time
	// issuedAt, when zero, is a minute ago.
	issuedAt time.Time
}

func validClaims() claims {
	return claims{
		issuer:   testIssuer,
		audience: []string{testAudience},
		email:    testEmail,
		expiry:   testNow.Add(time.Hour),
	}
}

func (c claims) build(t *testing.T) jwt.Token {
	t.Helper()
	issuedAt := c.issuedAt
	if issuedAt.IsZero() {
		issuedAt = testNow.Add(-time.Minute)
	}
	builder := jwt.NewBuilder().Subject("user-1").IssuedAt(issuedAt)
	if c.issuer != "" {
		builder = builder.Issuer(c.issuer)
	}
	if c.audience != nil {
		builder = builder.Audience(c.audience)
	}
	if c.email != nil {
		builder = builder.Claim("email", c.email)
	}
	if !c.expiry.IsZero() {
		builder = builder.Expiration(c.expiry)
	}
	if !c.notBefore.IsZero() {
		builder = builder.NotBefore(c.notBefore)
	}
	token, err := builder.Build()
	require.NoError(t, err)
	return token
}

func (s *signer) sign(t *testing.T, c claims) string {
	t.Helper()
	signed, err := jwt.Sign(c.build(t), jwt.WithKey(jwa.RS256(), s.key))
	require.NoError(t, err)
	return string(signed)
}

// rig is a gate in front of a handler that records who reached it.
type rig struct {
	handler  http.Handler
	logs     *bytes.Buffer
	reached  bool
	operator string
	known    bool
}

func newRig(t *testing.T, wrap func(next http.Handler) http.Handler, logs *bytes.Buffer) *rig {
	t.Helper()
	r := &rig{logs: logs}
	r.handler = wrap(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		r.reached = true
		r.operator, r.known = accessgate.Operator(request.Context())
		w.WriteHeader(http.StatusOK)
	}))
	return r
}

func newFixedRig(t *testing.T, keys jwk.Set) *rig {
	t.Helper()
	logs := &bytes.Buffer{}
	gate := accessgate.NewWithKeys(testIssuer, testAudience, keys,
		func() time.Time { return testNow }, slog.New(slog.NewTextHandler(logs, nil)))
	return newRig(t, gate.Wrap, logs)
}

// call sends a request carrying the given tokens in the Access header: none, one or several.
func (r *rig) call(tokens ...string) *httptest.ResponseRecorder {
	r.reached, r.operator, r.known = false, "", false
	request := httptest.NewRequest(http.MethodGet, testPath, http.NoBody)
	for _, token := range tokens {
		request.Header.Add("Cf-Access-Jwt-Assertion", token)
	}
	recorder := httptest.NewRecorder()
	r.handler.ServeHTTP(recorder, request)
	return recorder
}

// requireRefused checks the whole answer of a refusal: 401, no body, not cacheable, the handler
// behind not reached, and one Warn line that says the reason and nothing of the caller.
func (r *rig) requireRefused(t *testing.T, recorder *httptest.ResponseRecorder, reason string, secrets ...string) {
	t.Helper()
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Empty(t, recorder.Body.Bytes(), "a refusal has no body")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.False(t, r.reached, "the handler behind the gate is not reached")

	logged := r.logs.String()
	require.Equal(t, 1, strings.Count(logged, "\n"), "one line per refusal: %s", logged)
	require.Contains(t, logged, "level=WARN")
	require.Contains(t, logged, "reason="+reason)
	for _, secret := range append(secrets, testPath, testEmail, "192.0.2.1") {
		require.NotContains(t, logged, secret)
	}
	r.logs.Reset()
}

func Test_Gate_ValidToken(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))

	recorder := r.call(team.sign(t, validClaims()))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, r.reached)
	require.True(t, r.known)
	require.Equal(t, testEmail, r.operator)
	require.Empty(t, r.logs.String(), "the gate logs nothing of a request it lets through")
}

func Test_Gate_AudienceAmongSeveral(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))

	c := validClaims()
	c.audience = []string{"another-application", testAudience}
	recorder := r.call(team.sign(t, c))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, testEmail, r.operator)
}

func Test_Gate_SecondKeyOfTheSet(t *testing.T) {
	old, current := newSigner(t, "key-1"), newSigner(t, "key-2")
	r := newFixedRig(t, setOf(t, old, current))

	require.Equal(t, http.StatusOK, r.call(old.sign(t, validClaims())).Code)
	require.Equal(t, http.StatusOK, r.call(current.sign(t, validClaims())).Code)
}

func Test_Gate_MissingToken(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))

	r.requireRefused(t, r.call(), "missing")
	r.requireRefused(t, r.call(""), "missing")
}

func Test_Gate_ClockSkew(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))

	t.Run("expired within the allowance", func(t *testing.T) {
		c := validClaims()
		c.expiry = testNow.Add(-accessgate.ClockSkew + 5*time.Second)
		require.Equal(t, http.StatusOK, r.call(team.sign(t, c)).Code)
	})
	t.Run("expired beyond the allowance", func(t *testing.T) {
		c := validClaims()
		c.expiry = testNow.Add(-accessgate.ClockSkew - 5*time.Second)
		token := team.sign(t, c)
		r.requireRefused(t, r.call(token), "invalid", token)
	})
	t.Run("not valid yet, within the allowance", func(t *testing.T) {
		c := validClaims()
		c.notBefore = testNow.Add(accessgate.ClockSkew - 5*time.Second)
		require.Equal(t, http.StatusOK, r.call(team.sign(t, c)).Code)
	})
	t.Run("not valid yet, beyond the allowance", func(t *testing.T) {
		c := validClaims()
		c.notBefore = testNow.Add(accessgate.ClockSkew + 5*time.Second)
		token := team.sign(t, c)
		r.requireRefused(t, r.call(token), "invalid", token)
	})
	t.Run("issued in the future, within the allowance", func(t *testing.T) {
		c := validClaims()
		c.issuedAt = testNow.Add(accessgate.ClockSkew - 5*time.Second)
		require.Equal(t, http.StatusOK, r.call(team.sign(t, c)).Code)
	})
	t.Run("issued in the future, beyond the allowance", func(t *testing.T) {
		c := validClaims()
		c.issuedAt = testNow.Add(accessgate.ClockSkew + 5*time.Second)
		token := team.sign(t, c)
		r.requireRefused(t, r.call(token), "invalid", token)
	})
}

// The algorithm is decided by the gate, not by the token. Each token here carries a genuine
// RS256 signature of the team over its own header and payload, and differs from a sound one only
// by the algorithm its header declares: handing the key over as an RS256 key whatever the header
// says would let every one of them through.
func Test_Gate_DeclaredAlgorithm(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))

	sound := team.signedRS256(t, `{"alg":"RS256","kid":"key-1","typ":"JWT"}`)
	require.Equal(t, http.StatusOK, r.call(sound).Code, "the hand-built token is sound when it says RS256")
	require.Equal(t, testEmail, r.operator)

	for _, alg := range []string{"RS384", "RS512", "PS256", "HS256", "ES256", "none", "rs256", ""} {
		t.Run("declared as "+alg, func(t *testing.T) {
			token := team.signedRS256(t, `{"alg":"`+alg+`","kid":"key-1","typ":"JWT"}`)
			r.requireRefused(t, r.call(token), "invalid", token)
		})
	}
	t.Run("declared as nothing", func(t *testing.T) {
		token := team.signedRS256(t, `{"kid":"key-1","typ":"JWT"}`)
		r.requireRefused(t, r.call(token), "invalid", token)
	})
}

// A token may bring a key of its own in its header, or the address of one. The gate checks a
// signature with the keys of the team and no other.
func Test_Gate_KeyBroughtByTheToken_IsIgnored(t *testing.T) {
	team := newSigner(t, "key-1")
	forger := newSigner(t, "key-1")
	brought, err := json.Marshal(forger.public)
	require.NoError(t, err)

	for name, header := range map[string]string{
		"jwk":                    `{"alg":"RS256","kid":"key-1","jwk":` + string(brought) + `}`,
		"jwk without a key id":   `{"alg":"RS256","jwk":` + string(brought) + `}`,
		"jku":                    `{"alg":"RS256","kid":"key-1","jku":"https://forger.example.com/keys"}`,
		"jku under another name": `{"alg":"RS256","kid":"key-of-the-forger","jku":"https://forger.example.com/keys"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newFixedRig(t, setOf(t, team))
			token := forger.signedRS256(t, header)
			r.requireRefused(t, r.call(token), "invalid", token)
		})
	}
}

// Access also sets a cookie for the browser. The gate reads the header only: a cookie is what a
// page can be made to send.
func Test_Gate_CookieAlone_IsNotAToken(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))
	sound := team.sign(t, validClaims())

	request := httptest.NewRequest(http.MethodGet, testPath, http.NoBody)
	request.AddCookie(&http.Cookie{Name: "CF_Authorization", Value: sound})
	recorder := httptest.NewRecorder()
	r.handler.ServeHTTP(recorder, request)

	r.requireRefused(t, recorder, "missing", sound)
}

// signedRS256 builds a compact token by hand: the given protected header, the payload of sound
// claims, and an RS256 signature by this key over the two.
func (s *signer) signedRS256(t *testing.T, header string) string {
	t.Helper()
	payload := strings.Split(s.sign(t, validClaims()), ".")[1]
	input := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." + payload
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.private, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func Test_Gate_Refusals(t *testing.T) {
	team := newSigner(t, "key-1")
	// Same key id, other key: what a forger who read the public set can produce.
	forger := newSigner(t, "key-1")
	stranger := newSigner(t, "key-of-nobody")

	cases := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{"expired", func(t *testing.T) string {
			c := validClaims()
			c.expiry = testNow.Add(-time.Hour)
			return team.sign(t, c)
		}},
		{"without an expiry", func(t *testing.T) string {
			c := validClaims()
			c.expiry = time.Time{}
			return team.sign(t, c)
		}},
		{"another issuer", func(t *testing.T) string {
			c := validClaims()
			c.issuer = "https://other-team.example.com"
			return team.sign(t, c)
		}},
		{"an issuer that only starts like ours", func(t *testing.T) string {
			c := validClaims()
			c.issuer = testIssuer + ".evil.test"
			return team.sign(t, c)
		}},
		{"without an issuer", func(t *testing.T) string {
			c := validClaims()
			c.issuer = ""
			return team.sign(t, c)
		}},
		// Review Focus 1: the keys of a team sign the tokens of all its applications, so this
		// token is sound in every way but the application it was issued for.
		{"another application of the same team", func(t *testing.T) string {
			c := validClaims()
			c.audience = []string{"aud-of-a-neighbour"}
			return team.sign(t, c)
		}},
		{"without an audience", func(t *testing.T) string {
			c := validClaims()
			c.audience = nil
			return team.sign(t, c)
		}},
		{"without an email", func(t *testing.T) string {
			c := validClaims()
			c.email = nil
			return team.sign(t, c)
		}},
		{"an empty email", func(t *testing.T) string {
			c := validClaims()
			c.email = ""
			return team.sign(t, c)
		}},
		{"a blank email", func(t *testing.T) string {
			c := validClaims()
			c.email = "  "
			return team.sign(t, c)
		}},
		{"an email that is not text", func(t *testing.T) string {
			c := validClaims()
			c.email = []string{testEmail}
			return team.sign(t, c)
		}},
		{"signed by another key under the same key id", func(t *testing.T) string {
			return forger.sign(t, validClaims())
		}},
		{"signed by a key the set does not hold", func(t *testing.T) string {
			return stranger.sign(t, validClaims())
		}},
		{"without a key id", func(t *testing.T) string {
			signed, err := jwt.Sign(validClaims().build(t), jwt.WithKey(jwa.RS256(), team.private))
			require.NoError(t, err)
			return string(signed)
		}},
		{"alg none", func(t *testing.T) string {
			return unsigned(t, team.kid, team.sign(t, validClaims()))
		}},
		{"alg none keeping the signature", func(t *testing.T) string {
			sound := team.sign(t, validClaims())
			return unsigned(t, team.kid, sound) + sound[strings.LastIndex(sound, ".")+1:]
		}},
		// The classic confusion: the public key, which anybody can read, used as an HMAC secret.
		{"HS256 signed with the public key as PEM", func(t *testing.T) string {
			return signHS256(t, team.kid, publicPEM(t, team))
		}},
		{"HS256 signed with the public key as DER", func(t *testing.T) string {
			der, err := x509.MarshalPKIXPublicKey(&team.private.PublicKey)
			require.NoError(t, err)
			return signHS256(t, team.kid, der)
		}},
		{"RS384 by the right key", func(t *testing.T) string {
			signed, err := jwt.Sign(validClaims().build(t), jwt.WithKey(jwa.RS384(), team.key))
			require.NoError(t, err)
			return string(signed)
		}},
		{"PS256 by the right key", func(t *testing.T) string {
			signed, err := jwt.Sign(validClaims().build(t), jwt.WithKey(jwa.PS256(), team.key))
			require.NoError(t, err)
			return string(signed)
		}},
		{"a payload changed after signing", func(t *testing.T) string {
			sound := strings.Split(team.sign(t, validClaims()), ".")
			c := validClaims()
			c.email = "intruder@example.com"
			other := strings.Split(team.sign(t, c), ".")
			return sound[0] + "." + other[1] + "." + sound[2]
		}},
		{"not a token", func(*testing.T) string { return "not-a-token" }},
		{"three empty parts", func(*testing.T) string { return ".." }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newFixedRig(t, setOf(t, team))
			token := c.token(t)
			r.requireRefused(t, r.call(token), "invalid", token)
		})
	}
}

func Test_Gate_SeveralHeaders_AreRefused(t *testing.T) {
	team := newSigner(t, "key-1")
	r := newFixedRig(t, setOf(t, team))
	sound := team.sign(t, validClaims())

	r.requireRefused(t, r.call(sound, sound), "invalid", sound)
	r.requireRefused(t, r.call("not-a-token", sound), "invalid", sound)
}

func Test_Operator_WithoutAGate(t *testing.T) {
	email, ok := accessgate.Operator(t.Context())
	require.False(t, ok)
	require.Empty(t, email)
}

// unsigned rewrites a sound token as one that claims no signature at all.
func unsigned(t *testing.T, kid, sound string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"` + kid + `","typ":"JWT"}`))
	return header + "." + strings.Split(sound, ".")[1] + "."
}

func publicPEM(t *testing.T, s *signer) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&s.private.PublicKey)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func signHS256(t *testing.T, kid string, secret []byte) string {
	t.Helper()
	headers := jws.NewHeaders()
	require.NoError(t, headers.Set(jws.KeyIDKey, kid))
	signed, err := jwt.Sign(validClaims().build(t),
		jwt.WithKey(jwa.HS256(), secret, jws.WithProtectedHeaders(headers)))
	require.NoError(t, err)
	return string(signed)
}
