package auth_jwt

import (
	"context"
	"encoding/json"
	"fmt"
)

// CustomClaims contains custom data we want from the token.
//
// The display claims (Name, Email, EmailVerified, Picture) are the standard OIDC ones.
// They are not always there: an access token is not an ID token, and most providers keep
// the profile for the userinfo endpoint. Their absence is normal and is not an error --
// see HasProfileClaims.
type CustomClaims struct {
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions,omitempty"`

	Name          string      `json:"name,omitempty"`
	Email         *string     `json:"email,omitempty"`
	EmailVerified LenientBool `json:"email_verified,omitempty"`
	Picture       string      `json:"picture,omitempty"`
}

// Validate does nothing for this example, but we need
// it to satisfy validator.CustomClaims interface.
//
// The receiver is a pointer because the validator is handed one (WithCustomClaims returns
// *CustomClaims), and because the struct is no longer small enough to copy per token.
func (c *CustomClaims) Validate(ctx context.Context) error {
	return nil
}

// HasProfileClaims reports whether the token carries enough of a profile to be worth
// storing, which spares a call to the userinfo endpoint.
//
// The address is what decides. A name without an address is a label nobody can act on,
// and a provider that sends the address sends the rest with it.
func (c *CustomClaims) HasProfileClaims() bool {
	return c != nil && c.Email != nil && *c.Email != ""
}

// LenientBool is a boolean claim that also accepts the string spelling of one.
//
// This is not defensive decoration. The validator unmarshals the whole payload into
// CustomClaims and rejects the token on any error, so a single claim a provider spells
// "true" instead of true would lock every user of that deployment out -- and the payload
// comes from a provider Husonym does not choose. Only the claim it is used for degrades,
// and it degrades to false, which is the absence of proof.
type LenientBool bool

func (b *LenientBool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = LenientBool(asBool)
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err != nil {
		return fmt.Errorf("claim is neither a boolean nor a string: %w", err)
	}
	*b = asString == "true"
	return nil
}
