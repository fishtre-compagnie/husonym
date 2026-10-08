package cptest

import (
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/stretchr/testify/require"
)

const (
	// AccessHeader is the header an Access token travels in.
	AccessHeader = "Cf-Access-Jwt-Assertion"

	accessIssuer   = "https://team.example.com"
	accessAudience = "aud-of-the-console"
	accessKid      = "key-1"
)

// Access stands for the Access application of the backoffice, with a throwaway key: it gives the
// gate that application would be guarded by, and the tokens that pass it. Making the key is slow,
// so the tests of a package may share one.
type Access struct {
	key jwk.Key
}

// NewAccess returns an Access with a fresh key pair.
func NewAccess(t *testing.T) *Access {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := jwk.Import(private)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, accessKid))
	return &Access{key: key}
}

// Gate is the gate that lets through the tokens of Token, judged on the clock now.
func (a *Access) Gate(t *testing.T, now func() time.Time, logger *slog.Logger) *accessgate.Gate {
	t.Helper()
	public, err := jwk.PublicKeyOf(a.key)
	require.NoError(t, err)
	keys := jwk.NewSet()
	require.NoError(t, keys.AddKey(public))
	return accessgate.NewWithKeys(accessIssuer, accessAudience, keys, now, logger)
}

// Token is a token issued to email at the instant at, valid for an hour.
func (a *Access) Token(t *testing.T, email string, at time.Time) string {
	t.Helper()
	token, err := jwt.NewBuilder().
		Issuer(accessIssuer).
		Audience([]string{accessAudience}).
		Subject("user-1").
		IssuedAt(at.Add(-time.Minute)).
		Expiration(at.Add(time.Hour)).
		Claim("email", email).
		Build()
	require.NoError(t, err)
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.RS256(), a.key))
	require.NoError(t, err)
	return string(signed)
}
