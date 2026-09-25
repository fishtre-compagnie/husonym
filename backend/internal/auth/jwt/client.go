package auth_jwt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/auth0/go-jwt-middleware/v3/jwks"
	"github.com/auth0/go-jwt-middleware/v3/validator"
	"github.com/fishtre-compagnie/husonym/backend/internal/utils"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

type ClientConfig struct {
	// Standard Issuer Url. Used for building the JWKS Provider
	BackendIssuerUrl string
	// Optionally provide a frontend Issuer Url. Falls back to BackendIssuerUrl if not provided.
	// This should be equivalent to what will be present in the "iss" claim of the JWT token.
	// This may be different depending auth provider or if running thorugh a reverse proxy
	FrontendIssuerUrl *string
	ApiAudiences      []string

	// Algorithms the signature may use. A deployment that names one accepts that one; a
	// deployment that names none accepts DefaultAsymmetricAlgorithms, because an account
	// bringing its own provider brings the algorithm that provider signs with.
	Algorithms []validator.SignatureAlgorithm

	// IssuerResolver answers which issuers to accept, per request. Required.
	//
	// It is what makes several providers possible, and it is also where the single-issuer
	// deployment lives: such a resolver returns one entry and nothing else changes. There
	// is deliberately no second path for that case -- WithIssuersResolver and WithIssuer
	// are mutually exclusive in the library, and two paths would be two behaviors.
	IssuerResolver func(ctx context.Context) ([]string, error)

	// HTTPClient fetches the issuers' discovery documents and keys. Optional; it is where
	// the bound on what an account's issuer may make this deployment contact lives.
	HTTPClient *http.Client

	// MaxCachedProviders bounds how many providers' key sets are held at once, evicting
	// the least recently used. Zero takes the library's default of 100.
	MaxCachedProviders int
}

type JwtValidator interface {
	ValidateToken(ctx context.Context, tokenString string) (any, error)
}

type Client struct {
	jwtValidator JwtValidator
}

func New(
	cfg *ClientConfig,
) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("must provide jwt client cfg")
	}
	if cfg.IssuerResolver == nil {
		return nil, fmt.Errorf("must provide an issuer resolver")
	}
	if _, err := url.Parse(cfg.BackendIssuerUrl); err != nil {
		return nil, err
	}

	providerOpts := []jwks.MultiIssuerProviderOption{
		jwks.WithMultiIssuerCacheTTL(5 * time.Minute),
		// Key material is fetched from wherever an issuer's discovery document points, so
		// without this an issuer chooses where this deployment sends requests. It holds
		// for every provider, including the deployment's own.
		jwks.WithMultiIssuerStrictJWKSURIOrigin(),
	}
	if cfg.HTTPClient != nil {
		providerOpts = append(providerOpts, jwks.WithMultiIssuerHTTPClient(cfg.HTTPClient))
	}
	if cfg.MaxCachedProviders > 0 {
		providerOpts = append(providerOpts, jwks.WithMaxProviders(cfg.MaxCachedProviders))
	}
	provider, err := jwks.NewMultiIssuerProvider(providerOpts...)
	if err != nil {
		return nil, err
	}

	algorithms := cfg.Algorithms
	if len(algorithms) == 0 {
		algorithms = DefaultAsymmetricAlgorithms
	}

	jwtValidator, err := validator.New(
		validator.WithKeyFunc(provider.KeyFunc),
		validator.WithAlgorithms(algorithms),
		validator.WithIssuersResolver(cfg.IssuerResolver),
		validator.WithAudiences(cfg.ApiAudiences),
		validator.WithCustomClaims(func() *CustomClaims {
			return &CustomClaims{}
		}),
		validator.WithAllowedClockSkew(time.Minute),
	)
	if err != nil {
		return nil, err
	}

	return &Client{
		jwtValidator: jwtValidator,
	}, nil
}

// Validates and returns a parsed access token (if available)
func (j *Client) validateToken(
	ctx context.Context,
	accessToken string,
) (*validator.ValidatedClaims, error) {
	rawParsedToken, err := j.jwtValidator.ValidateToken(ctx, accessToken)
	if err != nil {
		return nil, husonymerrors.NewUnauthenticated(err.Error())
	}
	validatedClaims, ok := rawParsedToken.(*validator.ValidatedClaims)
	if !ok {
		return nil, husonymerrors.NewInternalError(
			"unable to convert token claims what was expected",
		)
	}
	return validatedClaims, nil
}

type TokenContextKey struct{}

type TokenContextData struct {
	ParsedToken *validator.ValidatedClaims
	RawToken    string

	Claims *CustomClaims

	// AuthUserId is the sub claim: who the provider says this is, in ITS OWN namespace.
	// It identifies nobody on its own -- whoever declares a provider chooses the subjects
	// it issues -- so it is only ever used together with AuthIssuer.
	AuthUserId string
	// AuthIssuer is the validated iss claim: which provider vouches for AuthUserId.
	AuthIssuer string
	Scopes     []string // Contains Scopes & Permissions
}

// Identity returns the pair that identifies a user. Neither half means anything alone.
func (t *TokenContextData) Identity() (issuer, subject string) {
	return t.AuthIssuer, t.AuthUserId
}

func (t *TokenContextData) HasScope(scope string) bool {
	return hasScope(t.Scopes, scope)
}

func hasScope(scopes []string, expectedScope string) bool {
	for _, scope := range scopes {
		if expectedScope == scope {
			return true
		}
	}
	return false
}

// Validates the ctx is authenticated. Stuffs the parsed token onto the context
func (j *Client) InjectTokenCtx(
	ctx context.Context,
	header http.Header,
	spec connect.Spec,
) (context.Context, error) {
	token, err := utils.GetBearerTokenFromHeader(header, "Authorization")
	if err != nil {
		return nil, err
	}

	parsedToken, err := j.validateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	claims, ok := parsedToken.CustomClaims.(*CustomClaims)
	if !ok {
		return nil, husonymerrors.NewInternalError(
			"unable to cast custom token claims to CustomClaims struct",
		)
	}

	scopes := getCombinedScopesAndPermissions(claims.Scope, claims.Permissions)

	return SetTokenData(ctx, &TokenContextData{
		ParsedToken: parsedToken,
		RawToken:    token,

		Claims: claims,

		AuthUserId: parsedToken.RegisteredClaims.Subject,
		// The validator has already checked this against what the deployment accepts, so
		// it is the issuer the token was verified against and not merely what it claimed.
		AuthIssuer: parsedToken.RegisteredClaims.Issuer,
		Scopes:     scopes,
	}), nil
}

func getCombinedScopesAndPermissions(scope string, permissions []string) []string {
	scopes := strings.Split(scope, " ")

	scopeSet := map[string]struct{}{}
	for _, scope := range scopes {
		scopeSet[scope] = struct{}{}
	}
	for _, perm := range permissions {
		if _, ok := scopeSet[perm]; !ok {
			scopes = append(scopes, perm)
			scopeSet[perm] = struct{}{}
		}
	}
	return scopes
}

func GetTokenDataFromCtx(ctx context.Context) (*TokenContextData, error) {
	val := ctx.Value(TokenContextKey{})
	data, ok := val.(*TokenContextData)
	if !ok {
		return nil, husonymerrors.NewUnauthenticated(
			fmt.Sprintf("ctx does not contain TokenContextData or unable to cast struct: %T", val),
		)
	}
	return data, nil
}

func SetTokenData(ctx context.Context, data *TokenContextData) context.Context {
	return context.WithValue(ctx, TokenContextKey{}, data)
}

// DefaultAsymmetricAlgorithms is what a deployment accepts when it names no algorithm.
//
// Every asymmetric algorithm the validator knows, and no symmetric one. "Any compliant
// provider" means not making the operator guess which of RS256 and ES256 their provider
// signs with; a symmetric algorithm, on the other hand, means a signing key held by both
// parties, which is not a provider vouching for anything.
var DefaultAsymmetricAlgorithms = []validator.SignatureAlgorithm{
	validator.RS256, validator.RS384, validator.RS512,
	validator.ES256, validator.ES384, validator.ES512,
	validator.PS256, validator.PS384, validator.PS512,
	validator.EdDSA,
}
