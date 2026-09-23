package auth_jwt

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/auth0/go-jwt-middleware/v3/validator"
)

func Test_hasScope(t *testing.T) {
	assert.True(
		t,
		hasScope([]string{"foo", "bar"}, "foo"),
	)
	assert.False(
		t,
		hasScope([]string{"foo", "bar"}, "fooo"),
	)
}

func Test_TokenContextData_HasScope(t *testing.T) {
	data := &TokenContextData{
		Scopes: []string{"foo", "bar"},
	}
	assert.True(
		t,
		data.HasScope("foo"),
	)
	assert.False(
		t,
		data.HasScope("fooo"),
	)
}

func Test_getCombinedScopesAndPermissions(t *testing.T) {
	assert.Equal(
		t,
		getCombinedScopesAndPermissions("foo bar baz", []string{"foo", "bazz"}),
		[]string{"foo", "bar", "baz", "bazz"},
	)
}

func Test_GetTokenDataFromCtx_Unauthenticated(t *testing.T) {
	data, err := GetTokenDataFromCtx(context.Background())
	assert.Error(t, err)
	assert.Nil(t, data)
}

func Test_GetTokenDataFromCtx_Authenticated(t *testing.T) {
	data := &TokenContextData{}
	ctx := context.WithValue(context.Background(), TokenContextKey{}, data)

	ctxdata, err := GetTokenDataFromCtx(ctx)
	assert.Nil(t, err)
	assert.Equal(t, ctxdata, data)
}

func Test_New(t *testing.T) {
	oneIssuer := func(context.Context) ([]string, error) {
		return []string{"http://example.com"}, nil
	}

	_, err := New(nil)
	assert.Error(t, err)

	_, err = New(
		&ClientConfig{
			BackendIssuerUrl: "http://example.com",
			Algorithms:       []validator.SignatureAlgorithm{validator.RS256},
			ApiAudiences:     []string{"foo"},
			IssuerResolver:   oneIssuer,
		},
	)
	assert.Nil(t, err)

	_, err = New(
		&ClientConfig{
			BackendIssuerUrl: "http://example.com",
			ApiAudiences:     []string{"foo"},
			IssuerResolver:   oneIssuer,
		},
	)
	assert.Nil(t, err, "naming no algorithm accepts the asymmetric ones")

	_, err = New(
		&ClientConfig{
			BackendIssuerUrl: "http://example.com",
			Algorithms:       []validator.SignatureAlgorithm{validator.RS256},
			ApiAudiences:     nil,
			IssuerResolver:   oneIssuer,
		},
	)
	assert.Error(t, err, "fails if api audiences is nil")

	// The resolver is what says which issuers are accepted. Without one there is no
	// answer to that question, and the library would have none either.
	_, err = New(
		&ClientConfig{
			BackendIssuerUrl: "http://example.com",
			ApiAudiences:     []string{"foo"},
		},
	)
	assert.Error(t, err, "fails without an issuer resolver")
}

func Test_Client_InjectTokenCtx(t *testing.T) {
	customclaims := &CustomClaims{
		Scope: "foo bar",
	}
	validatedClaims := &validator.ValidatedClaims{
		RegisteredClaims: validator.RegisteredClaims{
			Subject: "test user",
		},
		CustomClaims: customclaims,
	}
	jwtValidator := &MockJwtValidator{}
	jwtValidator.On("ValidateToken", mock.Anything, "123").Return(validatedClaims, nil)
	client := &Client{jwtValidator: jwtValidator}

	newCtx, err := client.InjectTokenCtx(
		context.Background(),
		http.Header{"Authorization": []string{"Bearer 123"}},
		connect.Spec{},
	)
	assert.Nil(t, err)

	data, err := GetTokenDataFromCtx(newCtx)
	assert.Nil(t, err)
	assert.Equal(
		t,
		data,
		&TokenContextData{
			ParsedToken: validatedClaims,
			RawToken:    "123",
			Claims:      customclaims,
			AuthUserId:  "test user",
			Scopes:      []string{"foo", "bar"},
		},
	)
}

// Runs the real validator against a local issuer: OIDC discovery, JWKS
// fetch, RS256 signature, issuer, audience, expiry and custom claims.
func Test_Client_InjectTokenCtx_SignedToken(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signingKey, err := jwk.Import(privateKey)
	require.NoError(t, err)
	require.NoError(t, signingKey.Set(jwk.KeyIDKey, "test-key"))
	require.NoError(t, signingKey.Set(jwk.AlgorithmKey, jwa.RS256()))
	publicKey, err := jwk.PublicKeyOf(signingKey)
	require.NoError(t, err)
	keySet := jwk.NewSet()
	require.NoError(t, keySet.AddKey(publicKey))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   srv.URL,
			"jwks_uri": srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(keySet)
	})

	client, err := New(&ClientConfig{
		BackendIssuerUrl: srv.URL,
		Algorithms:       []validator.SignatureAlgorithm{validator.RS256},
		ApiAudiences:     []string{"husonym-api"},
		IssuerResolver: func(context.Context) ([]string, error) {
			return []string{srv.URL}, nil
		},
	})
	require.NoError(t, err)

	sign := func(t *testing.T, audience string, expiry time.Time) string {
		t.Helper()
		token, err := jwt.NewBuilder().
			Issuer(srv.URL).
			Audience([]string{audience}).
			Subject("user-1").
			IssuedAt(time.Now()).
			Expiration(expiry).
			Claim("scope", "read write").
			Claim("permissions", []string{"admin"}).
			Build()
		require.NoError(t, err)
		signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), signingKey))
		require.NoError(t, err)
		return string(signed)
	}
	inject := func(token string) (context.Context, error) {
		return client.InjectTokenCtx(
			t.Context(),
			http.Header{"Authorization": []string{"Bearer " + token}},
			connect.Spec{},
		)
	}

	t.Run("valid token", func(t *testing.T) {
		ctx, err := inject(sign(t, "husonym-api", time.Now().Add(time.Hour)))
		require.NoError(t, err)
		data, err := GetTokenDataFromCtx(ctx)
		require.NoError(t, err)
		require.Equal(t, "user-1", data.AuthUserId)
		require.Equal(t, []string{"read", "write", "admin"}, data.Scopes)
	})

	t.Run("other audience", func(t *testing.T) {
		_, err := inject(sign(t, "other-api", time.Now().Add(time.Hour)))
		require.Error(t, err)
	})

	t.Run("expired beyond the clock skew", func(t *testing.T) {
		_, err := inject(sign(t, "husonym-api", time.Now().Add(-2*time.Minute)))
		require.Error(t, err)
	})

	t.Run("signed by another key", func(t *testing.T) {
		otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		token, err := jwt.NewBuilder().
			Issuer(srv.URL).
			Audience([]string{"husonym-api"}).
			Subject("user-1").
			Expiration(time.Now().Add(time.Hour)).
			Build()
		require.NoError(t, err)
		otherJwk, err := jwk.Import(otherKey)
		require.NoError(t, err)
		require.NoError(t, otherJwk.Set(jwk.KeyIDKey, "test-key"))
		signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), otherJwk))
		require.NoError(t, err)
		_, err = inject(string(signed))
		require.Error(t, err)
	})
}

func Test_Client_InjectTokenCtx_InvalidHeader(t *testing.T) {
	client := &Client{}
	_, err := client.InjectTokenCtx(
		context.Background(),
		http.Header{"Authorization": []string{}},
		connect.Spec{},
	)
	assert.Error(t, err)
}

func Test_Client_InjectTokenCtx_InvalidToken(t *testing.T) {
	jwtValidator := &MockJwtValidator{}
	jwtValidator.On("ValidateToken", mock.Anything, "123").Return(nil, errors.New("invalid token"))
	client := &Client{jwtValidator: jwtValidator}

	_, err := client.InjectTokenCtx(
		context.Background(),
		http.Header{"Authorization": []string{"Bearer 123"}},
		connect.Spec{},
	)
	assert.Error(t, err)
}

func Test_Client_InjectTokenCtx_InvalidTokenClaims(t *testing.T) {
	validatedClaims := map[string]string{}
	jwtValidator := &MockJwtValidator{}
	jwtValidator.On("ValidateToken", mock.Anything, "123").Return(validatedClaims, nil)
	client := &Client{jwtValidator: jwtValidator}

	_, err := client.InjectTokenCtx(
		context.Background(),
		http.Header{"Authorization": []string{"Bearer 123"}},
		connect.Spec{},
	)
	assert.Error(t, err)
}

func Test_Client_InjectTokenCtx_InvalidClaims(t *testing.T) {
	validatedClaims := &validator.ValidatedClaims{
		RegisteredClaims: validator.RegisteredClaims{
			Subject: "test user",
		},
		CustomClaims: nil,
	}
	jwtValidator := &MockJwtValidator{}
	jwtValidator.On("ValidateToken", mock.Anything, "123").Return(validatedClaims, nil)
	client := &Client{jwtValidator: jwtValidator}

	_, err := client.InjectTokenCtx(
		context.Background(),
		http.Header{"Authorization": []string{"Bearer 123"}},
		connect.Spec{},
	)
	assert.Error(t, err)
}

// Two providers at once, which is what the multi-issuer provider is for: each token has to
// be checked against the keys of the issuer that minted it, and no other.
func Test_Client_InjectTokenCtx_TwoIssuers(t *testing.T) {
	first := newTestIssuer(t)
	second := newTestIssuer(t)

	client, err := New(&ClientConfig{
		BackendIssuerUrl: first.url,
		Algorithms:       []validator.SignatureAlgorithm{validator.RS256},
		ApiAudiences:     []string{"husonym-api"},
		IssuerResolver: func(context.Context) ([]string, error) {
			return []string{first.url, second.url}, nil
		},
	})
	require.NoError(t, err)

	inject := func(token string) (context.Context, error) {
		return client.InjectTokenCtx(
			t.Context(),
			http.Header{"Authorization": []string{"Bearer " + token}},
			connect.Spec{},
		)
	}

	t.Run("each issuer's own token is accepted, and carries its issuer", func(t *testing.T) {
		for _, issuer := range []*testIssuer{first, second} {
			ctx, err := inject(issuer.sign(t, "husonym-api", time.Now().Add(time.Hour)))
			require.NoError(t, err)

			data, err := GetTokenDataFromCtx(ctx)
			require.NoError(t, err)
			require.Equal(t, issuer.url, data.AuthIssuer,
				"the identity is a pair, so the issuer has to reach the context")
			require.Equal(t, "user-1", data.AuthUserId)
		}
	})

	// The whole point of keying identities on the issuer: both providers mint the same
	// subject, and they are two different people.
	t.Run("the same subject from each issuer is two identities", func(t *testing.T) {
		firstCtx, err := inject(first.sign(t, "husonym-api", time.Now().Add(time.Hour)))
		require.NoError(t, err)
		secondCtx, err := inject(second.sign(t, "husonym-api", time.Now().Add(time.Hour)))
		require.NoError(t, err)

		firstData, err := GetTokenDataFromCtx(firstCtx)
		require.NoError(t, err)
		secondData, err := GetTokenDataFromCtx(secondCtx)
		require.NoError(t, err)

		require.Equal(t, firstData.AuthUserId, secondData.AuthUserId)
		require.NotEqual(t, firstData.AuthIssuer, secondData.AuthIssuer)
	})

	t.Run("an issuer the resolver does not name is refused", func(t *testing.T) {
		stranger := newTestIssuer(t)
		_, err := inject(stranger.sign(t, "husonym-api", time.Now().Add(time.Hour)))
		require.Error(t, err, "a token nobody vouches for must not authenticate")
	})

	// A token signed by one issuer but claiming to be another: the keys are fetched for
	// the issuer in the claim, so the signature does not check out.
	t.Run("a token cannot borrow another issuer's name", func(t *testing.T) {
		_, err := inject(first.signAs(t, second.url, "husonym-api", time.Now().Add(time.Hour)))
		require.Error(t, err)
	})
}

type testIssuer struct {
	url        string
	signingKey jwk.Key
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signingKey, err := jwk.Import(privateKey)
	require.NoError(t, err)
	require.NoError(t, signingKey.Set(jwk.KeyIDKey, "key-"+t.Name()))
	require.NoError(t, signingKey.Set(jwk.AlgorithmKey, jwa.RS256()))
	publicKey, err := jwk.PublicKeyOf(signingKey)
	require.NoError(t, err)
	keySet := jwk.NewSet()
	require.NoError(t, keySet.AddKey(publicKey))

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   srv.URL,
			"jwks_uri": srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(keySet)
	})

	return &testIssuer{url: srv.URL, signingKey: signingKey}
}

func (i *testIssuer) sign(t *testing.T, audience string, expiry time.Time) string {
	return i.signAs(t, i.url, audience, expiry)
}

// signAs signs with this issuer's key while claiming to be another, which is how a token
// tries to borrow a name it cannot prove.
func (i *testIssuer) signAs(t *testing.T, issuer, audience string, expiry time.Time) string {
	t.Helper()
	token, err := jwt.NewBuilder().
		Issuer(issuer).
		Audience([]string{audience}).
		Subject("user-1").
		IssuedAt(time.Now()).
		Expiration(expiry).
		Claim("scope", "read").
		Build()
	require.NoError(t, err)
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), i.signingKey))
	require.NoError(t, err)
	return string(signed)
}
