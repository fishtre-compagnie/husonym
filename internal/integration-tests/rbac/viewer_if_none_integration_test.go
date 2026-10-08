package rbac

import (
	"context"
	"sync"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// rolesStoredFor gives the roles the table holds for a person in an account.
func rolesStoredFor(ctx context.Context, t *testing.T, db *pgxpool.Pool, userId, accountId string) []string {
	t.Helper()
	var roles []string
	for _, row := range storedRows(ctx, t, db) {
		if row.Kind == "g" && row.V0 == "users/"+userId && row.V2 == "accounts/"+accountId {
			roles = append(roles, row.V1)
		}
	}
	return roles
}

func TestRbacGrantViewerIfNone(t *testing.T) {
	t.Parallel()
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	container := migratedDatabase(t.Context(), t)
	db := container.DB
	instance := func(ctx context.Context, t *testing.T) *rbac.Service {
		t.Helper()
		service, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)
		return service
	}

	t.Run("a person who holds no role is given viewer, and this instance decides from it", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		userId, accountId := uuid.NewString(), uuid.NewString()

		require.NoError(t, here.GrantViewerIfNone(ctx, rbac.NewUser(userId), rbac.NewAccount(accountId)))

		require.Equal(t, []string{"job_viewer"}, rolesStoredFor(ctx, t, db, userId, accountId))
		requireAccess(ctx, t, here, userId, accountId, allowedTo["job_viewer"])
	})

	// The table decides, not what this instance holds in memory: a role another instance gave
	// since the last reload is kept.
	t.Run("a stored admin is not replaced although this instance believes no role is held", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		userId, accountId := uuid.NewString(), uuid.NewString()
		storeRow(ctx, t, db, "g", "users/"+userId, "account_admin", "accounts/"+accountId)
		requireAccess(ctx, t, here, userId, accountId, nil)

		require.NoError(t, here.GrantViewerIfNone(ctx, rbac.NewUser(userId), rbac.NewAccount(accountId)))

		require.Equal(t, []string{"account_admin"}, rolesStoredFor(ctx, t, db, userId, accountId))
		// And this instance now decides from it.
		requireAccess(ctx, t, here, userId, accountId, allowedTo["account_admin"])
	})

	// Two instances of the API: the one that did not give the role lets the person in as soon as
	// it is asked, without waiting for its periodic reload.
	t.Run("a viewer another instance made is let in here at once", func(t *testing.T) {
		ctx := t.Context()
		here, there := instance(ctx, t), instance(ctx, t)
		userId, accountId := uuid.NewString(), uuid.NewString()
		user, account := rbac.NewUser(userId), rbac.NewAccount(accountId)

		require.NoError(t, there.GrantViewerIfNone(ctx, user, account))
		requireAccess(ctx, t, here, userId, accountId, nil)

		require.NoError(t, here.GrantViewerIfNone(ctx, user, account))

		requireAccess(ctx, t, here, userId, accountId, allowedTo["job_viewer"])
		require.Equal(t, []string{"job_viewer"}, rolesStoredFor(ctx, t, db, userId, accountId))
	})

	// It is asked on every page load of every member: where a role is held it writes nothing,
	// and does not read the roles again either.
	t.Run("nothing is written and the roles are not read again when a role is held", func(t *testing.T) {
		ctx := t.Context()
		userId, accountId := uuid.NewString(), uuid.NewString()
		storeRow(ctx, t, db, "g", "users/"+userId, "job_developer", "accounts/"+accountId)
		here := instance(ctx, t)
		// A role this instance has not read: it learns of it at its next reload, and no sooner.
		unreadId := uuid.NewString()
		storeRow(ctx, t, db, "g", "users/"+unreadId, "account_admin", "accounts/"+accountId)
		before := storedRows(ctx, t, db)

		for range 3 {
			require.NoError(t, here.GrantViewerIfNone(ctx, rbac.NewUser(userId), rbac.NewAccount(accountId)))
		}

		require.Equal(t, before, storedRows(ctx, t, db), "a role was held, and the table was written")
		requireAccess(ctx, t, here, unreadId, accountId, nil)
		requireAccess(ctx, t, here, userId, accountId, allowedTo["job_developer"])
	})

	t.Run("two instances asked at once leave exactly one viewer row", func(t *testing.T) {
		ctx := t.Context()
		here, there := instance(ctx, t), instance(ctx, t)
		accountId := uuid.NewString()
		account := rbac.NewAccount(accountId)

		for range 100 {
			userId := uuid.NewString()
			user := rbac.NewUser(userId)
			var wg sync.WaitGroup
			wg.Go(func() { require.NoError(t, here.GrantViewerIfNone(ctx, user, account)) })
			wg.Go(func() { require.NoError(t, there.GrantViewerIfNone(ctx, user, account)) })
			wg.Wait()

			require.Equal(t, []string{"job_viewer"}, rolesStoredFor(ctx, t, db, userId, accountId))
		}
	})

	// Whichever of the two reaches the table first, the role that was asked for by name is the
	// one left: viewer never stands beside it, and never in its place.
	t.Run("asked at once with a change of role, it leaves the role that was set", func(t *testing.T) {
		ctx := t.Context()
		here, there := instance(ctx, t), instance(ctx, t)
		accountId := uuid.NewString()
		account := rbac.NewAccount(accountId)

		for range 100 {
			userId := uuid.NewString()
			user := rbac.NewUser(userId)
			var wg sync.WaitGroup
			wg.Go(func() { require.NoError(t, here.GrantViewerIfNone(ctx, user, account)) })
			wg.Go(func() {
				require.NoError(t, there.SetRole(ctx, user, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN))
			})
			wg.Wait()

			require.Equal(t, []string{"account_admin"}, rolesStoredFor(ctx, t, db, userId, accountId))
		}
	})
}
