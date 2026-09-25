package oidcprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/fishtre-compagnie/husonym/backend/internal/safehttp"
)

// Level is how much a finding weighs.
type Level int

const (
	LevelInfo Level = iota
	LevelWarning
	LevelBlocking
)

// Check is one finding: what was checked, how much it weighs, what was found, and what to
// do about it. A human fixes a provider from a remedy, never from a stack trace.
type Check struct {
	Check  string
	Level  Level
	Detail string
	Remedy string
}

// Input is the provider a caller wants to try.
type Input struct {
	// Issuer as the account declared it: the string its tokens carry in iss.
	Issuer string
	// ClientID, only checked for presence.
	ClientID string
	// AcceptedAlgorithms is what the deployment will validate signatures with. A provider
	// that signs with none of them cannot be used, however well it is configured.
	AcceptedAlgorithms []string
}

// Probe tries a provider without writing anything, and returns what it found.
//
// This is what makes "any compliant provider" true rather than merely claimed. Every check
// here is one that would otherwise be discovered after saving -- by an account whose
// members can no longer sign in.
type Probe struct {
	client *http.Client
	policy safehttp.Policy
}

// New returns a probe that fetches only where the policy lets it: by default a public
// https endpoint (see safehttp).
func New(policy safehttp.Policy) *Probe {
	return &Probe{client: newSafeClient(policy), policy: policy}
}

// discovery is the part of the OpenID Provider Metadata this needs.
type discovery struct {
	Issuer                           string   `json:"issuer"`
	JwksURI                          string   `json:"jwks_uri"`
	AuthorizationEndpoint            string   `json:"authorization_endpoint"`
	TokenEndpoint                    string   `json:"token_endpoint"`
	UserinfoEndpoint                 string   `json:"userinfo_endpoint"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
}

type jwks struct {
	Keys []struct {
		Kty string `json:"kty"`
		Alg string `json:"alg"`
		Use string `json:"use"`
	} `json:"keys"`
}

// Run performs the checks, in the order a failure makes the rest meaningless.
func (p *Probe) Run(ctx context.Context, in Input) []Check {
	checks := []Check{}

	if in.ClientID == "" {
		checks = append(checks, Check{
			Check:  "client_id_present",
			Level:  LevelBlocking,
			Detail: "no client id was given",
			Remedy: "register an application with the provider and copy its client id here",
		})
	}

	issuerURL, err := url.Parse(in.Issuer)
	if err != nil || issuerURL.Host == "" || p.policy.CheckIssuerURL(issuerURL) != nil {
		return append(checks, Check{
			Check:  "issuer_is_an_https_url",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("%q is not an issuer this deployment may use", in.Issuer),
			Remedy: "give the issuer exactly as the provider's tokens spell it, starting with https://, without query, fragment or credentials",
		})
	}

	doc, check := p.fetchDiscovery(ctx, in.Issuer)
	if check != nil {
		return append(checks, *check)
	}

	checks = append(checks, checkIssuerMatches(in.Issuer, doc)...)
	originChecks := checkJwksOrigin(issuerURL, doc)
	checks = append(checks, originChecks...)
	// Keys published elsewhere than on the issuer are not fetched: the document would
	// otherwise choose where this deployment sends a request.
	if len(originChecks) == 0 {
		checks = append(checks, p.checkKeys(ctx, doc)...)
	}
	checks = append(checks, checkAlgorithms(in.AcceptedAlgorithms, doc)...)
	checks = append(checks, checkEndpoints(doc)...)

	return checks
}

// discoveryURL builds the well-known URL from the issuer, the way the standard says: the
// path is appended to the issuer, not substituted for it.
func discoveryURL(issuer string) string {
	return strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
}

func (p *Probe) fetchDiscovery(ctx context.Context, issuer string) (*discovery, *Check) {
	resp, err := get(ctx, p.client, p.policy, discoveryURL(issuer))
	if err != nil {
		return nil, &Check{
			Check:  "discovery_document_reachable",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("could not read %s: %s", discoveryURL(issuer), err.Error()),
			Remedy: "check the issuer, and that this deployment can reach it over https",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &Check{
			Check:  "discovery_document_reachable",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("%s answered %s", discoveryURL(issuer), resp.Status),
			Remedy: "check the issuer: it is the base of the well-known URL, not the URL itself",
		}
	}

	var doc discovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&doc); err != nil {
		return nil, &Check{
			Check:  "discovery_document_is_json",
			Level:  LevelBlocking,
			Detail: "the discovery document is not readable JSON",
			Remedy: "check that the issuer points at the provider and not at a proxy or a login page",
		}
	}
	return &doc, nil
}

// checkIssuerMatches is the check that earns this whole endpoint.
//
// OpenID Connect Discovery requires the issuer in the document to equal the one it was
// fetched under. Microsoft Entra's common and organizations endpoints answer with the
// literal template "https://login.microsoftonline.com/{tenantid}/v2.0" -- a string no
// token will ever carry -- so an account configured that way would save cleanly and then
// refuse every sign-in. One comparison catches it, with a sentence that says what to do.
func checkIssuerMatches(issuer string, doc *discovery) []Check {
	if doc.Issuer == issuer {
		return nil
	}
	remedy := fmt.Sprintf("use %q as the issuer: it is what the provider calls itself", doc.Issuer)
	if strings.Contains(doc.Issuer, "{") {
		remedy = "this is a multi-tenant endpoint, which does not name an issuer. Use the URL of one tenant instead"
	}
	return []Check{{
		Check:  "issuer_matches_discovery",
		Level:  LevelBlocking,
		Detail: fmt.Sprintf("the provider calls itself %q, not %q", doc.Issuer, issuer),
		Remedy: remedy,
	}}
}

// checkJwksOrigin refuses now what the validator will refuse later.
//
// Key material is fetched from wherever the document points, so a document that points
// elsewhere is a document that chooses where this deployment sends requests. The validator
// is configured to require the same origin; finding that out here beats finding it out
// when nobody can sign in.
func checkJwksOrigin(issuerURL *url.URL, doc *discovery) []Check {
	if doc.JwksURI == "" {
		return []Check{{
			Check:  "jwks_uri_present",
			Level:  LevelBlocking,
			Detail: "the discovery document names no jwks_uri",
			Remedy: "the provider must publish its keys for its tokens to be validated",
		}}
	}
	jwksURL, err := url.Parse(doc.JwksURI)
	if err != nil {
		return []Check{{
			Check:  "jwks_uri_present",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("jwks_uri %q is not a URL", doc.JwksURI),
		}}
	}
	if jwksURL.Scheme != issuerURL.Scheme || jwksURL.Host != issuerURL.Host {
		return []Check{{
			Check:  "jwks_uri_same_origin",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("the keys are published on %s, not on %s", jwksURL.Host, issuerURL.Host),
			Remedy: "a provider must publish its keys under its own issuer for this deployment to trust them",
		}}
	}
	return nil
}

func (p *Probe) checkKeys(ctx context.Context, doc *discovery) []Check {
	if doc.JwksURI == "" {
		return nil // already reported
	}
	resp, err := get(ctx, p.client, p.policy, doc.JwksURI)
	if err != nil {
		return []Check{{
			Check:  "keys_readable",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("could not read the keys at %s: %s", doc.JwksURI, err.Error()),
		}}
	}
	defer resp.Body.Close()

	var set jwks
	if resp.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&set) != nil {
		return []Check{{
			Check:  "keys_readable",
			Level:  LevelBlocking,
			Detail: fmt.Sprintf("the keys at %s are not readable", doc.JwksURI),
		}}
	}

	for _, key := range set.Keys {
		// A symmetric key would mean a secret shared with the provider, which is a
		// signing key held by both parties rather than a provider vouching for anything.
		if key.Kty != "oct" && (key.Use == "" || key.Use == "sig") {
			return nil
		}
	}
	return []Check{{
		Check:  "keys_readable",
		Level:  LevelBlocking,
		Detail: "the provider publishes no public signing key",
		Remedy: "this deployment validates signatures with public keys only",
	}}
}

func checkAlgorithms(accepted []string, doc *discovery) []Check {
	if len(accepted) == 0 || len(doc.IDTokenSigningAlgValuesSupported) == 0 {
		return nil
	}
	for _, alg := range doc.IDTokenSigningAlgValuesSupported {
		if slices.Contains(accepted, alg) {
			return nil
		}
	}
	return []Check{{
		Check: "signature_algorithm_accepted",
		Level: LevelBlocking,
		Detail: fmt.Sprintf("the provider signs with %s, none of which this deployment accepts",
			strings.Join(doc.IDTokenSigningAlgValuesSupported, ", ")),
		Remedy: fmt.Sprintf("this deployment accepts %s", strings.Join(accepted, ", ")),
	}}
}

func checkEndpoints(doc *discovery) []Check {
	checks := []Check{}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		checks = append(checks, Check{
			Check:  "authorization_endpoints_present",
			Level:  LevelBlocking,
			Detail: "the discovery document names no authorization or token endpoint",
			Remedy: "signing in needs both",
		})
	}
	if doc.UserinfoEndpoint == "" {
		checks = append(checks, Check{
			Check:  "userinfo_endpoint_present",
			Level:  LevelWarning,
			Detail: "the provider publishes no userinfo endpoint",
			Remedy: "members will be listed without a name unless the tokens carry the profile claims themselves",
		})
	}
	return checks
}

// HasBlocking reports whether anything found would stop the setting from working.
func HasBlocking(checks []Check) bool {
	return slices.ContainsFunc(checks, func(c Check) bool { return c.Level == LevelBlocking })
}

// AuthorizationEndpoint returns where a sign-in starts for an issuer, discovered under the
// same bounds as everything else in this package.
//
// It is a separate entry point rather than a field of a stored setting, because an
// endpoint is the provider's to move: reading it each time is how "any compliant provider"
// stays true after the provider changes something.
func (p *Probe) AuthorizationEndpoint(ctx context.Context, issuer string) (string, error) {
	doc, check := p.fetchDiscovery(ctx, issuer)
	if check != nil {
		return "", fmt.Errorf("%s", check.Detail)
	}
	if doc.Issuer != issuer {
		return "", fmt.Errorf("the provider calls itself %q, not %q", doc.Issuer, issuer)
	}
	if doc.AuthorizationEndpoint == "" {
		return "", fmt.Errorf("the provider names no authorization endpoint")
	}
	if err := p.policy.CheckURL(mustParse(doc.AuthorizationEndpoint)); err != nil {
		return "", err
	}
	return doc.AuthorizationEndpoint, nil
}

func mustParse(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return parsed
}
