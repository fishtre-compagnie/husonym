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
}

// A provider that spells a boolean claim as a string must not fail the whole payload:
// the validator rejects the token on any unmarshal error, so that would lock every user
// of the deployment out over a display claim.
func Test_LenientBool(t *testing.T) {
	t.Run("accepts a boolean", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{"email_verified": true}`), &claims))
		require.True(t, bool(claims.EmailVerified))

		require.NoError(t, json.Unmarshal([]byte(`{"email_verified": false}`), &claims))
		require.False(t, bool(claims.EmailVerified))
	})

	t.Run("accepts the string spelling of one", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{"email_verified": "true"}`), &claims))
		require.True(t, bool(claims.EmailVerified))

		require.NoError(t, json.Unmarshal([]byte(`{"email_verified": "false"}`), &claims))
		require.False(t, bool(claims.EmailVerified))
	})

	t.Run("anything else it cannot read is false, not verified", func(t *testing.T) {
		var claims CustomClaims
		require.NoError(t, json.Unmarshal([]byte(`{"email_verified": "yes"}`), &claims))
		require.False(t, bool(claims.EmailVerified))
	})

	t.Run("a shape it can read neither way is still an error", func(t *testing.T) {
		var claims CustomClaims
		require.Error(t, json.Unmarshal([]byte(`{"email_verified": {"a": 1}}`), &claims))
	})
}
