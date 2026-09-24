package userdata

import (
	"context"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
)

type UserEntityEnforcer struct {
	enforcer             rbac.EntityEnforcer
	user                 rbac.EntityString
	enforceAccountAccess func(ctx context.Context, accountId string) error
	isApiKey             bool
	// keyScope is what an account API key was granted; nil for a person, and for a worker key,
	// which its own list of procedures bounds. A key is answered by its scope, not by the RBAC:
	// its scope was capped by its creator's rights when it was made.
	keyScope *permission.Scope
}

var _ EntityEnforcer = (*UserEntityEnforcer)(nil)

// Higher level entity enforcer that slightly abstracts away the lower level rbac interface
// The intention here is to be able to use objects that are closer to the mgmt domain model
// rather than the lower level rbac model
// see the entity.go file for functions that help with this
type EntityEnforcer interface {
	EnforceJob(ctx context.Context, job DomainEntity, action rbac.JobAction) error
	Job(ctx context.Context, job DomainEntity, action rbac.JobAction) (bool, error)
	EnforceConnection(
		ctx context.Context,
		connection DomainEntity,
		action rbac.ConnectionAction,
	) error
	Connection(
		ctx context.Context,
		connection DomainEntity,
		action rbac.ConnectionAction,
	) (bool, error)
	EnforceAccount(ctx context.Context, account Identifier, action rbac.AccountAction) error
	Account(ctx context.Context, account Identifier, action rbac.AccountAction) (bool, error)
}

func (u *UserEntityEnforcer) EnforceJob(
	ctx context.Context,
	job DomainEntity,
	action rbac.JobAction,
) error {
	if err := u.enforceAccountAccess(ctx, job.GetAccountId()); err != nil {
		return err
	}
	if u.isApiKey {
		return u.keyRequire(permission.Job(action))
	}
	return u.enforcer.EnforceJob(
		ctx,
		u.user,
		rbac.NewAccountIdEntity(job.GetAccountId()),
		rbac.NewJobIdEntity(job.GetId()),
		action,
	)
}

func (u *UserEntityEnforcer) Job(
	ctx context.Context,
	job DomainEntity,
	action rbac.JobAction,
) (bool, error) {
	if err := u.enforceAccountAccess(ctx, job.GetAccountId()); err != nil {
		return false, err
	}
	if u.isApiKey {
		return u.keyAllows(permission.Job(action)), nil
	}
	return u.enforcer.Job(
		ctx,
		u.user,
		rbac.NewAccountIdEntity(job.GetAccountId()),
		rbac.NewJobIdEntity(job.GetId()),
		action,
	)
}

func (u *UserEntityEnforcer) EnforceConnection(
	ctx context.Context,
	connection DomainEntity,
	action rbac.ConnectionAction,
) error {
	if err := u.enforceAccountAccess(ctx, connection.GetAccountId()); err != nil {
		return err
	}
	if u.isApiKey {
		return u.keyRequire(permission.Connection(action))
	}
	return u.enforcer.EnforceConnection(
		ctx,
		u.user,
		rbac.NewAccountIdEntity(connection.GetAccountId()),
		rbac.NewConnectionIdEntity(connection.GetId()),
		action,
	)
}

func (u *UserEntityEnforcer) Connection(
	ctx context.Context,
	connection DomainEntity,
	action rbac.ConnectionAction,
) (bool, error) {
	if err := u.enforceAccountAccess(ctx, connection.GetAccountId()); err != nil {
		return false, err
	}
	if u.isApiKey {
		return u.keyAllows(permission.Connection(action)), nil
	}
	return u.enforcer.Connection(
		ctx,
		u.user,
		rbac.NewAccountIdEntity(connection.GetAccountId()),
		rbac.NewConnectionIdEntity(connection.GetId()),
		action,
	)
}

func (u *UserEntityEnforcer) EnforceAccount(
	ctx context.Context,
	account Identifier,
	action rbac.AccountAction,
) error {
	if err := u.enforceAccountAccess(ctx, account.GetId()); err != nil {
		return err
	}
	if u.isApiKey {
		return u.keyRequire(permission.Account(action))
	}
	return u.enforcer.EnforceAccount(ctx, u.user, rbac.NewAccountIdEntity(account.GetId()), action)
}

func (u *UserEntityEnforcer) Account(
	ctx context.Context,
	account Identifier,
	action rbac.AccountAction,
) (bool, error) {
	if err := u.enforceAccountAccess(ctx, account.GetId()); err != nil {
		return false, err
	}
	if u.isApiKey {
		return u.keyAllows(permission.Account(action)), nil
	}
	return u.enforcer.Account(ctx, u.user, rbac.NewAccountIdEntity(account.GetId()), action)
}

// keyRequire answers for an API key: a worker key passes, as its list of procedures bounds it;
// an account key holds what its scope grants, and is told what it lacks.
func (u *UserEntityEnforcer) keyRequire(p mgmtv1alpha1.Permission) error {
	if u.keyScope == nil {
		return nil
	}
	return u.keyScope.Require(p)
}

func (u *UserEntityEnforcer) keyAllows(p mgmtv1alpha1.Permission) bool {
	return u.keyScope == nil || u.keyScope.Allows(p)
}
