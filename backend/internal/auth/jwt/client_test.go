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
	_, err := New(nil)
	assert.Error(t, err)

	_, err = New(
		&ClientConfig{
			BackendIssuerUrl:   "http://example.com",
			SignatureAlgorithm: validator.RS256,
			ApiAudiences:       []string{"foo"},
		},
	)
	assert.Nil(t, err)

	_, err = New(
		&ClientConfig{
			BackendIssuerUrl:   "http://example.com",
			SignatureAlgorithm: validator.RS256,
			ApiAudiences:       nil,
		},
	)
	assert.Error(t, err, "fails if api audiences is nil")
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
		BackendIssuerUrl:   srv.URL,
		SignatureAlgorithm: validator.RS256,
		ApiAudiences:       []string{"husonym-api"},
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
