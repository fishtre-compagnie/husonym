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

// adminsStoredIn gives the people the table holds as administrators of an account.
func adminsStoredIn(ctx context.Context, t *testing.T, db *pgxpool.Pool, accountId string) []string {
	t.Helper()
	var admins []string
	for _, row := range storedRows(ctx, t, db) {
		if row.Kind == "g" && row.V1 == "account_admin" && row.V2 == "accounts/"+accountId {
			admins = append(admins, row.V0)
		}
	}
	return admins
}

func TestRbacKeepingAnAdmin(t *testing.T) {
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
	const (
		admin  = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN
		viewer = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER
	)

	t.Run("the only administrator is neither demoted nor removed", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		userId, accountId := uuid.NewString(), uuid.NewString()
		user, account := rbac.NewUser(userId), rbac.NewAccount(accountId)
		require.NoError(t, here.SetRole(ctx, user, account, admin))
		// Somebody else is in the account, and administers nothing.
		require.NoError(t, here.SetRole(ctx, someone(), account, viewer))

		require.ErrorIs(t, here.SetRoleKeepingAnAdmin(ctx, user, account, viewer), rbac.ErrLastAdmin)
		require.ErrorIs(t, here.RemoveMemberKeepingAnAdmin(ctx, user, account), rbac.ErrLastAdmin)

		require.Equal(t, []string{"account_admin"}, rolesStoredFor(ctx, t, db, userId, accountId))
		requireAccess(ctx, t, here, userId, accountId, allowedTo["account_admin"])
	})

	t.Run("giving the only administrator the role they hold is no change", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		userId, accountId := uuid.NewString(), uuid.NewString()
		user, account := rbac.NewUser(userId), rbac.NewAccount(accountId)
		require.NoError(t, here.SetRole(ctx, user, account, admin))

		require.NoError(t, here.SetRoleKeepingAnAdmin(ctx, user, account, admin))

		require.Equal(t, []string{"account_admin"}, rolesStoredFor(ctx, t, db, userId, accountId))
	})

	t.Run("of two administrators one is demoted, and the other then is not", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		oneId, otherId, accountId := uuid.NewString(), uuid.NewString(), uuid.NewString()
		one, other, account := rbac.NewUser(oneId), rbac.NewUser(otherId), rbac.NewAccount(accountId)
		require.NoError(t, here.SetRole(ctx, one, account, admin))
		require.NoError(t, here.SetRole(ctx, other, account, admin))

		require.NoError(t, here.SetRoleKeepingAnAdmin(ctx, one, account, viewer))

		require.Equal(t, []string{"job_viewer"}, rolesStoredFor(ctx, t, db, oneId, accountId))
		requireAccess(ctx, t, here, oneId, accountId, allowedTo["job_viewer"])
		require.ErrorIs(t, here.SetRoleKeepingAnAdmin(ctx, other, account, viewer), rbac.ErrLastAdmin)
		require.Equal(t, []string{"users/" + otherId}, adminsStoredIn(ctx, t, db, accountId))
	})

	t.Run("of two administrators one is removed, and the other then is not", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		oneId, otherId, accountId := uuid.NewString(), uuid.NewString(), uuid.NewString()
		one, other, account := rbac.NewUser(oneId), rbac.NewUser(otherId), rbac.NewAccount(accountId)
		require.NoError(t, here.SetRole(ctx, one, account, admin))
		require.NoError(t, here.SetRole(ctx, other, account, admin))

		require.NoError(t, here.RemoveMemberKeepingAnAdmin(ctx, one, account))

		require.Empty(t, rolesStoredFor(ctx, t, db, oneId, accountId))
		requireAccess(ctx, t, here, oneId, accountId, nil)
		require.ErrorIs(t, here.RemoveMemberKeepingAnAdmin(ctx, other, account), rbac.ErrLastAdmin)
		require.Equal(t, []string{"users/" + otherId}, adminsStoredIn(ctx, t, db, accountId))
	})

	t.Run("who is no administrator is changed and removed where there is one administrator", func(t *testing.T) {
		ctx := t.Context()
		here := instance(ctx, t)
		memberId, accountId := uuid.NewString(), uuid.NewString()
		member, account := rbac.NewUser(memberId), rbac.NewAccount(accountId)
		require.NoError(t, here.SetRole(ctx, someone(), account, admin))
		require.NoError(t, here.SetRole(ctx, member, account, viewer))

		require.NoError(t, here.SetRoleKeepingAnAdmin(ctx, member, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER))
		require.Equal(t, []string{"job_developer"}, rolesStoredFor(ctx, t, db, memberId, accountId))

		require.NoError(t, here.RemoveMemberKeepingAnAdmin(ctx, member, account))
		require.Empty(t, rolesStoredFor(ctx, t, db, memberId, accountId))
		require.Len(t, adminsStoredIn(ctx, t, db, accountId), 1)
	})

	// The table decides, not what this instance holds in memory.
	t.Run("an administrator this instance has not read yet is counted", func(t *testing.T) {
		ctx := t.Context()
		userId, accountId := uuid.NewString(), uuid.NewString()
		user, account := rbac.NewUser(userId), rbac.NewAccount(accountId)
		here := instance(ctx, t)
		require.NoError(t, here.SetRole(ctx, user, account, admin))
		unreadId := uuid.NewString()
		storeRow(ctx, t, db, "g", "users/"+unreadId, "account_admin", "accounts/"+accountId)

		require.NoError(t, here.SetRoleKeepingAnAdmin(ctx, user, account, viewer))

		require.Equal(t, []string{"users/" + unreadId}, adminsStoredIn(ctx, t, db, accountId))
	})

	t.Run("an administrator this instance still believes in, and the table no longer holds, is not counted", func(t *testing.T) {
		ctx := t.Context()
		here, there := instance(ctx, t), instance(ctx, t)
		userId, accountId := uuid.NewString(), uuid.NewString()
		user, other, account := rbac.NewUser(userId), someone(), rbac.NewAccount(accountId)
		require.NoError(t, here.SetRole(ctx, user, account, admin))
		require.NoError(t, here.SetRole(ctx, other, account, admin))
		// Another instance demotes the other one: this one has not read it.
		require.NoError(t, there.SetRole(ctx, other, account, viewer))

		require.ErrorIs(t, here.SetRoleKeepingAnAdmin(ctx, user, account, viewer), rbac.ErrLastAdmin)

		require.Equal(t, []string{"users/" + userId}, adminsStoredIn(ctx, t, db, accountId))
	})

	t.Run("two administrators demoting each other at once on two instances leave one", func(t *testing.T) {
		ctx := t.Context()
		here, there := instance(ctx, t), instance(ctx, t)

		for range 50 {
			accountId := uuid.NewString()
			one, other, account := someone(), someone(), rbac.NewAccount(accountId)
			require.NoError(t, here.SetRole(ctx, one, account, admin))
			require.NoError(t, here.SetRole(ctx, other, account, admin))

			var errOne, errOther error
			var wg sync.WaitGroup
			wg.Go(func() { errOne = here.SetRoleKeepingAnAdmin(ctx, one, account, viewer) })
			wg.Go(func() { errOther = there.SetRoleKeepingAnAdmin(ctx, other, account, viewer) })
			wg.Wait()

			refused, done := errOne, errOther
			if errOne == nil {
				refused, done = errOther, errOne
			}
			require.NoError(t, done)
			require.ErrorIs(t, refused, rbac.ErrLastAdmin)
			require.Len(t, adminsStoredIn(ctx, t, db, accountId), 1)
		}
	})

	t.Run("one demoted while the other is removed, at once on two instances, leaves one", func(t *testing.T) {
		ctx := t.Context()
		here, there := instance(ctx, t), instance(ctx, t)

		for range 50 {
			accountId := uuid.NewString()
			one, other, account := someone(), someone(), rbac.NewAccount(accountId)
			require.NoError(t, here.SetRole(ctx, one, account, admin))
			require.NoError(t, here.SetRole(ctx, other, account, admin))

			var errOne, errOther error
			var wg sync.WaitGroup
			wg.Go(func() { errOne = here.SetRoleKeepingAnAdmin(ctx, one, account, viewer) })
			wg.Go(func() { errOther = there.RemoveMemberKeepingAnAdmin(ctx, other, account) })
			wg.Wait()

			refused, done := errOne, errOther
			if errOne == nil {
				refused, done = errOther, errOne
			}
			require.NoError(t, done)
			require.ErrorIs(t, refused, rbac.ErrLastAdmin)
			require.Len(t, adminsStoredIn(ctx, t, db, accountId), 1)
		}
	})
}
