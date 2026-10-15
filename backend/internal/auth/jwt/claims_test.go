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

// A token issued to a daemon carries the subject of a service principal, not a person.
// Signing one in would mint a user, and a personal account with it, for every such
// application in the tenant.
func Test_CustomClaims_IsApplicationToken(t *testing.T) {
	cases := []struct {
		name  string
		claim string
		want  bool
	}{
		{"an application token says so", `{"idtyp": "app"}`, true},
		{"whatever the case", `{"idtyp": "App"}`, true},
		{"a user token says so too", `{"idtyp": "user"}`, false},
		// The claim is one provider's. Everywhere else it is absent, and a provider that
		// says nothing must keep working: inferring an application from a sparse token
		// would lock out real people.
		{"a provider that says nothing", `{"scope": "read:jobs"}`, false},
		{"an empty value", `{"idtyp": ""}`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var claims CustomClaims
			require.NoError(t, json.Unmarshal([]byte(tc.claim), &claims))
			require.Equal(t, tc.want, claims.IsApplicationToken())
		})
	}

	t.Run("nil claims are not an application token", func(t *testing.T) {
		var claims *CustomClaims
		require.False(t, claims.IsApplicationToken())
	})
}

func Test_TokenContextData_Identity(t *testing.T) {
	data := &TokenContextData{AuthUserId: "sub-1", AuthIssuer: "https://idp.example.com/"}
	issuer, subject := data.Identity()

	require.Equal(t, "https://idp.example.com/", issuer)
	require.Equal(t, "sub-1", subject)
}

// Entra never emits email_verified. It says instead, in an optional claim, that the tenant owns
// the domain of the address: that is what an invitation may be accepted on there.
func Test_CustomClaims_IsEmailVerified(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{"the standard claim", `{"email":"ada@example.com","email_verified":true}`, true},
		{"the standard claim as a string", `{"email":"ada@example.com","email_verified":"true"}`, true},
		{"entra, as a number", `{"email":"ada@example.com","xms_edov":1}`, true},
		{"entra, as a string", `{"email":"ada@example.com","xms_edov":"1"}`, true},
		{"entra, as a boolean", `{"email":"ada@example.com","xms_edov":true}`, true},
		{"entra says the domain is not verified", `{"email":"ada@example.com","xms_edov":0}`, false},
		{"entra, as a false string", `{"email":"ada@example.com","xms_edov":"0"}`, false},
		{"no statement at all", `{"email":"ada@example.com"}`, false},
		{"a statement without an address", `{"xms_edov":1}`, false},
		{"an empty address", `{"email":"","xms_edov":1}`, false},
		{"entra, in a shape nobody wrote", `{"email":"ada@example.com","xms_edov":{"value":1}}`, false},
		{"1 is not email_verified", `{"email":"ada@example.com","email_verified":"1"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var claims CustomClaims
			require.NoError(t, json.Unmarshal([]byte(c.payload), &claims))
			require.Equal(t, c.want, claims.IsEmailVerified())
		})
	}

	t.Run("a nil claims value vouches for nothing", func(t *testing.T) {
		var claims *CustomClaims
		require.False(t, claims.IsEmailVerified())
	})
}
