package v1alpha1_useraccountservice

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/tokenctx"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/jackc/pgx/v5/pgtype"
)

// isInstanceIdentity reports whether the caller is a person the provider of the deployment
// vouches for: the only ones the organization of the instance is for. An API key is not one,
// nor is an identity another provider issued, nor is anybody when authentication is off.
func (s *Service) isInstanceIdentity(ctx context.Context) bool {
	if !s.cfg.IsAuthEnabled {
		return false
	}
	tokenctxResp, err := tokenctx.GetTokenCtx(ctx)
	if err != nil || tokenctxResp.JwtContextData == nil {
		return false
	}
	issuer, _ := tokenctxResp.JwtContextData.Identity()
	return s.isDeploymentIssuer(issuer)
}

// EnterInstance brings the caller into the organization of the instance, and answers the account
// they land in. Whoever the organization is not for, and whoever enters an instance that has
// accounts and retains none, gets what SetPersonalAccount gives, from SetPersonalAccount itself.
func (s *Service) EnterInstance(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.EnterInstanceRequest],
) (*connect.Response[mgmtv1alpha1.EnterInstanceResponse], error) {
	if !s.isInstanceIdentity(ctx) {
		return s.enterPersonalAccount(ctx)
	}

	user, err := s.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	if err != nil {
		return nil, err
	}
	userId, err := husonymdb.ToUuid(user.Msg.GetUserId())
	if err != nil {
		return nil, err
	}

	entry, err := s.db.EnterInstance(ctx, userId, instanceRoles{service: s})
	if errors.Is(err, husonymdb.ErrInstanceOrganizationMissing) {
		// The error names the account that is retained and gone. Nobody is let in elsewhere
		// meanwhile: a second organization would be worse than a refusal.
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).ErrorContext(
			ctx,
			"the account this instance retains as its organization no longer exists, nobody can enter it",
			"error", err,
		)
		return nil, husonymerrors.NewInternalError("unable to enter the organization of this instance")
	}
	if err != nil {
		return nil, err
	}
	if entry.Outcome == husonymdb.EntryPersonal {
		return s.enterPersonalAccount(ctx)
	}
	return connect.NewResponse(&mgmtv1alpha1.EnterInstanceResponse{
		AccountId: husonymdb.UUIDString(entry.AccountId),
	}), nil
}

// enterPersonalAccount is the entry of who no organization applies to.
func (s *Service) enterPersonalAccount(
	ctx context.Context,
) (*connect.Response[mgmtv1alpha1.EnterInstanceResponse], error) {
	personal, err := s.SetPersonalAccount(ctx, connect.NewRequest(&mgmtv1alpha1.SetPersonalAccountRequest{}))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.EnterInstanceResponse{
		AccountId: personal.Msg.GetAccountId(),
	}), nil
}

// instanceRoles gives an entry its roles through the service, so that a role stored and not
// read back yet is a warning there as it is everywhere else.
type instanceRoles struct {
	service *Service
}

var _ husonymdb.InstanceRoles = instanceRoles{}

func (r instanceRoles) GrantAdmin(ctx context.Context, userId, accountId pgtype.UUID) error {
	return r.service.setRole(
		ctx,
		rbac.NewPgUser(userId),
		rbac.NewAccount(husonymdb.UUIDString(accountId)),
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
	)
}

func (r instanceRoles) GrantViewerIfNone(ctx context.Context, userId, accountId pgtype.UUID) error {
	user, account := rbac.NewPgUser(userId), rbac.NewAccount(husonymdb.UUIDString(accountId))
	err := r.service.rbacClient.GrantViewerIfNone(ctx, user, account)
	if errors.Is(err, rbac.ErrRoleNotReadBack) {
		warnRoleNotReadBack(ctx, user, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER, err)
		return nil
	}
	return err
}

// SetInstanceOrganization has the instance retain an account as its organization, once. It is a
// person's gesture: one the provider of the deployment vouches for, who may edit the account.
func (s *Service) SetInstanceOrganization(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetInstanceOrganizationRequest],
) (*connect.Response[mgmtv1alpha1.SetInstanceOrganizationResponse], error) {
	if !s.cfg.IsAuthEnabled {
		return nil, husonymerrors.NewForbidden(
			"unable to set the organization of the instance as authentication is not enabled",
		)
	}
	if !s.isInstanceIdentity(ctx) {
		return nil, husonymerrors.NewForbidden(
			"the organization of the instance is set by a person signed in with the identity provider of the deployment",
		)
	}

	user, err := s.UserDataClient().GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}
	accountId, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	account, err := s.db.DesignateInstanceOrganization(ctx, user.PgId(), accountId, req.Msg.GetName())
	if errors.Is(err, husonymdb.ErrInstanceOrganizationSet) {
		return nil, husonymerrors.NewFailedPrecondition(err.Error())
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.SetInstanceOrganizationResponse{
		AccountId: husonymdb.UUIDString(account.ID),
	}), nil
}

// instanceOrganizationId gives the account the instance retains as its organization, or nothing
// when it retains none. It gives nothing either when that cannot be read, and says so in the
// log: what asks it answers without authentication, and has to keep answering.
func (s *Service) instanceOrganizationId(ctx context.Context) *string {
	organization, retained, err := s.db.GetInstanceOrganization(ctx)
	if err != nil {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).ErrorContext(
			ctx, "unable to read the organization of the instance", "error", err,
		)
		return nil
	}
	if !retained {
		return nil
	}
	id := husonymdb.UUIDString(organization)
	return &id
}
