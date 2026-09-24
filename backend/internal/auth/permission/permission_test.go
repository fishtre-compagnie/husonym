package permission

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/stretchr/testify/require"
)

func Test_NameAndParse_RoundTrip(t *testing.T) {
	require.Len(t, All(), len(mgmtv1alpha1.Permission_name)-1, "every value but the unspecified one")
	for _, p := range All() {
		back, ok := Parse(Name(p))
		require.True(t, ok, Name(p))
		require.Equal(t, p, back)
	}
	require.Equal(t, "connection:view_sensitive", Name(mgmtv1alpha1.Permission_PERMISSION_CONNECTION_VIEW_SENSITIVE))
	require.Equal(t, "job:execute", Name(mgmtv1alpha1.Permission_PERMISSION_JOB_EXECUTE))
}

func Test_Parse_RefusesWhatIsNoPermission(t *testing.T) {
	for _, name := range []string{"", "job", "job:", ":view", "job:fly", "unspecified:", "JOB:VIEW:x"} {
		_, ok := Parse(name)
		require.False(t, ok, name)
	}
}

// Every action the RBAC knows has a permission, and every permission is an action the RBAC
// knows: the two lists are one list, written twice.
func Test_EveryRbacActionHasAPermission(t *testing.T) {
	var named []mgmtv1alpha1.Permission
	for _, a := range []rbac.AccountAction{
		rbac.AccountAction_Create, rbac.AccountAction_Delete, rbac.AccountAction_View, rbac.AccountAction_Edit,
	} {
		named = append(named, Account(a))
	}
	for _, a := range []rbac.ConnectionAction{
		rbac.ConnectionAction_Create, rbac.ConnectionAction_Delete, rbac.ConnectionAction_View,
		rbac.ConnectionAction_ViewSensitive, rbac.ConnectionAction_Edit,
	} {
		named = append(named, Connection(a))
	}
	for _, a := range []rbac.JobAction{
		rbac.JobAction_Create, rbac.JobAction_Delete, rbac.JobAction_Execute, rbac.JobAction_View, rbac.JobAction_Edit,
	} {
		named = append(named, Job(a))
	}
	require.ElementsMatch(t, All(), named)
}

func Test_Scope(t *testing.T) {
	scope := NewScope([]string{"job:view", "connection:view", "made:up", ""})

	require.True(t, scope.Allows(mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW))
	require.False(t, scope.Allows(mgmtv1alpha1.Permission_PERMISSION_JOB_EXECUTE))
	require.NoError(t, scope.Require())
	require.NoError(t, scope.Require(
		mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW, mgmtv1alpha1.Permission_PERMISSION_CONNECTION_VIEW,
	))

	err := scope.Require(mgmtv1alpha1.Permission_PERMISSION_JOB_VIEW, mgmtv1alpha1.Permission_PERMISSION_JOB_EXECUTE)
	require.ErrorContains(t, err, "lacks the permission job:execute")

	// Nothing granted allows nothing.
	empty := NewScope(nil)
	for _, p := range All() {
		require.False(t, empty.Allows(p))
	}
}
