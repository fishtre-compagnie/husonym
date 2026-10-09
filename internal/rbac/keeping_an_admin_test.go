package rbac

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/rbac/enforcer"
	"github.com/stretchr/testify/require"
)

// A table that cannot keep a role held says so, and changes nothing, instead of deciding from
// what memory holds. What the table of the API database does is tried on that table, in
// internal/integration-tests/rbac.
func Test_KeepingAnAdmin_OnATableThatCannot(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()
	require.NoError(t, service.SetRole(ctx, member, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN))
	before := rows.stored()

	err := service.SetRoleKeepingAnAdmin(ctx, member, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
	require.ErrorIs(t, err, enforcer.ErrNoKeptAssignments)
	err = service.RemoveMemberKeepingAnAdmin(ctx, member, account)
	require.ErrorIs(t, err, enforcer.ErrNoKeptAssignments)

	require.Equal(t, before, rows.stored())
}

func Test_SetRoleKeepingAnAdmin_ARoleThatIsNoneIsRefused(t *testing.T) {
	service := serviceOn(t, &memoryRows{})

	err := service.SetRoleKeepingAnAdmin(
		context.Background(), someone(), someAccount(), mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED,
	)

	require.Error(t, err)
	require.NotErrorIs(t, err, enforcer.ErrNoKeptAssignments)
}
