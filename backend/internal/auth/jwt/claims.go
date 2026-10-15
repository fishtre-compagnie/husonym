package auth_jwt

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/fishtre-compagnie/husonym/backend/internal/utils"
)

// CustomClaims contains custom data we want from the token.
//
// The display claims (Name, Email, EmailVerified, Picture) are the standard OIDC ones.
// They are not always there: an access token is not an ID token, and most providers keep
// the profile for the userinfo endpoint. Their absence is normal and is not an error --
// see HasProfileClaims.
//
// The validator unmarshals the whole token payload into this struct and rejects the token
// on any error, so every field added here has to read whatever a provider may write.
// That is what utils.LenientBool and entraFlag are for.
type CustomClaims struct {
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions,omitempty"`

	// IdentityType is Entra's idtyp claim, the only in-band way a provider states that a
	// token was issued to an application rather than to a person. Absent everywhere else,
	// which is the point: see IsApplicationToken.
	IdentityType string `json:"idtyp,omitempty"`

	Name          string            `json:"name,omitempty"`
	Email         *string           `json:"email,omitempty"`
	EmailVerified utils.LenientBool `json:"email_verified,omitempty"`
	Picture       string            `json:"picture,omitempty"`

	// EmailDomainOwnerVerified is Entra's xms_edov optional claim: the domain of the address
	// belongs to the tenant, which is what Entra says in place of email_verified, a claim it
	// never emits. Absent everywhere else.
	EmailDomainOwnerVerified entraFlag `json:"xms_edov,omitempty"`
}

// entraFlag reads the boolean Entra writes in its own claims, which comes as true, "true",
// 1 or "1" depending on the token. Anything else is false, and never an error: the validator
// rejects the whole token on one, and a sign-in must not hang on the shape of a claim.
//
// It is not utils.LenientBool on purpose: that one reads email_verified, and "1" there is
// not a statement of a provider that means it.
type entraFlag bool

func (f *entraFlag) UnmarshalJSON(data []byte) error {
	*f = false
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	switch v := raw.(type) {
	case bool:
		*f = entraFlag(v)
	case float64:
		*f = entraFlag(v == 1)
	case string:
		*f = entraFlag(v == "1" || strings.EqualFold(v, "true"))
	}
	return nil
}

// IsEmailVerified reports whether the provider vouches for the address of the token: the
// standard email_verified claim, or, for Entra, the ownership of the domain of the address by
// the tenant (xms_edov).
//
// The address has to be in the token: a statement about an address nobody gave is not one, and
// Entra only returns xms_edov next to email, which its application therefore has to ask for as
// well. Without it the profile comes from the userinfo endpoint, which says nothing of the kind.
//
// It vouches for the address, never for the person -- identity stays the pair (issuer,
// subject), and an invitation is only accepted from the issuer it was made under. Entra
// tells nothing about an address whose domain the tenant does not own, which keeps out the
// guests of other organizations. Whoever administers the tenant, or its directory
// synchronisation, can still write an address of a verified domain: that trust is the tenant's.
func (c *CustomClaims) IsEmailVerified() bool {
	if !c.HasProfileClaims() {
		return false
	}
	return bool(c.EmailVerified) || bool(c.EmailDomainOwnerVerified)
}

// Validate does nothing for this example, but we need
// it to satisfy validator.CustomClaims interface.
//
// The receiver is a pointer because the validator is handed one (WithCustomClaims returns
// *CustomClaims), and because the struct is no longer small enough to copy per token.
func (c *CustomClaims) Validate(ctx context.Context) error {
	return nil
}

// IsApplicationToken reports whether the token says it was issued to an application
// rather than to a person.
//
// It reads a positive statement and never infers one. A provider that says nothing is a
// provider whose users must keep signing in -- guessing from the absence of a scope or a
// profile claim would lock out anyone whose access token is merely sparse.
func (c *CustomClaims) IsApplicationToken() bool {
	return c != nil && strings.EqualFold(c.IdentityType, "app")
}

// HasProfileClaims reports whether the token carries enough of a profile to be worth
// storing, which spares a call to the userinfo endpoint.
//
// The address is what decides. A name without an address is a label nobody can act on,
// and a provider that sends the address sends the rest with it.
func (c *CustomClaims) HasProfileClaims() bool {
	return c != nil && c.Email != nil && *c.Email != ""
}
