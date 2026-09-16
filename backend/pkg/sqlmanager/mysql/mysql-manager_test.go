package sqlmanager_mysql

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	"github.com/stretchr/testify/require"
)

func Test_EscapeMysqlColumns(t *testing.T) {
	require.Empty(t, EscapeMysqlColumns(nil))
	require.Equal(
		t,
		EscapeMysqlColumns([]string{"foo", "bar", "baz"}),
		[]string{"`foo`", "`bar`", "`baz`"},
	)
}

func Test_BuildMysqlTruncateStatement(t *testing.T) {
	actual, err := BuildMysqlTruncateStatement("public", "users")
	require.NoError(t, err)
	require.Equal(
		t,
		`TRUNCATE "public"."users";`,
		actual,
	)
}

func Test_ParseConstraintColumnPrefixes(t *testing.T) {
	t.Run("keys lengths by column", func(t *testing.T) {
		actual, err := parseConstraintColumnPrefixes(
			json.RawMessage(`[{"column":"endpoint","sub_part":255},{"column":"id","sub_part":null}]`),
		)
		require.NoError(t, err)
		require.Equal(t, map[string]int64{"endpoint": 255}, actual)
	})

	t.Run("tolerates a constraint with no column", func(t *testing.T) {
		actual, err := parseConstraintColumnPrefixes(
			json.RawMessage(`[{"column":null,"sub_part":null}]`),
		)
		require.NoError(t, err)
		require.Empty(t, actual)
	})

	t.Run("tolerates no rows at all", func(t *testing.T) {
		actual, err := parseConstraintColumnPrefixes(nil)
		require.NoError(t, err)
		require.Empty(t, actual)
	})
}

func Test_EscapeMysqlColumnsWithPrefixes(t *testing.T) {
	// The length belongs outside the backticks: inside, MySQL would read it as part of
	// the column name.
	require.Equal(
		t,
		[]string{"`a`", "`b`(50)"},
		escapeMysqlColumnsWithPrefixes([]string{"a", "b"}, map[string]int64{"b": 50}),
	)
	require.Equal(
		t,
		[]string{"`a`"},
		escapeMysqlColumnsWithPrefixes([]string{"a"}, nil),
	)
}

func Test_BuildAlterStatementByConstraint_CarriesPrefixLength(t *testing.T) {
	t.Run("unique", func(t *testing.T) {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:               "web_stations",
			TableName:                "PUSH_SUBSCRIPTION",
			ConstraintName:           "UNIQ_PUSH_SUB_ENDPOINT",
			ConstraintType:           "UNIQUE",
			ConstraintColumns:        json.RawMessage(`["ENDPOINT"]`),
			ReferencedColumnNames:    json.RawMessage(`[null]`),
			ConstraintColumnPrefixes: json.RawMessage(`[{"column":"ENDPOINT","sub_part":255}]`),
		})
		require.NoError(t, err)
		require.Contains(
			t,
			actual.Statement,
			"ADD CONSTRAINT `UNIQ_PUSH_SUB_ENDPOINT` UNIQUE (`ENDPOINT`(255));",
		)
	})

	t.Run("primary key", func(t *testing.T) {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:               "db",
			TableName:                "t",
			ConstraintName:           "PRIMARY",
			ConstraintType:           "PRIMARY KEY",
			ConstraintColumns:        json.RawMessage(`["url","kind"]`),
			ReferencedColumnNames:    json.RawMessage(`[null]`),
			ConstraintColumnPrefixes: json.RawMessage(`[{"column":"url","sub_part":100},{"column":"kind","sub_part":null}]`),
		})
		require.NoError(t, err)
		require.Contains(t, actual.Statement, "ADD PRIMARY KEY (`url`(100),`kind`);")
	})

	t.Run("without a prefix the statement is unchanged", func(t *testing.T) {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:               "db",
			TableName:                "t",
			ConstraintName:           "uniq_email",
			ConstraintType:           "UNIQUE",
			ConstraintColumns:        json.RawMessage(`["email"]`),
			ReferencedColumnNames:    json.RawMessage(`[null]`),
			ConstraintColumnPrefixes: json.RawMessage(`[{"column":"email","sub_part":null}]`),
		})
		require.NoError(t, err)
		require.Contains(t, actual.Statement, "ADD CONSTRAINT `uniq_email` UNIQUE (`email`);")
	})

	t.Run("a foreign key never carries one", func(t *testing.T) {
		actual, err := buildAlterStatementByConstraint(&mysql_queries.GetTableConstraintsRow{
			SchemaName:               "db",
			TableName:                "child",
			ConstraintName:           "fk_parent",
			ConstraintType:           "FOREIGN KEY",
			ConstraintColumns:        json.RawMessage(`["parent_id"]`),
			ReferencedSchemaName:     "db",
			ReferencedTableName:      "parent",
			ReferencedColumnNames:    json.RawMessage(`["id"]`),
			ConstraintColumnPrefixes: json.RawMessage(`[{"column":"parent_id","sub_part":10}]`),
			UpdateRule:               sql.NullString{String: "NO ACTION", Valid: true},
			DeleteRule:               sql.NullString{String: "NO ACTION", Valid: true},
		})
		require.NoError(t, err)
		require.Contains(t, actual.Statement, "FOREIGN KEY (`parent_id`)")
		require.NotContains(t, actual.Statement, "(10)")
	})
}

func Test_WrapIdempotentIndex_CarriesPrefixLength(t *testing.T) {
	actual := wrapIdempotentIndex("db", "t", &indexInfo{
		indexName:      "idx_mixed",
		indexType:      "BTREE",
		columns:        []string{"a", "b", "(lower(`c`))"},
		columnPrefixes: map[string]int64{"b": 50},
	})
	require.Contains(t, actual, "ADD INDEX `idx_mixed` (`a`, `b`(50), (lower(`c`))) USING BTREE;")
}

func Test_IdempotentWrappers_AreConcurrencySafe(t *testing.T) {
	constraintStmt := "ALTER TABLE `db`.`t` ADD CONSTRAINT `uniq` UNIQUE (`a`);"

	t.Run("a leftover procedure cannot block a replay", func(t *testing.T) {
		// The reconcile path replays a failed block statement by statement. Without this
		// DROP, the replay hits the procedure the batch pass left behind and reports
		// "already exists" instead of the error that actually stopped the ALTER.
		actual := wrapIdempotentConstraint("db", "t", "uniq", constraintStmt)
		require.True(t, strings.HasPrefix(actual, "DROP PROCEDURE IF EXISTS "))
	})

	t.Run("two runs of the same table do not share a procedure name", func(t *testing.T) {
		first := procedureNameOf(t, wrapIdempotentConstraint("db", "t", "uniq", constraintStmt))
		second := procedureNameOf(t, wrapIdempotentConstraint("db", "t", "uniq", constraintStmt))
		require.NotEqual(t, first, second)
		require.LessOrEqual(t, len(first), maxIdentifierLength)
	})

	t.Run("index procedure names hold the same guarantees", func(t *testing.T) {
		idx := &indexInfo{indexName: "idx", indexType: "BTREE", columns: []string{"a"}}
		first := procedureNameOf(t, wrapIdempotentIndex("db", "t", idx))
		second := procedureNameOf(t, wrapIdempotentIndex("db", "t", idx))
		require.NotEqual(t, first, second)
		require.LessOrEqual(t, len(first), maxIdentifierLength)
	})
}

// procedureNameOf reads the name back out of a generated statement, which starts with
// "DROP PROCEDURE IF EXISTS <name>;".
func procedureNameOf(t *testing.T, stmt string) string {
	t.Helper()
	_, rest, found := strings.Cut(stmt, "DROP PROCEDURE IF EXISTS ")
	require.True(t, found)
	name, _, found := strings.Cut(rest, ";")
	require.True(t, found)
	return name
}
