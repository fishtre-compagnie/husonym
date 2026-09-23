package v1alpha1_useraccountservice

import (
	"context"

	authjwt "github.com/fishtre-compagnie/husonym/backend/internal/auth/jwt"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// isDisplayIdentityComplete reports whether every field a members list shows is filled,
// which is what decides that no second source has to be asked.
func isDisplayIdentityComplete(u *authmgmt.User) bool {
	return u != nil && u.Name != "" && u.Email != "" && u.Picture != ""
}

// completeDisplayIdentity fills the blanks of a stored display identity from a fallback
// source, and only the blanks.
//
// The three fields are written independently at sign-in -- a token that carries an
// address but no name leaves exactly that -- so the question is never "was a profile
// stored" but "which fields are still blank". The fallback may only ever add: what the
// provider asserted at sign-in is the fresher statement and wins.
func completeDisplayIdentity(stored, fallback *authmgmt.User) *authmgmt.User {
	if fallback == nil {
		return stored
	}
	if stored == nil {
		return fallback
	}
	return &authmgmt.User{
		Name:          firstNonEmpty(stored.Name, fallback.Name),
		Email:         firstNonEmpty(stored.Email, fallback.Email),
		EmailVerified: stored.EmailVerified,
		Picture:       firstNonEmpty(stored.Picture, fallback.Picture),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// resolveIdentityProfile returns the display identity the provider presents for the
// signed-in subject, taken from the standard OIDC claims.
//
// Two sources, in this order, and both are the norm rather than a product:
//
//  1. the claims of the token itself, when it carries them;
//  2. the provider's userinfo endpoint, found through its discovery document.
//
// It returns nil rather than an error when neither answers. A provider that sends no
// profile is a provider Husonym still has to let in -- the members list then shows what
// the deployment's administration API can still give it, or nothing.
func (s *Service) resolveIdentityProfile(
	ctx context.Context,
	tokenCtxData *authjwt.TokenContextData,
) *authmgmt.User {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)

	if tokenCtxData == nil {
		return nil
	}

	if claims := tokenCtxData.Claims; claims.HasProfileClaims() {
		return &authmgmt.User{
			Name:          claims.Name,
			Email:         *claims.Email,
			EmailVerified: bool(claims.EmailVerified),
			Picture:       claims.Picture,
		}
	}

	userinfo, err := s.authclient.GetUserInfo(ctx, tokenCtxData.RawToken)
	if err != nil {
		// Not an error for the caller: signing in must not depend on the provider
		// answering a second request.
		logger.Warn("unable to retrieve the profile from the userinfo endpoint: " + err.Error())
		return nil
	}
	return &authmgmt.User{
		Name:          userinfo.Name,
		Email:         userinfo.Email,
		EmailVerified: bool(userinfo.EmailVerified),
		Picture:       userinfo.Picture,
	}
}

// identityOf turns a validated token into the pair that identifies its bearer.
//
// MayAdoptLegacy is true only for the deployment's own issuer. Identities recorded before
// issuers were belong to whoever the deployment was pointed at then; letting any other
// issuer claim one would hand it the users it names.
func (s *Service) identityOf(tokenCtxData *authjwt.TokenContextData) husonymdb.Identity {
	issuer, subject := tokenCtxData.Identity()
	return husonymdb.Identity{
		Issuer:  issuer,
		Subject: subject,
		// An empty deployment issuer adopts nothing: it means the deployment has no
		// issuer configured, so there is no "own" issuer to be.
		MayAdoptLegacy: s.cfg.DeploymentIssuer != "" && issuer == s.cfg.DeploymentIssuer,
	}
}

// refuseApplicationToken rejects a token that has no human behind it, before it can bring
// a user into existence.
//
// A daemon holding client credentials for this API gets a token whose subject is the
// service principal, not a person. Signing that in would mint a Husonym user -- and a
// personal account with it -- for every such application in the tenant. The claim is
// Entra's; elsewhere it is absent, which is why this reads as "refuse what says it is an
// application" and not "require what says it is a person": a provider that says nothing
// must keep working.
func (s *Service) refuseApplicationToken(tokenCtxData *authjwt.TokenContextData) error {
	if tokenCtxData.Claims.IsApplicationToken() {
		return husonymerrors.NewForbidden(
			"this token was issued to an application rather than to a person, and cannot be used to create a user",
		)
	}
	return nil
}
