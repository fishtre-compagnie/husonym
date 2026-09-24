package userdata

import (
	"context"
	"testing"

	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/stretchr/testify/require"
)

// An API key is answered by its scope, never by the RBAC: here the RBAC allows everything, as it
// does without a license, and the scope still narrows.
func Test_UserEntityEnforcer_AnswersAKeyByItsScope(t *testing.T) {
	ctx := context.Background()
	scope := permission.NewScope([]string{"connection:view", "job:view"})
	enforcer := func(isApiKey bool, keyScope *permission.Scope) *UserEntityEnforcer {
		return &UserEntityEnforcer{
			enforcer:             rbac.NewAllowAllClient(),
			user:                 rbac.NewUserIdEntity("user"),
			enforceAccountAccess: func(context.Context, string) error { return nil },
			isApiKey:             isApiKey,
			keyScope:             keyScope,
		}
	}
	connection := NewWildcardDomainEntity("account")
	account := NewIdentifier("account")

	t.Run("an account key holds what its scope grants", func(t *testing.T) {
		key := enforcer(true, &scope)
		require.NoError(t, key.EnforceConnection(ctx, connection, rbac.ConnectionAction_View))
		require.NoError(t, key.EnforceJob(ctx, connection, rbac.JobAction_View))
	})

	t.Run("and nothing else, told what it lacks", func(t *testing.T) {
		key := enforcer(true, &scope)
		require.ErrorContains(t, key.EnforceJob(ctx, connection, rbac.JobAction_Execute), "job:execute")
		require.ErrorContains(t, key.EnforceAccount(ctx, account, rbac.AccountAction_Edit), "account:edit")
	})

	t.Run("a connection's secrets come back masked to a key that may only view it", func(t *testing.T) {
		key := enforcer(true, &scope)
		sensitive, err := key.Connection(ctx, connection, rbac.ConnectionAction_ViewSensitive)
		require.NoError(t, err)
		require.False(t, sensitive)
		viewable, err := key.Connection(ctx, connection, rbac.ConnectionAction_View)
		require.NoError(t, err)
		require.True(t, viewable)
	})

	t.Run("a worker key is bounded by its procedures, not by a scope", func(t *testing.T) {
		worker := enforcer(true, nil)
		require.NoError(t, worker.EnforceJob(ctx, connection, rbac.JobAction_Execute))
	})

	t.Run("a person is answered by the RBAC", func(t *testing.T) {
		person := enforcer(false, nil)
		require.NoError(t, person.EnforceJob(ctx, connection, rbac.JobAction_Execute))
	})
}
