package runprivileges_activity

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

type fakeDb struct {
	granted      map[string][]string
	readOnlyVars int64
}

func (f *fakeDb) GetRolePermissionsMap(context.Context) (map[string][]string, error) {
	return f.granted, nil
}

func (f *fakeDb) GetTableRowCount(context.Context, string, string, *string) (int64, error) {
	return f.readOnlyVars, nil
}

var tables = []string{"shop.ARTICLE", "shop.CLIENT"}

func Test_checkSource(t *testing.T) {
	// A read-only replica is a fine source: only SELECT matters.
	db := &fakeDb{readOnlyVars: 2, granted: map[string][]string{"shop.ARTICLE": {"SELECT"}, "shop.CLIENT": {"select"}}}
	findings, err := checkSource(context.Background(), db, "prod", tables)
	require.NoError(t, err)
	require.Empty(t, findings)

	db.granted["shop.CLIENT"] = []string{"INSERT"}
	findings, err = checkSource(context.Background(), db, "prod", tables)
	require.NoError(t, err)
	require.Equal(t, []string{`source "prod" cannot read shop.CLIENT (missing SELECT)`}, findings)
}

func Test_checkDestination(t *testing.T) {
	all := []string{"SELECT", "INSERT", "UPDATE", "DELETE"}

	readOnly := &fakeDb{readOnlyVars: 1, granted: map[string][]string{"shop.ARTICLE": all, "shop.CLIENT": all}}
	findings, err := checkDestination(context.Background(), readOnly, "staging", tables, false)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "read-only server")

	selectOnly := &fakeDb{granted: map[string][]string{"shop.ARTICLE": {"SELECT"}, "shop.CLIENT": all}}
	findings, err = checkDestination(context.Background(), selectOnly, "staging", tables, false)
	require.NoError(t, err)
	require.Equal(t, []string{`destination "staging" cannot write shop.ARTICLE (missing INSERT, UPDATE, DELETE)`}, findings)

	// A table the job creates itself has no privilege of its own yet.
	missingTable := &fakeDb{granted: map[string][]string{"shop.CLIENT": all}}
	findings, err = checkDestination(context.Background(), missingTable, "staging", tables, true)
	require.NoError(t, err)
	require.Empty(t, findings)
	findings, err = checkDestination(context.Background(), missingTable, "staging", tables, false)
	require.NoError(t, err)
	require.Len(t, findings, 1)
}

func Test_jobTables(t *testing.T) {
	require.Equal(t, []string{"shop.ARTICLE", "shop.CLIENT"}, jobTables([]*mgmtv1alpha1.JobMapping{
		{Schema: "shop", Table: "CLIENT", Column: "id"},
		{Schema: "shop", Table: "ARTICLE", Column: "id"},
		{Schema: "shop", Table: "CLIENT", Column: "nom"},
	}))
}

// No privilege at all reported means the account could not be looked up (declared for a
// specific host, rights held through a role): the run is not stopped on that.
func Test_unknownPrivilegesDoNotBlock(t *testing.T) {
	db := &fakeDb{granted: map[string][]string{}}
	findings, err := checkSource(context.Background(), db, "prod", tables)
	require.NoError(t, err)
	require.Empty(t, findings)
	findings, err = checkDestination(context.Background(), db, "staging", tables, false)
	require.NoError(t, err)
	require.Empty(t, findings)
}
