package auth_jwt

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_CustomClaims_ProfileClaims(t *testing.T) {
	t.Run("reads the standard OIDC profile claims", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{
			"scope": "openid profile",
			"name": "Ada Lovelace",
			"email": "ada@example.com",
			"email_verified": true,
			"picture": "https://example.com/ada.png"
		}`), &claims))

		require.True(t, claims.HasProfileClaims())
		require.Equal(t, "Ada Lovelace", claims.Name)
		require.Equal(t, "ada@example.com", *claims.Email)
		require.True(t, bool(claims.EmailVerified))
		require.Equal(t, "https://example.com/ada.png", claims.Picture)
	})

	t.Run("a token without a profile is not an error", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{"scope": "read:jobs"}`), &claims))

		require.False(t, claims.HasProfileClaims())
		require.False(t, bool(claims.EmailVerified))
	})

	t.Run("a name without an address is not a profile", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{"name": "Ada Lovelace"}`), &claims))

		require.False(t, claims.HasProfileClaims())
	})

	t.Run("an empty address is not a profile", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{"email": ""}`), &claims))

		require.False(t, claims.HasProfileClaims())
	})

	// The shape of a display claim must never cost a sign-in: the validator rejects the
	// token on any unmarshal error. utils.LenientBool covers the shapes themselves.
	t.Run("a display claim in an unexpected shape keeps the rest of the payload", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{
			"scope": "read:jobs",
			"email": "ada@example.com",
			"email_verified": {"value": true}
		}`), &claims))

		require.Equal(t, "read:jobs", claims.Scope)
		require.True(t, claims.HasProfileClaims())
		require.False(t, bool(claims.EmailVerified))
	})
}
