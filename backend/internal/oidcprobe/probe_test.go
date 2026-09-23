package oidcprobe

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func firstCheck(t *testing.T, checks []Check) Check {
	t.Helper()
	require.NotEmpty(t, checks)
	return checks[0]
}

// The check that earns the whole endpoint. Entra's multi-tenant endpoints answer with a
// literal template as their issuer, so an account configured that way saves cleanly and
// then refuses every sign-in -- the failure this is here to turn into a sentence.
func Test_checkIssuerMatches(t *testing.T) {
	t.Run("a provider that calls itself what it was asked under", func(t *testing.T) {
		require.Empty(t, checkIssuerMatches(
			"https://login.microsoftonline.com/72f9-tenant/v2.0",
			&discovery{Issuer: "https://login.microsoftonline.com/72f9-tenant/v2.0"},
		))
	})

	t.Run("the multi-tenant endpoint is named for what it is", func(t *testing.T) {
		got := firstCheck(t, checkIssuerMatches(
			"https://login.microsoftonline.com/common/v2.0",
			&discovery{Issuer: "https://login.microsoftonline.com/{tenantid}/v2.0"},
		))

		require.Equal(t, LevelBlocking, got.Level)
		require.Contains(t, got.Remedy, "one tenant",
			"a template issuer needs the remedy for a template, not the one for a typo")
	})

	t.Run("a mismatch names what the provider calls itself", func(t *testing.T) {
		got := firstCheck(t, checkIssuerMatches(
			"https://idp.example.com",
			&discovery{Issuer: "https://idp.example.com/"},
		))

		require.Equal(t, LevelBlocking, got.Level)
		require.Contains(t, got.Remedy, "https://idp.example.com/",
			"the remedy must carry the value to paste, trailing slash included")
	})
}

// The validator will refuse a jwks_uri on another origin. Finding that out here beats
// finding it out when nobody can sign in.
func Test_checkJwksOrigin(t *testing.T) {
	issuer := mustURL(t, "https://idp.example.com")

	t.Run("same origin passes", func(t *testing.T) {
		require.Empty(t, checkJwksOrigin(issuer, &discovery{
			JwksURI: "https://idp.example.com/keys",
		}))
	})

	t.Run("another host is refused", func(t *testing.T) {
		got := firstCheck(t, checkJwksOrigin(issuer, &discovery{
			JwksURI: "https://attacker.example.net/keys",
		}))
		require.Equal(t, LevelBlocking, got.Level)
		require.Equal(t, "jwks_uri_same_origin", got.Check)
	})

	t.Run("another scheme is refused", func(t *testing.T) {
		got := firstCheck(t, checkJwksOrigin(issuer, &discovery{
			JwksURI: "http://idp.example.com/keys",
		}))
		require.Equal(t, LevelBlocking, got.Level)
	})

	t.Run("no jwks_uri at all is refused", func(t *testing.T) {
		got := firstCheck(t, checkJwksOrigin(issuer, &discovery{}))
		require.Equal(t, "jwks_uri_present", got.Check)
	})
}

func Test_checkAlgorithms(t *testing.T) {
	accepted := []string{"RS256", "ES256"}

	t.Run("one in common is enough", func(t *testing.T) {
		require.Empty(t, checkAlgorithms(accepted, &discovery{
			IDTokenSigningAlgValuesSupported: []string{"PS512", "ES256"},
		}))
	})

	t.Run("none in common is refused, and both lists are named", func(t *testing.T) {
		got := firstCheck(t, checkAlgorithms(accepted, &discovery{
			IDTokenSigningAlgValuesSupported: []string{"HS256"},
		}))
		require.Equal(t, LevelBlocking, got.Level)
		require.Contains(t, got.Detail, "HS256")
		require.Contains(t, got.Remedy, "RS256")
	})

	t.Run("a provider that publishes no algorithms is not judged on it", func(t *testing.T) {
		require.Empty(t, checkAlgorithms(accepted, &discovery{}))
	})
}

func Test_checkEndpoints(t *testing.T) {
	t.Run("a complete provider has nothing to say", func(t *testing.T) {
		require.Empty(t, checkEndpoints(&discovery{
			AuthorizationEndpoint: "https://idp.example.com/authorize",
			TokenEndpoint:         "https://idp.example.com/token",
			UserinfoEndpoint:      "https://idp.example.com/userinfo",
		}))
	})

	t.Run("no authorization endpoint blocks", func(t *testing.T) {
		got := firstCheck(t, checkEndpoints(&discovery{
			TokenEndpoint: "https://idp.example.com/token",
		}))
		require.Equal(t, LevelBlocking, got.Level)
	})

	// Not blocking: a deployment whose tokens carry the profile claims needs no userinfo,
	// and refusing it would rule out providers that work.
	t.Run("no userinfo endpoint only warns", func(t *testing.T) {
		got := firstCheck(t, checkEndpoints(&discovery{
			AuthorizationEndpoint: "https://idp.example.com/authorize",
			TokenEndpoint:         "https://idp.example.com/token",
		}))
		require.Equal(t, LevelWarning, got.Level)
		require.Equal(t, "userinfo_endpoint_present", got.Check)
	})
}

func Test_discoveryURL(t *testing.T) {
	// The standard says the path is appended to the issuer, and an issuer that ends in a
	// slash is common enough that doubling it would be a daily paper cut.
	require.Equal(t,
		"https://idp.example.com/.well-known/openid-configuration",
		discoveryURL("https://idp.example.com"))
	require.Equal(t,
		"https://idp.example.com/.well-known/openid-configuration",
		discoveryURL("https://idp.example.com/"))
	// An issuer with a path keeps it: that is what a tenant looks like.
	require.Equal(t,
		"https://login.microsoftonline.com/tenant/v2.0/.well-known/openid-configuration",
		discoveryURL("https://login.microsoftonline.com/tenant/v2.0"))
}

func Test_Run_refusesWhatCannotBeAnIssuer(t *testing.T) {
	probe := New()

	for _, issuer := range []string{"", "not a url", "http://idp.example.com", "ftp://idp.example.com"} {
		t.Run(issuer, func(t *testing.T) {
			checks := probe.Run(t.Context(), Input{Issuer: issuer, ClientID: "client"})
			require.True(t, HasBlocking(checks))
			require.Equal(t, "issuer_is_an_https_url", checks[0].Check)
		})
	}
}

func Test_Run_reportsAMissingClientId(t *testing.T) {
	checks := New().Run(t.Context(), Input{Issuer: "nonsense", ClientID: ""})

	require.True(t, HasBlocking(checks))
	require.Equal(t, "client_id_present", checks[0].Check)
}

func Test_HasBlocking(t *testing.T) {
	require.False(t, HasBlocking(nil))
	require.False(t, HasBlocking([]Check{{Level: LevelWarning}, {Level: LevelInfo}}))
	require.True(t, HasBlocking([]Check{{Level: LevelWarning}, {Level: LevelBlocking}}))
}
