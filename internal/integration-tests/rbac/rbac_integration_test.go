package rbac

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	testcontainers_postgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// migratedDatabase starts a PostgreSQL holding the schema of the API.
func migratedDatabase(ctx context.Context, t *testing.T) *testcontainers_postgres.PostgresTestContainer {
	t.Helper()
	container, err := testcontainers_postgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../../backend/sql/postgresql/schema", testutil.GetTestLogger(t)))
	return container
}

func someone() rbac.User        { return rbac.NewUser(uuid.NewString()) }
func someAccount() rbac.Account { return rbac.NewAccount(uuid.NewString()) }

func TestRbac(t *testing.T) {
	t.Parallel()
	ok := testutil.ShouldRunIntegrationTest()
	if !ok {
		return
	}

	ctx := t.Context()
	container := migratedDatabase(ctx, t)

	rbacclient, err := rbac.New(ctx, container.DB, testutil.GetTestLogger(t))
	require.NoError(t, err)

	// memberWith gives somebody a role in an account.
	memberWith := func(t *testing.T, account rbac.Account, role mgmtv1alpha1.AccountRole) rbac.User {
		t.Helper()
		member := someone()
		require.NoError(t, rbacclient.SetRole(ctx, member, account, role))
		return member
	}
	refused := func(t *testing.T, member rbac.User, account rbac.Account, action rbac.Action, message string) {
		t.Helper()
		err := rbacclient.Enforce(ctx, member, account, action)
		require.Error(t, err)
		require.ErrorContains(t, err, message)
	}

	t.Run("account_admin", func(t *testing.T) {
		t.Parallel()
		account := someAccount()
		member := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)

		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.AccountAction_View))
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.AccountAction_Edit))
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.JobAction_Create))
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.ConnectionAction_Create))
	})

	t.Run("job_developer", func(t *testing.T) {
		t.Parallel()
		account := someAccount()
		member := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER)

		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.AccountAction_View))
		refused(t, member, account, rbac.AccountAction_Edit, "user does not have permission to edit account")
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.JobAction_Create))
	})

	t.Run("job_executor", func(t *testing.T) {
		t.Parallel()
		account := someAccount()
		member := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_EXECUTOR)

		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.AccountAction_View))
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.JobAction_View))
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.JobAction_Execute))
		refused(t, member, account, rbac.JobAction_Create, "user does not have permission to create job")
	})

	t.Run("job_viewer", func(t *testing.T) {
		t.Parallel()
		account := someAccount()
		member := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)

		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.AccountAction_View))
		require.NoError(t, rbacclient.Enforce(ctx, member, account, rbac.JobAction_View))
		refused(t, member, account, rbac.JobAction_Execute, "user does not have permission to execute job")
		refused(t, member, account, rbac.JobAction_Create, "user does not have permission to create job")
	})

	t.Run("role_changes", func(t *testing.T) {
		t.Parallel()
		account := someAccount()
		member := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)

		require.NoError(t, rbacclient.RemoveMember(ctx, member, account))

		refused(t, member, account, rbac.AccountAction_View, "user does not have permission to view account")
	})

	t.Run("cross_account_access", func(t *testing.T) {
		t.Parallel()
		account1, account2 := someAccount(), someAccount()
		user1 := memberWith(t, account1, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)
		memberWith(t, account2, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)

		refused(t, user1, account2, rbac.AccountAction_View, "user does not have permission to view account")
		refused(t, user1, account2, rbac.JobAction_View, "user does not have permission to view job")
		refused(t, user1, account2, rbac.ConnectionAction_View, "user does not have permission to view connection")
	})

	t.Run("mixed_roles_same_account", func(t *testing.T) {
		t.Parallel()
		account := someAccount()
		adminUser := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)
		viewerUser := memberWith(t, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)

		require.NoError(t, rbacclient.Enforce(ctx, adminUser, account, rbac.JobAction_Create))
		require.NoError(t, rbacclient.Enforce(ctx, adminUser, account, rbac.JobAction_Delete))
		require.NoError(t, rbacclient.Enforce(ctx, adminUser, account, rbac.ConnectionAction_Create))

		require.NoError(t, rbacclient.Enforce(ctx, viewerUser, account, rbac.JobAction_View))
		refused(t, viewerUser, account, rbac.JobAction_Execute, "user does not have permission to execute job")
		refused(t, viewerUser, account, rbac.JobAction_Create, "user does not have permission to create job")
		refused(t, viewerUser, account, rbac.JobAction_Delete, "user does not have permission to delete job")
		refused(t, viewerUser, account, rbac.JobAction_Edit, "user does not have permission to edit job")
	})
}
