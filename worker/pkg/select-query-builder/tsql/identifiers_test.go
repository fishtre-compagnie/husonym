package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The columns of the where clause are named by the table the caller gives, written as one
// identifier between double quotes like the rest of a SQL Server statement of the builder.
func Test_QualifyWhereConditionAs(t *testing.T) {
	for name, tc := range map[string]struct{ table, sql, want string }{
		"an ordinary name": {
			table: "users",
			sql:   "SELECT * FROM t WHERE name = 'John' AND age > 30",
			want:  `SELECT * FROM t WHERE "users"."name" = 'John' AND "users"."age" > 30`,
		},
		"a name holding a double quote and a closing bracket": {
			table: `we"i]rd`,
			sql:   "SELECT * FROM t WHERE name = 'John' OR t.age > 30",
			want:  `SELECT * FROM t WHERE "we""i]rd"."name" = 'John' OR "we""i]rd"."age" > 30`,
		},
		"harmless oddities": {
			table: `o'clock back\slash semi;colon`,
			sql:   "SELECT * FROM t WHERE id = 1",
			want:  `SELECT * FROM t WHERE "o'clock back\slash semi;colon"."id" = 1`,
		},
		"a dot does not separate": {
			table: "a.b",
			sql:   "SELECT * FROM t WHERE id = 1",
			want:  `SELECT * FROM t WHERE "a.b"."id" = 1`,
		},
		"the from clause is not what names the columns": {
			table: `we"ird`,
			sql:   `SELECT * FROM "other" AS o WHERE id = 1`,
			want:  `SELECT * FROM "other" AS o WHERE "we""ird"."id" = 1`,
		},
		"a subquery names its columns by its own table": {
			table: `we"ird`,
			sql:   "SELECT * FROM t WHERE id IN (SELECT user_id FROM orders WHERE amount > 100)",
			want:  `SELECT * FROM t WHERE "we""ird"."id" IN ( SELECT user_id FROM orders WHERE "orders"."amount" > 100 )`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := QualifyWhereConditionAs(tc.sql, tc.table)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("a table without a name is refused", func(t *testing.T) {
		_, err := QualifyWhereConditionAs("SELECT * FROM t WHERE id = 1", "")
		require.Error(t, err)
	})
	t.Run("a statement that does not parse is refused", func(t *testing.T) {
		_, err := QualifyWhereConditionAs("SELECT * FROM WHERE id = 1", "users")
		require.Error(t, err)
	})
}

// A table named by an alias, or by a name written without quotes, is written as one
// identifier where it names a column.
func Test_QualifyWhereCondition_TableIsWrittenAsOneIdentifier(t *testing.T) {
	for name, tc := range map[string]struct{ sql, want string }{
		"alias": {
			sql:  "SELECT id FROM dbo.users u WHERE name = 'John'",
			want: `SELECT id FROM dbo.users u WHERE "u"."name" = 'John'`,
		},
		"schema and table": {
			sql:  "SELECT id FROM dbo.users WHERE name = 'John'",
			want: `SELECT id FROM dbo.users WHERE "dbo"."users"."name" = 'John'`,
		},
		"a table already between double quotes is left as it is": {
			sql:  `SELECT id FROM "we ird" WHERE name = 'John'`,
			want: `SELECT id FROM "we ird" WHERE "we ird"."name" = 'John'`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := QualifyWhereCondition(tc.sql)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
