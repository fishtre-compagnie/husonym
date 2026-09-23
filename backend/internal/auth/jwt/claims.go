package auth_jwt

import (
	"context"
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
// That is what utils.LenientBool is for.
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
