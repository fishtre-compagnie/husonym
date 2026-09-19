package referentialintegrity_activity

import (
	"context"
	"strings"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
)

// fakeDb answers orphan counts from a queue per table and records the repairs.
type fakeDb struct {
	counts  map[string][]int64
	repairs []string
}

func (f *fakeDb) GetTableRowCount(_ context.Context, _, table string, _ *string) (int64, error) {
	queue := f.counts[table]
	if len(queue) == 0 {
		return 0, nil
	}
	f.counts[table] = queue[1:]
	return queue[0], nil
}

func (f *fakeDb) Exec(_ context.Context, statement string) error {
	f.repairs = append(f.repairs, statement)
	return nil
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

var _ log.Logger = nopLogger{}

func tables() []*TableForeignKeys {
	zero := "0"
	return []*TableForeignKeys{
		{Schema: "shop", Table: "LIGNE", ForeignKeys: []*tableplan.ForeignKey{{
			Columns: []string{"commande_id"}, NotNull: []bool{true},
			ParentSchema: "shop", ParentTable: "COMMANDE", ParentColumns: []string{"id"}, NoParentValue: &zero,
		}}},
		{Schema: "shop", Table: "COMMANDE", ForeignKeys: []*tableplan.ForeignKey{{
			Columns: []string{"groupe_id"}, NotNull: []bool{false},
			ParentSchema: "shop", ParentTable: "COMMANDE", ParentColumns: []string{"id"},
		}}},
	}
}

func Test_checkForeignKeys_NoOrphan(t *testing.T) {
	db := &fakeDb{}
	repaired, err := checkForeignKeys(context.Background(), db, sqlmanager_shared.MysqlDriver, tables(), policy{}, nopLogger{})
	require.NoError(t, err)
	require.Zero(t, repaired)
	require.Empty(t, db.repairs)
}

func Test_checkForeignKeys_FailsWhenItCannotRepair(t *testing.T) {
	for name, p := range map[string]policy{
		"violations not skipped":  {skipViolations: false, emptiedByRun: true},
		"destination not emptied": {skipViolations: true, emptiedByRun: false},
	} {
		db := &fakeDb{counts: map[string][]int64{"LIGNE": {3}}}
		_, err := checkForeignKeys(context.Background(), db, sqlmanager_shared.MysqlDriver, tables(), p, nopLogger{})
		require.ErrorContains(t, err, "shop.LIGNE (commande_id) -> shop.COMMANDE: 3", name)
		require.Empty(t, db.repairs, name)
	}
}

func Test_checkForeignKeys_RepairsUntilNothingIsLeft(t *testing.T) {
	// First pass: 3 orphan lines and 2 orphan groups; second pass: the deleted lines
	// orphaned nothing more.
	db := &fakeDb{counts: map[string][]int64{"LIGNE": {3, 0}, "COMMANDE": {2, 0}}}
	repaired, err := checkForeignKeys(context.Background(), db, sqlmanager_shared.MysqlDriver, tables(),
		policy{skipViolations: true, emptiedByRun: true}, nopLogger{})
	require.NoError(t, err)
	require.EqualValues(t, 5, repaired)
	require.Len(t, db.repairs, 2)
	require.True(t, strings.HasPrefix(db.repairs[0], "DELETE FROM `shop`.`LIGNE` WHERE "), db.repairs[0])
	require.True(t, strings.HasPrefix(db.repairs[1], "UPDATE `shop`.`COMMANDE` SET `groupe_id` = NULL WHERE "), db.repairs[1])
}

func Test_orphanCondition(t *testing.T) {
	all := tables()
	require.Equal(t,
		"`shop`.`LIGNE`.`commande_id` IS NOT NULL AND `shop`.`LIGNE`.`commande_id` <> '0' AND "+
			"NOT EXISTS (SELECT 1 FROM `shop`.`COMMANDE` p WHERE p.`id` = `shop`.`LIGNE`.`commande_id`)",
		orphanCondition(sqlmanager_shared.MysqlDriver, all[0], all[0].ForeignKeys[0]))

	// MySQL cannot modify a table it reads in a plain subquery: a self-reference goes
	// through a derived table.
	require.Contains(t, orphanCondition(sqlmanager_shared.MysqlDriver, all[1], all[1].ForeignKeys[0]),
		"FROM (SELECT * FROM `shop`.`COMMANDE`) p WHERE")
	require.Contains(t, orphanCondition(sqlmanager_shared.PostgresDriver, all[1], all[1].ForeignKeys[0]),
		`FROM "shop"."COMMANDE" p WHERE`)
}
