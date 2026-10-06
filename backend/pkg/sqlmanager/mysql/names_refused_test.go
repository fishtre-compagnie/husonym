package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const nulName = "nul\x00byte"

// constraintRow gives a constraint of one column that refers to one column.
func constraintRow(t *testing.T, kind, column, referencedColumn string) *mysql_queries.GetTableConstraintsRow {
	t.Helper()
	return &mysql_queries.GetTableConstraintsRow{
		SchemaName:            "db",
		TableName:             "t",
		ConstraintName:        "c1",
		ConstraintType:        kind,
		ConstraintColumns:     jsonNames(t, column),
		ReferencedSchemaName:  "db",
		ReferencedTableName:   "p",
		ReferencedColumnNames: jsonNames(t, referencedColumn),
	}
}

// MySQL cuts a statement at a NUL byte: the name of a column that holds one is refused where
// the statement of a constraint is built. The catalog gives no name for a key part that is an
// expression, nor for a key column the user cannot see: an empty name is accepted.
func Test_ConstraintColumns_NamesRefused(t *testing.T) {
	cases := []struct {
		name             string
		kind             string
		column           string
		referencedColumn string
		refused          bool
	}{
		{name: "primary key", kind: "PRIMARY KEY", column: "a", referencedColumn: ""},
		{name: "primary key, NUL in a column", kind: "PRIMARY KEY", column: nulName, refused: true},
		{name: "primary key, column without a name", kind: "PRIMARY KEY", column: ""},
		{name: "unique", kind: "UNIQUE", column: "a"},
		{name: "unique, NUL in a column", kind: "UNIQUE", column: nulName, refused: true},
		{name: "unique, key part that is an expression", kind: "UNIQUE", column: ""},
		{name: "foreign key", kind: "FOREIGN KEY", column: "a", referencedColumn: "id"},
		{name: "foreign key, NUL in a column", kind: "FOREIGN KEY", column: nulName, referencedColumn: "id", refused: true},
		{name: "foreign key, NUL in a referenced column", kind: "FOREIGN KEY", column: "a", referencedColumn: nulName, refused: true},
		{name: "foreign key, column without a name", kind: "FOREIGN KEY", column: "", referencedColumn: "id"},
		{name: "foreign key, referenced column without a name", kind: "FOREIGN KEY", column: "a", referencedColumn: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statement, err := buildAlterStatementByConstraint(constraintRow(t, tc.kind, tc.column, tc.referencedColumn))
			if tc.refused {
				require.Error(t, err)
				require.Nil(t, statement)
				return
			}
			require.NoError(t, err)
			require.NotContains(t, statement.Statement, "\x00")
		})
	}
}

// A key part of a constraint that is an expression comes from the catalog as a JSON null.
func Test_ConstraintColumns_ExpressionKeyPartIsAccepted(t *testing.T) {
	row := constraintRow(t, "UNIQUE", "a", "")
	row.ConstraintColumns = json.RawMessage(`["a", null]`)
	row.ReferencedColumnNames = json.RawMessage(`[null, null]`)
	statement, err := buildAlterStatementByConstraint(row)
	require.NoError(t, err)
	require.Contains(t, statement.Statement, "ALTER TABLE `db`.`t` ADD CONSTRAINT `c1` UNIQUE (`a`,``);")
}

// The catalog shows a key whose column the user cannot see and hides the name of that column,
// which comes as a JSON null. The statement of such a key is built as it is for any column,
// with an empty name, so that the server answers it alone and the table is still read.
func Test_ConstraintColumns_AKeyColumnTheUserCannotSee(t *testing.T) {
	ordinary := func(name string) oddName { return oddName{name: name, lit: "'" + name + "'"} }
	cases := []struct {
		name              string
		kind              string
		constraint        string
		columns           string
		referencedColumns string
		alter             string
	}{
		{
			name: "primary key", kind: "PRIMARY KEY", constraint: "PRIMARY",
			columns: `[null]`, referencedColumns: `[null]`,
			alter: "ALTER TABLE `db`.`t` ADD PRIMARY KEY (``);",
		},
		{
			name: "primary key, one column of two", kind: "PRIMARY KEY", constraint: "PRIMARY",
			columns: `["a", null]`, referencedColumns: `[null, null]`,
			alter: "ALTER TABLE `db`.`t` ADD PRIMARY KEY (`a`,``);",
		},
		{
			name: "foreign key, child column", kind: "FOREIGN KEY", constraint: "fk1",
			columns: `[null]`, referencedColumns: `["id"]`,
			alter: "ALTER TABLE `db`.`t` ADD CONSTRAINT `fk1` FOREIGN KEY (``) REFERENCES `db`.`p`(`id`) ON DELETE CASCADE ON UPDATE NO ACTION;",
		},
		{
			name: "foreign key, referenced column", kind: "FOREIGN KEY", constraint: "fk1",
			columns: `["a"]`, referencedColumns: `[null]`,
			alter: "ALTER TABLE `db`.`t` ADD CONSTRAINT `fk1` FOREIGN KEY (`a`) REFERENCES `db`.`p`(``) ON DELETE CASCADE ON UPDATE NO ACTION;",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := &mysql_queries.GetTableConstraintsRow{
				SchemaName:            "db",
				TableName:             "t",
				ConstraintName:        tc.constraint,
				ConstraintType:        tc.kind,
				ConstraintColumns:     json.RawMessage(tc.columns),
				ReferencedSchemaName:  "db",
				ReferencedTableName:   "p",
				ReferencedColumnNames: json.RawMessage(tc.referencedColumns),
				DeleteRule:            sql.NullString{String: "CASCADE", Valid: true},
				UpdateRule:            sql.NullString{String: "NO ACTION", Valid: true},
			}
			statement, err := buildAlterStatementByConstraint(row)
			require.NoError(t, err)
			require.Equal(t,
				wantConstraintProcedure(ordinary("db"), ordinary("t"), ordinary(tc.constraint), tc.alter),
				withFixedProcedureName(t, statement.Statement))
		})
	}
}

// The other statements of a table are still built when one key column is hidden from the user.
func Test_GetTableInitStatements_AKeyColumnTheUserCannotSee(t *testing.T) {
	querier := mysql_queries.NewMockQuerier(t)
	querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
			SchemaName: "db", TableName: "t", ColumnName: "a", DataType: "varchar(20)",
			ColumnDefault: []uint8(""), GenerationExp: []uint8(""), IsNullable: 1,
		}}, nil)
	querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetTableConstraintsRow{
			{
				SchemaName: "db", TableName: "t", ConstraintName: "PRIMARY", ConstraintType: "PRIMARY KEY",
				ConstraintColumns: json.RawMessage(`[null]`), ReferencedColumnNames: json.RawMessage(`[null]`),
			},
			{
				SchemaName: "db", TableName: "t", ConstraintName: "fk1", ConstraintType: "FOREIGN KEY",
				ConstraintColumns: json.RawMessage(`[null]`), ReferencedColumnNames: json.RawMessage(`[null]`),
				ReferencedSchemaName: "db", ReferencedTableName: "p",
				DeleteRule: sql.NullString{String: "CASCADE", Valid: true},
				UpdateRule: sql.NullString{String: "NO ACTION", Valid: true},
			},
		}, nil)
	querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{}, nil)
	manager := &MysqlManager{resolvedQuerier: querier}

	statements, err := manager.GetTableInitStatements(context.Background(),
		[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}})
	require.NoError(t, err)
	require.Len(t, statements, 1)
	require.Len(t, statements[0].AlterTableStatements, 2)
	require.Contains(t, statements[0].AlterTableStatements[0].Statement,
		"ALTER TABLE `db`.`t` ADD PRIMARY KEY (``);")
	require.Contains(t, statements[0].AlterTableStatements[1].Statement,
		"ALTER TABLE `db`.`t` ADD CONSTRAINT `fk1` FOREIGN KEY (``) REFERENCES `db`.`p`(``) ON DELETE CASCADE ON UPDATE NO ACTION;")
}

func Test_IdempotentIndex_NamesRefused(t *testing.T) {
	index := func(name string, columns ...string) *indexInfo {
		return &indexInfo{indexName: name, indexType: "BTREE", columns: columns, columnPrefixes: map[string]int64{}}
	}
	cases := []struct {
		name          string
		schema, table string
		index         *indexInfo
		refused       bool
	}{
		{name: "columns", schema: "db", table: "t", index: index("idx", "a", "b")},
		{name: "expression", schema: "db", table: "t", index: index("idx", "a", "(lower(`b`))")},
		{name: "schema without a name", schema: "", table: "t", index: index("idx", "a"), refused: true},
		{name: "NUL in the schema", schema: nulName, table: "t", index: index("idx", "a"), refused: true},
		{name: "table without a name", schema: "db", table: "", index: index("idx", "a"), refused: true},
		{name: "NUL in the table", schema: "db", table: nulName, index: index("idx", "a"), refused: true},
		{name: "index without a name", schema: "db", table: "t", index: index("", "a"), refused: true},
		{name: "NUL in the index", schema: "db", table: "t", index: index(nulName, "a"), refused: true},
		{name: "column without a name", schema: "db", table: "t", index: index("idx", "a", ""), refused: true},
		{name: "NUL in a column", schema: "db", table: "t", index: index("idx", "a", nulName), refused: true},
		{name: "NUL in an expression", schema: "db", table: "t", index: index("idx", "(lower(`b\x00`))"), refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			statement, err := buildIdempotentIndexStatement(tc.schema, tc.table, tc.index)
			if tc.refused {
				require.Error(t, err)
				require.Empty(t, statement)
				return
			}
			require.NoError(t, err)
			require.Equal(t, withFixedProcedureName(t, wrapIdempotentIndex(tc.schema, tc.table, tc.index)),
				withFixedProcedureName(t, statement))
		})
	}
}

func Test_IdempotentTrigger_NamesRefused(t *testing.T) {
	build := func(schema, table, trigger, triggerSchema string) (string, error) {
		return buildIdempotentTriggerStatement(schema, table, trigger, triggerSchema,
			"BEFORE", "INSERT", "ROW", "SET NEW.id = 1")
	}
	statement, err := build("db", "t", "trg", "db")
	require.NoError(t, err)
	require.Equal(t, wrapIdempotentTrigger("db", "t", "trg", "db", "BEFORE", "INSERT", "ROW", "SET NEW.id = 1"), statement)

	for _, name := range []string{"", nulName} {
		for position, names := range map[string][4]string{
			"schema":         {name, "t", "trg", "db"},
			"table":          {"db", name, "trg", "db"},
			"trigger":        {"db", "t", name, "db"},
			"trigger schema": {"db", "t", "trg", name},
		} {
			statement, err := build(names[0], names[1], names[2], names[3])
			require.Error(t, err, "%s %q", position, name)
			require.Empty(t, statement)
		}
	}
}

func Test_IdempotentFunction_NamesRefused(t *testing.T) {
	build := func(schema, function string) (string, error) {
		return buildIdempotentFunctionStatement(schema, function, "a int", "int", "RETURN a + 1", true)
	}
	statement, err := build("db", "fn")
	require.NoError(t, err)
	require.Equal(t, wrapIdempotentFunction("db", "fn", "a int", "int", "RETURN a + 1", true), statement)

	for _, name := range []string{"", nulName} {
		_, err := build(name, "fn")
		require.Error(t, err, "schema %q", name)
		_, err = build("db", name)
		require.Error(t, err, "function %q", name)
	}
}

// The triggers and the functions the manager reads from the catalog go through the builders that
// refuse a name.
func Test_TriggersAndFunctions_NamesRefused(t *testing.T) {
	tables := []*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}}

	triggers := func(name string) error {
		querier := mysql_queries.NewMockQuerier(t)
		querier.EXPECT().GetCustomTriggersBySchemaAndTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mysql_queries.GetCustomTriggersBySchemaAndTablesRow{{
				TriggerName: name, TriggerSchema: "db", SchemaName: "db", TableName: "t",
				Statement: "SET NEW.id = 1", EventType: "INSERT", Orientation: "ROW", Timing: "BEFORE",
			}}, nil)
		manager := &MysqlManager{resolvedQuerier: querier}
		_, err := manager.GetSchemaTableTriggers(context.Background(), tables)
		return err
	}
	require.NoError(t, triggers("trg"))
	require.Error(t, triggers(nulName))
	require.Error(t, triggers(""))

	functions := func(name string) error {
		querier := mysql_queries.NewMockQuerier(t)
		querier.EXPECT().GetCustomFunctionsBySchemas(mock.Anything, mock.Anything, []string{"db"}).
			Return([]*mysql_queries.GetCustomFunctionsBySchemasRow{{
				FunctionName: name, SchemaName: "db", ReturnDataType: "int",
				Definition: "RETURN a + 1", IsDeterministic: 1, FunctionSignature: []uint8("a int"),
			}}, nil)
		manager := &MysqlManager{resolvedQuerier: querier}
		_, err := manager.GetFunctionsByTables(context.Background(), tables)
		return err
	}
	require.NoError(t, functions("fn"))
	require.Error(t, functions(nulName))
	require.Error(t, functions(""))
}

// The index of a table whose key part is an expression is still built with the table.
func Test_GetTableInitStatements_ExpressionKeyPartIsAccepted(t *testing.T) {
	querier := mysql_queries.NewMockQuerier(t)
	querier.EXPECT().GetDatabaseTableSchemasBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow{{
			SchemaName: "db", TableName: "t", ColumnName: "a", DataType: "varchar(40)",
			ColumnDefault: []uint8(""), GenerationExp: []uint8(""),
		}}, nil)
	querier.EXPECT().GetTableConstraints(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetTableConstraintsRow{{
			SchemaName: "db", TableName: "t", ConstraintName: "u_lower", ConstraintType: "UNIQUE",
			ConstraintColumns: json.RawMessage(`[null]`), ReferencedColumnNames: json.RawMessage(`[null]`),
		}}, nil)
	querier.EXPECT().GetIndicesBySchemasAndTables(mock.Anything, mock.Anything, mock.Anything).
		Return([]*mysql_queries.GetIndicesBySchemasAndTablesRow{{
			SchemaName: "db", TableName: "t", IndexName: "idx_lower", IndexType: "BTREE",
			Expression: sql.NullString{String: "lower(`a`)", Valid: true},
		}}, nil)
	manager := &MysqlManager{resolvedQuerier: querier}

	statements, err := manager.GetTableInitStatements(context.Background(),
		[]*sqlmanager_shared.SchemaTable{{Schema: "db", Table: "t"}})
	require.NoError(t, err)
	require.Len(t, statements, 1)
	require.Len(t, statements[0].AlterTableStatements, 1)
	require.Contains(t, statements[0].AlterTableStatements[0].Statement,
		"ALTER TABLE `db`.`t` ADD CONSTRAINT `u_lower` UNIQUE (``);")
	require.Len(t, statements[0].IndexStatements, 1)
	require.Contains(t, statements[0].IndexStatements[0],
		"ALTER TABLE `db`.`t` ADD INDEX `idx_lower` ((lower(`a`))) USING BTREE;")
}
