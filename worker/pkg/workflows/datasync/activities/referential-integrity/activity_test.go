package referentialintegrity_activity

import (
	"context"
	"strings"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
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
		orphanCondition(sqlident.MySQL, all[0], all[0].ForeignKeys[0]))

	// MySQL cannot modify a table it reads in a plain subquery: a self-reference goes
	// through a derived table.
	require.Contains(t, orphanCondition(sqlident.MySQL, all[1], all[1].ForeignKeys[0]),
		"FROM (SELECT * FROM `shop`.`COMMANDE`) p WHERE")
	require.Contains(t, orphanCondition(sqlident.Postgres, all[1], all[1].ForeignKeys[0]),
		`FROM "shop"."COMMANDE" p WHERE`)
}

// The "no parent" value is the default of the column as the source catalog gives it: it is
// written as one string literal of the engine, whatever it holds.
func Test_orphanCondition_NoParentValueIsOneLiteral(t *testing.T) {
	type engine struct {
		dialect  sqlident.Dialect
		table    string
		column   string
		parent   string
		parentID string
		literals map[string]string
	}
	engines := map[string]engine{
		"postgres": {
			dialect: sqlident.Postgres, table: `"shop"."line"`, column: `"kind"`, parent: `"shop"."it's"`, parentID: `p."code"`,
			literals: map[string]string{
				`none`:       `'none'`,
				`o'clock`:    `'o''clock'`,
				`none\`:      `E'none\\'`,
				`back\slash`: `E'back\\slash'`,
				`0`:          `'0'`,
			},
		},
		"mysql": {
			dialect: sqlident.MySQL, table: "`shop`.`line`", column: "`kind`", parent: "`shop`.`it's`", parentID: "p.`code`",
			literals: map[string]string{
				`none`:       `'none'`,
				`o'clock`:    `'o''clock'`,
				`none\`:      `_utf8mb4 0x6E6F6E655C`,
				`back\slash`: `_utf8mb4 0x6261636B5C736C617368`,
				`0`:          `'0'`,
			},
		},
		"sqlserver": {
			dialect: sqlident.SQLServer, table: `[shop].[line]`, column: `[kind]`, parent: `[shop].[it's]`, parentID: `p.[code]`,
			literals: map[string]string{
				`none`:       `N'none'`,
				`o'clock`:    `N'o''clock'`,
				`none\`:      `N'none\'`,
				`back\slash`: `N'back\slash'`,
				`0`:          `N'0'`,
			},
		},
	}
	for name, e := range engines {
		for value, literal := range e.literals {
			table := &TableForeignKeys{Schema: "shop", Table: "line", ForeignKeys: []*tableplan.ForeignKey{{
				Columns: []string{"kind"}, NotNull: []bool{true},
				ParentSchema: "shop", ParentTable: "it's", ParentColumns: []string{"code"}, NoParentValue: &value,
			}}}
			child := e.table + "." + e.column
			condition := child + " IS NOT NULL AND " + child + " <> " + literal + " AND " +
				"NOT EXISTS (SELECT 1 FROM " + e.parent + " p WHERE " + e.parentID + " = " + child + ")"
			require.Equal(t, condition, orphanCondition(e.dialect, table, table.ForeignKeys[0]), "%s, %q", name, value)
			require.Equal(t, "DELETE FROM "+e.table+" WHERE "+condition,
				repairStatement(e.dialect, table, table.ForeignKeys[0]), "%s, %q", name, value)
		}
	}
}

// A driver the product has no dialect for is refused before anything is read.
func Test_checkForeignKeys_UnknownDriver(t *testing.T) {
	db := &fakeDb{counts: map[string][]int64{"LIGNE": {3}}}
	_, err := checkForeignKeys(context.Background(), db, "oracle", tables(),
		policy{skipViolations: true, emptiedByRun: true}, nopLogger{})
	require.ErrorContains(t, err, `no SQL dialect for driver "oracle"`)
	require.Empty(t, db.repairs)
	require.Equal(t, []int64{3}, db.counts["LIGNE"])
}
