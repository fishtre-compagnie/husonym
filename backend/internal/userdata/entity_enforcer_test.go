package userdata

import (
	"context"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/auth/permission"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
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

// Every method answers the same way: an account key by its scope, a worker key and a person as
// before. The two shapes of each — the one that refuses and the one that says yes or no — are
// held to the same answer, so that one of them cannot quietly say yes.
func Test_UserEntityEnforcer_EveryMethod(t *testing.T) {
	ctx := context.Background()
	entity := NewWildcardDomainEntity("account")
	account := NewIdentifier("account")
	scope := permission.NewScope([]string{"job:view", "connection:view", "account:view"})

	type method struct {
		name    string
		enforce func(*UserEntityEnforcer) error
		allowed func(*UserEntityEnforcer) (bool, error)
		inScope bool
	}
	methods := []method{
		{"job view", func(u *UserEntityEnforcer) error { return u.EnforceJob(ctx, entity, rbac.JobAction_View) },
			func(u *UserEntityEnforcer) (bool, error) { return u.Job(ctx, entity, rbac.JobAction_View) }, true},
		{"job execute", func(u *UserEntityEnforcer) error { return u.EnforceJob(ctx, entity, rbac.JobAction_Execute) },
			func(u *UserEntityEnforcer) (bool, error) { return u.Job(ctx, entity, rbac.JobAction_Execute) }, false},
		{"connection view", func(u *UserEntityEnforcer) error {
			return u.EnforceConnection(ctx, entity, rbac.ConnectionAction_View)
		}, func(u *UserEntityEnforcer) (bool, error) {
			return u.Connection(ctx, entity, rbac.ConnectionAction_View)
		}, true},
		{"connection secrets", func(u *UserEntityEnforcer) error {
			return u.EnforceConnection(ctx, entity, rbac.ConnectionAction_ViewSensitive)
		}, func(u *UserEntityEnforcer) (bool, error) {
			return u.Connection(ctx, entity, rbac.ConnectionAction_ViewSensitive)
		}, false},
		{"account view", func(u *UserEntityEnforcer) error { return u.EnforceAccount(ctx, account, rbac.AccountAction_View) },
			func(u *UserEntityEnforcer) (bool, error) { return u.Account(ctx, account, rbac.AccountAction_View) }, true},
		{"account edit", func(u *UserEntityEnforcer) error { return u.EnforceAccount(ctx, account, rbac.AccountAction_Edit) },
			func(u *UserEntityEnforcer) (bool, error) { return u.Account(ctx, account, rbac.AccountAction_Edit) }, false},
	}
	callers := map[string]struct {
		enforcer  *UserEntityEnforcer
		anyAction bool // allowed whatever the scope
	}{
		"account key": {&UserEntityEnforcer{
			enforcer: rbac.NewAllowAllClient(), user: rbac.NewUserIdEntity("key"),
			enforceAccountAccess: func(context.Context, string) error { return nil },
			isApiKey:             true, keyScope: &scope,
		}, false},
		"worker key": {&UserEntityEnforcer{
			enforcer: rbac.NewAllowAllClient(), user: rbac.NewUserIdEntity("worker"),
			enforceAccountAccess: func(context.Context, string) error { return nil },
			isApiKey:             true,
		}, true},
		"person": {&UserEntityEnforcer{
			enforcer: rbac.NewAllowAllClient(), user: rbac.NewUserIdEntity("person"),
			enforceAccountAccess: func(context.Context, string) error { return nil },
		}, true},
	}
	for callerName, caller := range callers {
		for _, m := range methods {
			t.Run(callerName+", "+m.name, func(t *testing.T) {
				want := caller.anyAction || m.inScope
				allowed, err := m.allowed(caller.enforcer)
				require.NoError(t, err)
				require.Equal(t, want, allowed)
				if want {
					require.NoError(t, m.enforce(caller.enforcer))
				} else {
					require.ErrorContains(t, m.enforce(caller.enforcer), "lacks the permission")
				}
			})
		}
	}
}

// The scope comes from the key the request carries: an account key's permissions, nothing for a
// worker key or a person.
func Test_accountKeyScope(t *testing.T) {
	require.Nil(t, accountKeyScope(nil))
	require.Nil(t, accountKeyScope(&auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey}))

	scope := accountKeyScope(&auth_apikey.TokenContextData{
		ApiKeyType: apikey.AccountApiKey,
		ApiKey:     &db_queries.HusonymApiAccountApiKey{Permissions: []string{"job:view"}},
	})
	require.NotNil(t, scope)
	require.True(t, scope.Allows(mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW))
	require.False(t, scope.Allows(mgmtv1alpha1.Permission_PERMISSION_JOB_EXECUTE))

	// An account key with no permission is scoped to nothing — never mistaken for a worker key.
	empty := accountKeyScope(&auth_apikey.TokenContextData{
		ApiKeyType: apikey.AccountApiKey,
		ApiKey:     &db_queries.HusonymApiAccountApiKey{},
	})
	require.NotNil(t, empty)
	require.False(t, empty.Allows(mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW))
}
