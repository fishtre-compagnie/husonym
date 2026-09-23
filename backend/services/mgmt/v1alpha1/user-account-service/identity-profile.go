package v1alpha1_useraccountservice

import (
	"context"

	authjwt "github.com/fishtre-compagnie/husonym/backend/internal/auth/jwt"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
)

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
