package v1alpha1_useraccountservice

import (
	"context"
	"errors"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
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

// errOrganizationKeepsAnAdmin refuses the change that would leave the organization of the
// instance with no administrator: it is the only account of the people of the instance, and
// nobody could administer it again from the product.
func errOrganizationKeepsAnAdmin() error {
	return husonymerrors.NewFailedPrecondition(
		"the organization of this instance must keep an administrator: make another member an administrator first",
	)
}

// setRoleKeepingAnAdmin is setRole for the organization of the instance: the role is not changed
// when the member is its only administrator and the role is another.
func (s *Service) setRoleKeepingAnAdmin(ctx context.Context, user rbac.User, account rbac.Account, role mgmtv1alpha1.AccountRole) error {
	err := s.rbacClient.SetRoleKeepingAnAdmin(ctx, user, account, role)
	if errors.Is(err, rbac.ErrLastAdmin) {
		return errOrganizationKeepsAnAdmin()
	}
	if errors.Is(err, rbac.ErrRoleNotReadBack) {
		warnRoleNotReadBack(ctx, user, account, role, err)
		return nil
	}
	return err
}

// removeRolesKeepingAnAdmin takes the roles of a member of the organization of the instance
// away, unless they are its only administrator. Roles taken away in the table and still held on
// this instance are taken away: they are no longer held here once the roles are read again,
// within seconds, and a role grants nothing to who is no member, which the caller sees to next.
func (s *Service) removeRolesKeepingAnAdmin(ctx context.Context, user rbac.User, account rbac.Account) error {
	err := s.rbacClient.RemoveMemberKeepingAnAdmin(ctx, user, account)
	if errors.Is(err, rbac.ErrLastAdmin) {
		return errOrganizationKeepsAnAdmin()
	}
	if errors.Is(err, rbac.ErrRoleNotReadBack) {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
			ctx,
			"the roles of a member are taken away, and are no longer held on this instance once the roles are read again",
			"userId", user.String(), "accountId", account.String(), "error", err,
		)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to remove account user from rbac engine: %w", err)
	}
	return nil
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
