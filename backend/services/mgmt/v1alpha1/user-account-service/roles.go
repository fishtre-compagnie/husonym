package v1alpha1_useraccountservice

import (
	"context"
	"errors"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
)

// setRole gives a member a role in an account. A role that is stored and that this instance
// could not read back is a role given: the member holds it here once the roles are read again,
// within seconds, so the request goes on, and a warning tells it. Until then this instance
// answers the member from the role held before.
func (s *Service) setRole(ctx context.Context, user rbac.User, account rbac.Account, role mgmtv1alpha1.AccountRole) error {
	err := s.rbacClient.SetRole(ctx, user, account, role)
	if errors.Is(err, rbac.ErrRoleNotReadBack) {
		warnRoleNotReadBack(ctx, user, account, role, err)
		return nil
	}
	return err
}

// warnRoleNotReadBack tells that a role is stored and not held on this instance yet.
func warnRoleNotReadBack(ctx context.Context, user rbac.User, account rbac.Account, role mgmtv1alpha1.AccountRole, err error) {
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
		ctx,
		"the role of a member is stored, and is held on this instance once the roles are read again",
		"userId", user.String(), "accountId", account.String(), "role", role.String(), "error", err,
	)
}

// enforceRbacForRole asks for the rbac feature when a role other than administrator is being
// given. Administrator is the way back to every member being one, and a role not named is not
// given. Roles already assigned keep applying whatever the license: nothing here, and nothing in
// the enforcement, reads it.
func enforceRbacForRole(ctx context.Context, user *userdata.User, accountId string, role mgmtv1alpha1.AccountRole) error {
	if role == mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED || role == mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN {
		return nil
	}
	return user.EnforceFeature(ctx, accountId, license.FeatureRbac)
}
