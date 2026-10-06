package querybuilder

import (
	"log/slog"
	"testing"

	"github.com/doug-martin/goqu/v9"
	"github.com/stretchr/testify/require"
)

// Each engine is given a schema, a table and a column that hold the quote character its
// statements are written with: " for PostgreSQL, ` for MySQL, and " for SQL Server, whose
// statements goqu writes between double quotes. The SQL Server table also holds a closing
// bracket, which is an ordinary character between double quotes.
type oddNames struct {
	driver                string
	schema, table, column string
	// the same names as the statements must hold them
	qSchema, qTable, qColumn string
	// an ordinary column, quoted
	qID string
}

var (
	oddPostgres = oddNames{
		driver: "postgres", schema: `sch"ema`, table: `we"ird`, column: `co"l`,
		qSchema: `"sch""ema"`, qTable: `"we""ird"`, qColumn: `"co""l"`, qID: `"id"`,
	}
	oddMysql = oddNames{
		driver: "mysql", schema: "sch`ema", table: "we`ird", column: "co`l",
		qSchema: "`sch``ema`", qTable: "`we``ird`", qColumn: "`co``l`", qID: "`id`",
	}
	oddSqlServer = oddNames{
		driver: "sqlserver", schema: `sch"ema`, table: `we"i]rd`, column: `co"l`,
		qSchema: `"sch""ema"`, qTable: `"we""i]rd"`, qColumn: `"co""l"`, qID: `"id"`,
	}
	oddEngines = []oddNames{oddPostgres, oddMysql, oddSqlServer}
)

func (n oddNames) qualified() string { return n.qSchema + "." + n.qTable }

func Test_BuildSelectQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			sql, err := BuildSelectQuery(n.driver, n.schema, n.table, []string{n.column, "id"}, nil)
			require.NoError(t, err)
			require.Equal(t, "SELECT "+n.qColumn+", "+n.qID+" FROM "+n.qualified()+";", sql)
		})
	}
	t.Run("a dot in a schema, a table or a column does not separate", func(t *testing.T) {
		sql, err := BuildSelectQuery("postgres", "a.b", "c.d", []string{"e.f", "g.h.i"}, nil)
		require.NoError(t, err)
		require.Equal(t, `SELECT "e.f", "g.h.i" FROM "a.b"."c.d";`, sql)
	})
	t.Run("a column named * is a column", func(t *testing.T) {
		sql, err := BuildSelectQuery("mysql", "db", "t", []string{"*"}, nil)
		require.NoError(t, err)
		require.Equal(t, "SELECT `*` FROM `db`.`t`;", sql)
	})
	t.Run("harmless oddities are written as they are", func(t *testing.T) {
		sql, err := BuildSelectQuery("postgres", "o'clock", `back\slash`,
			[]string{"semi;colon", "two$$dollars", "new\nline", "a space", "we`ird", "we]ird"}, nil)
		require.NoError(t, err)
		require.Equal(t,
			`SELECT "semi;colon", "two$$dollars", "new`+"\n"+`line", "a space", "we`+"`"+`ird", "we]ird" FROM "o'clock"."back\slash";`,
			sql)
	})
	t.Run("no column selects every column", func(t *testing.T) {
		sql, err := BuildSelectQuery("postgres", "public", "accounts", nil, nil)
		require.NoError(t, err)
		require.Equal(t, `SELECT * FROM "public"."accounts";`, sql)
	})
}

func Test_BuildSelectLimitQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]string{
		"postgres":  `SELECT * FROM "sch""ema"."we""ird" LIMIT 5`,
		"mysql":     "SELECT * FROM `sch``ema`.`we``ird` LIMIT 5",
		"sqlserver": `SELECT  TOP (5) * FROM "sch""ema"."we""i]rd"`,
	}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			sql, err := BuildSelectLimitQuery(n.driver, n.schema, n.table, 5)
			require.NoError(t, err)
			require.Equal(t, expected[n.driver], sql)
		})
	}
	t.Run("a dot in the schema does not separate", func(t *testing.T) {
		sql, err := BuildSelectLimitQuery("postgres", "a.b", "t", 0)
		require.NoError(t, err)
		require.Equal(t, `SELECT * FROM "a.b"."t"`, sql)
	})
	t.Run("ordinary names", func(t *testing.T) {
		sql, err := BuildSelectLimitQuery("sqlserver", "public", "accounts", 0)
		require.NoError(t, err)
		require.Equal(t, `SELECT * FROM "public"."accounts"`, sql)
	})
}

func Test_BuildSampledSelectLimitQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	expected := map[string]string{
		"postgres":  `SELECT * FROM (SELECT * FROM "sch""ema"."we""ird" LIMIT 1000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`,
		"mysql":     "SELECT * FROM (SELECT * FROM `sch``ema`.`we``ird` LIMIT 1000) AS `husonym_sample` ORDER BY RAND() ASC LIMIT 10",
		"sqlserver": `SELECT  TOP (10) * FROM (SELECT  TOP (1000) * FROM "sch""ema"."we""i]rd") AS "husonym_sample" ORDER BY NEWID() ASC`,
	}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			sql, err := BuildSampledSelectLimitQuery(n.driver, n.schema, n.table, 10, nil)
			require.NoError(t, err)
			require.Equal(t, expected[n.driver], sql)
		})
	}
	t.Run("a dot in the schema does not separate", func(t *testing.T) {
		sql, err := BuildSampledSelectLimitQuery("mysql", "a.b", "t", 10, nil)
		require.NoError(t, err)
		require.Equal(t,
			"SELECT * FROM (SELECT * FROM `a.b`.`t` LIMIT 1000) AS `husonym_sample` ORDER BY RAND() ASC LIMIT 10", sql)
	})
}

func Test_BuildTableSampleQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	// 2 rows a page: the window is on 500 pages, 0.5 percent, and every row is kept.
	size := TableSize{Rows: 200_000, Pages: 100_000}
	expected := map[string]string{
		"postgres":  `SELECT * FROM (SELECT * FROM "sch""ema"."we""ird" TABLESAMPLE SYSTEM (0.5) LIMIT 4000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`,
		"sqlserver": `SELECT  TOP (10) * FROM (SELECT * FROM "sch""ema"."we""i]rd" TABLESAMPLE (0.5 PERCENT)) AS "husonym_sample" ORDER BY NEWID() ASC`,
	}
	for _, n := range []oddNames{oddPostgres, oddSqlServer} {
		t.Run(n.driver, func(t *testing.T) {
			sql, ok, err := BuildTableSampleQuery(n.driver, n.schema, n.table, size, 10, nil)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, expected[n.driver], sql)
		})
	}
	t.Run("a dot in the schema does not separate", func(t *testing.T) {
		sql, ok, err := BuildTableSampleQuery("postgres", "a.b", "t", size, 10, nil)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t,
			`SELECT * FROM (SELECT * FROM "a.b"."t" TABLESAMPLE SYSTEM (0.5) LIMIT 4000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`,
			sql)
	})
}

func Test_BuildKeySlicesQueries_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	ranges := []KeyRange{{From: 5, To: 8999}, {From: 9000, To: 20000}}
	const (
		first  = "FROM `sch``ema`.`we``ird` WHERE ((`co``l` >= 5) AND (`co``l` <= 8999)) ORDER BY `co``l` ASC LIMIT 100"
		second = "FROM `sch``ema`.`we``ird` WHERE ((`co``l` >= 9000) AND (`co``l` <= 20000)) ORDER BY `co``l` ASC LIMIT 100"
	)
	n := oddMysql

	t.Run("sample", func(t *testing.T) {
		sql, err := BuildKeySlicesSampleQuery(n.driver, n.schema, n.table, n.column, ranges, 10, nil)
		require.NoError(t, err)
		require.Equal(t,
			"SELECT * FROM (SELECT * FROM (SELECT * "+first+") AS `t1` "+
				"UNION ALL (SELECT * FROM (SELECT * "+second+") AS `t1`)) AS `husonym_sample` ORDER BY RAND() ASC LIMIT 10",
			sql)
	})
	t.Run("count", func(t *testing.T) {
		sql, err := BuildKeySlicesCountQuery(n.driver, n.schema, n.table, n.column, ranges)
		require.NoError(t, err)
		require.Equal(t,
			"SELECT COUNT(*) FROM (SELECT * FROM (SELECT `co``l` "+first+") AS `t1` "+
				"UNION ALL (SELECT * FROM (SELECT `co``l` "+second+") AS `t1`)) AS `husonym_sample`",
			sql)
	})
	t.Run("a dot in the schema does not separate", func(t *testing.T) {
		sql, err := BuildKeySlicesCountQuery("mysql", "a.b", "t", "id", ranges[:1])
		require.NoError(t, err)
		require.Equal(t,
			"SELECT COUNT(*) FROM (SELECT `id` FROM `a.b`.`t` WHERE ((`id` >= 5) AND (`id` <= 8999)) ORDER BY `id` ASC LIMIT 100) AS `husonym_sample`",
			sql)
	})
	t.Run("ordinary names", func(t *testing.T) {
		sql, err := BuildKeySlicesSampleQuery("mysql", "db", "accounts", "id", ranges, 10, nil)
		require.NoError(t, err)
		require.Equal(t,
			"SELECT * FROM (SELECT * FROM (SELECT * FROM `db`.`accounts` WHERE ((`id` >= 5) AND (`id` <= 8999)) ORDER BY `id` ASC LIMIT 100) AS `t1` "+
				"UNION ALL (SELECT * FROM (SELECT * FROM `db`.`accounts` WHERE ((`id` >= 9000) AND (`id` <= 20000)) ORDER BY `id` ASC LIMIT 100) AS `t1`)) "+
				"AS `husonym_sample` ORDER BY RAND() ASC LIMIT 10",
			sql)
	})
}

func Test_BuildTruncateQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			sql, err := BuildTruncateQuery(n.driver, n.schema, n.table)
			require.NoError(t, err)
			require.Equal(t, "TRUNCATE "+n.qualified(), sql)
		})
	}
	t.Run("a dot in the schema does not separate", func(t *testing.T) {
		sql, err := BuildTruncateQuery("mysql", "a.b", "t")
		require.NoError(t, err)
		require.Equal(t, "TRUNCATE `a.b`.`t`", sql)
	})
	t.Run("ordinary names", func(t *testing.T) {
		sql, err := BuildTruncateQuery("postgres", "public", "accounts")
		require.NoError(t, err)
		require.Equal(t, `TRUNCATE "public"."accounts"`, sql)
	})
}

func Test_BuildInsertQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	no, yes := false, true
	values := map[string]string{"postgres": "($1, $2), ($3, $4)", "mysql": "(?, ?), (?, ?)", "sqlserver": "(@p1, @p2), (@p3, @p4)"}
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			records := []goqu.Record{{n.column: "a", "id": "1"}, {"id": "2", n.column: "b"}}
			sql, args, err := BuildInsertQuery(n.driver, n.schema, n.table, records, &no)
			require.NoError(t, err)
			require.Equal(t,
				"INSERT INTO "+n.qualified()+" ("+n.qColumn+", "+n.qID+") VALUES "+values[n.driver], sql)
			require.Equal(t, []any{"a", "1", "b", "2"}, args)
		})
	}
	t.Run("mysql names the first column as one identifier to skip a row already there", func(t *testing.T) {
		n := oddMysql
		sql, args, err := BuildInsertQuery(n.driver, n.schema, n.table, []goqu.Record{{n.column: "a", "id": "1"}}, &yes)
		require.NoError(t, err)
		require.Equal(t,
			"INSERT INTO `sch``ema`.`we``ird` (`co``l`, `id`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `co``l`=`co``l`", sql)
		require.Equal(t, []any{"a", "1"}, args)
	})
	t.Run("a column named * or holding a dot is a column", func(t *testing.T) {
		sql, args, err := BuildInsertQuery("postgres", "s", "t", []goqu.Record{{"a.b": "x", "*": "y"}}, &no)
		require.NoError(t, err)
		require.Equal(t, `INSERT INTO "s"."t" ("*", "a.b") VALUES ($1, $2)`, sql)
		require.Equal(t, []any{"y", "x"}, args)

		sql, _, err = BuildInsertQuery("mysql", "s", "t", []goqu.Record{{"a.b": "x", "*": "y"}}, &yes)
		require.NoError(t, err)
		require.Equal(t, "INSERT INTO `s`.`t` (`*`, `a.b`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `*`=`*`", sql)
	})
	t.Run("a value that is an expression is written as before", func(t *testing.T) {
		sql, args, err := BuildInsertQuery("postgres", "public", "users",
			[]goqu.Record{{"a": goqu.L("DEFAULT"), "b": nil}}, &no)
		require.NoError(t, err)
		require.Equal(t, `INSERT INTO "public"."users" ("a", "b") VALUES (DEFAULT, $1)`, sql)
		require.Equal(t, []any{nil}, args)
	})
	t.Run("no schema writes the table alone", func(t *testing.T) {
		sql, _, err := BuildInsertQuery("sqlserver", "", "users", []goqu.Record{{"name": "A"}}, &no)
		require.NoError(t, err)
		require.Equal(t, `INSERT INTO "users" ("name") VALUES (@p1)`, sql)
	})
	t.Run("rows without a column are written as before", func(t *testing.T) {
		expected := map[string]string{
			"postgres":  `INSERT INTO "public"."users" DEFAULT VALUES ON CONFLICT DO NOTHING`,
			"mysql":     "INSERT IGNORE INTO `public`.`users`",
			"sqlserver": `INSERT INTO "public"."users"`,
		}
		for driver, want := range expected {
			for _, records := range [][]goqu.Record{{}, {{}, {}}} {
				sql, args, err := BuildInsertQuery(driver, "public", "users", records, &yes)
				require.NoError(t, err)
				require.Equal(t, want, sql)
				require.Empty(t, args)
			}
		}
	})
	t.Run("rows that do not hold the same columns are refused", func(t *testing.T) {
		for _, records := range [][]goqu.Record{
			{{"a": 1}, {"b": 1}},
			{{"a": 1}, {"a": 1, "b": 2}},
			{{"a": 1, "b": 2}, {"a": 1}},
			{{}, {"a": 1}},
		} {
			_, _, err := BuildInsertQuery("postgres", "public", "users", records, &no)
			require.Error(t, err, "%v", records)
		}
	})
}

func Test_BuildUpdateQuery_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	for _, n := range oddEngines {
		t.Run(n.driver, func(t *testing.T) {
			sql, err := BuildUpdateQuery(n.driver, n.schema, n.table,
				[]string{"name", n.column}, []string{n.column, "id"},
				map[string]any{"name": "Alice", n.column: "x", "id": 1})
			require.NoError(t, err)
			require.Equal(t,
				"UPDATE "+n.qualified()+" SET "+n.qColumn+"='x',"+n.qID[:1]+"name"+n.qID[:1]+"='Alice' "+
					"WHERE (("+n.qColumn+" = 'x') AND ("+n.qID+" = 1))",
				sql)
		})
	}
	t.Run("one column", func(t *testing.T) {
		n := oddPostgres
		sql, err := BuildUpdateQuery(n.driver, n.schema, n.table, []string{n.column}, []string{"id"},
			map[string]any{n.column: "x", "id": 1})
		require.NoError(t, err)
		require.Equal(t, `UPDATE "sch""ema"."we""ird" SET "co""l"='x' WHERE ("id" = 1)`, sql)
	})
	t.Run("a column named * or holding a dot is a column", func(t *testing.T) {
		sql, err := BuildUpdateQuery("postgres", "s", "t", []string{"a.b", "*", "z"}, []string{"c.d", "*"},
			map[string]any{"a.b": 1, "*": 2, "z": 3, "c.d": 4})
		require.NoError(t, err)
		require.Equal(t, `UPDATE "s"."t" SET "*"=2,"a.b"=1,"z"=3 WHERE (("c.d" = 4) AND ("*" = 2))`, sql)
	})
	t.Run("a question mark in a name or in a value is written as it is", func(t *testing.T) {
		sql, err := BuildUpdateQuery("postgres", "s", "t", []string{"a?", "b?"}, []string{"id"},
			map[string]any{"a?": "why?", "b?": "?", "id": 1})
		require.NoError(t, err)
		require.Equal(t, `UPDATE "s"."t" SET "a?"='why?',"b?"='?' WHERE ("id" = 1)`, sql)
	})
	t.Run("values are written as before", func(t *testing.T) {
		expected := map[string]string{
			"postgres":  `UPDATE "public"."users" SET "email"='a@b.c',"name"='Alice' WHERE (("id" = 1) AND ("other" IS NULL))`,
			"mysql":     "UPDATE `public`.`users` SET `email`='a@b.c',`name`='Alice' WHERE ((`id` = 1) AND (`other` IS NULL))",
			"sqlserver": `UPDATE "public"."users" SET "email"='a@b.c',"name"='Alice' WHERE (("id" = 1) AND ("other" IS NULL))`,
		}
		for driver, want := range expected {
			sql, err := BuildUpdateQuery(driver, "public", "users", []string{"name", "email"}, []string{"id", "other"},
				map[string]any{"name": "Alice", "id": 1, "email": "a@b.c", "other": nil})
			require.NoError(t, err)
			require.Equal(t, want, sql)
		}
		sql, err := BuildUpdateQuery("postgres", "public", "users", []string{"name", "note"}, []string{"id"},
			map[string]any{"name": nil, "note": "o'clock", "id": []int{1, 2}})
		require.NoError(t, err)
		require.Equal(t, `UPDATE "public"."users" SET "name"=NULL,"note"='o''clock' WHERE ("id" IN (1, 2))`, sql)

		sql, err = BuildUpdateQuery("postgres", "public", "users", []string{"name"}, nil, map[string]any{"name": "x"})
		require.NoError(t, err)
		require.Equal(t, `UPDATE "public"."users" SET "name"='x'`, sql)
	})
	t.Run("a column set after the first is written as one identifier", func(t *testing.T) {
		expected := map[string]struct{ column, want string }{
			"postgres":  {`z"z`, `UPDATE "s"."t" SET "a"=1,"z""z"=2 WHERE ("id" = 3)`},
			"mysql":     {"z`z", "UPDATE `s`.`t` SET `a`=1,`z``z`=2 WHERE (`id` = 3)"},
			"sqlserver": {`z"z`, `UPDATE "s"."t" SET "a"=1,"z""z"=2 WHERE ("id" = 3)`},
		}
		for driver, e := range expected {
			sql, err := BuildUpdateQuery(driver, "s", "t", []string{"a", e.column}, []string{"id"},
				map[string]any{"a": 1, e.column: 2, "id": 3})
			require.NoError(t, err)
			require.Equal(t, e.want, sql, driver)
		}
	})
	t.Run("no column to set is refused", func(t *testing.T) {
		_, err := BuildUpdateQuery("postgres", "public", "users", nil, []string{"id"}, map[string]any{"id": 1})
		require.Error(t, err)
	})
}

func upsertBuilder(t *testing.T, driver string, n oddNames, conflictColumns ...string) InsertQueryBuilder {
	t.Helper()
	builder, err := GetInsertBuilder(slog.New(slog.DiscardHandler), driver, n.schema, n.table, nil,
		WithOnConflictDoUpdate(conflictColumns))
	require.NoError(t, err)
	return builder
}

func Test_InsertBuilder_OnConflictDoUpdate_NamesAreWrittenAsOneIdentifier(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		n := oddPostgres
		rows := []map[string]any{
			{`i"d`: "1", `ke"y`: "k", n.column: "a", "name": "Alice"},
			{`i"d`: "2", `ke"y`: "l", n.column: "b", "name": "Bob"},
		}
		sql, args, err := upsertBuilder(t, "pgx", n, `i"d`, `ke"y`).BuildInsertQuery(rows)
		require.NoError(t, err)
		require.Equal(t,
			`INSERT INTO "sch""ema"."we""ird" ("co""l", "i""d", "ke""y", "name") VALUES ($1, $2, $3, $4), ($5, $6, $7, $8) `+
				`ON CONFLICT ("i""d", "ke""y") DO UPDATE SET "co""l"=EXCLUDED."co""l","name"=EXCLUDED."name"`,
			sql)
		require.Equal(t, []any{"a", "1", "k", "Alice", "b", "2", "l", "Bob"}, args)
	})
	t.Run("postgres, a conflict column in mixed case or named by a keyword", func(t *testing.T) {
		n := oddNames{schema: "public", table: "users"}
		sql, _, err := upsertBuilder(t, "pgx", n, "userId", "order").BuildInsertQuery(
			[]map[string]any{{"userId": "1", "order": "2", "name": "Alice"}})
		require.NoError(t, err)
		require.Equal(t,
			`INSERT INTO "public"."users" ("name", "order", "userId") VALUES ($1, $2, $3) `+
				`ON CONFLICT ("userId", "order") DO UPDATE SET "name"=EXCLUDED."name"`,
			sql)
	})
	t.Run("postgres, a column named *, holding a dot or a question mark", func(t *testing.T) {
		n := oddNames{schema: "s", table: "t"}
		sql, args, err := upsertBuilder(t, "pgx", n, "a.id").BuildInsertQuery(
			[]map[string]any{{"a.id": "1", "*": "2", "b.c": "3", "why?": "4"}})
		require.NoError(t, err)
		require.Equal(t,
			`INSERT INTO "s"."t" ("*", "a.id", "b.c", "why?") VALUES ($1, $2, $3, $4) `+
				`ON CONFLICT ("a.id") DO UPDATE SET "*"=EXCLUDED."*","b.c"=EXCLUDED."b.c","why?"=EXCLUDED."why?"`,
			sql)
		require.Equal(t, []any{"2", "1", "3", "4"}, args)
	})
	t.Run("mysql", func(t *testing.T) {
		n := oddMysql
		rows := []map[string]any{{"id": "1", n.column: "a"}, {"id": "2", n.column: "b"}}
		sql, args, err := upsertBuilder(t, "mysql", n, "id").BuildInsertQuery(rows)
		require.NoError(t, err)
		require.Equal(t,
			"INSERT INTO `sch``ema`.`we``ird` (`co``l`, `id`) VALUES (?, ?), (?, ?) "+
				"ON DUPLICATE KEY UPDATE `co``l`=VALUES(`co``l`),`id`=VALUES(`id`)",
			sql)
		require.Equal(t, []any{"a", "1", "b", "2"}, args)
	})
	t.Run("postgres, a column updated after the first is written as one identifier", func(t *testing.T) {
		n := oddNames{schema: "s", table: "t"}
		sql, _, err := upsertBuilder(t, "pgx", n, "id").BuildInsertQuery(
			[]map[string]any{{"id": "1", "a": "2", `z"z`: "3"}})
		require.NoError(t, err)
		require.Equal(t,
			`INSERT INTO "s"."t" ("a", "id", "z""z") VALUES ($1, $2, $3) `+
				`ON CONFLICT ("id") DO UPDATE SET "a"=EXCLUDED."a","z""z"=EXCLUDED."z""z"`,
			sql)
	})
	t.Run("mysql, a column updated after the first is written as one identifier", func(t *testing.T) {
		n := oddNames{schema: "s", table: "t"}
		sql, _, err := upsertBuilder(t, "mysql", n, "id").BuildInsertQuery(
			[]map[string]any{{"id": "1", "a": "2", "z`z": "3"}})
		require.NoError(t, err)
		require.Equal(t,
			"INSERT INTO `s`.`t` (`a`, `id`, `z``z`) VALUES (?, ?, ?) "+
				"ON DUPLICATE KEY UPDATE `a`=VALUES(`a`),`id`=VALUES(`id`),`z``z`=VALUES(`z``z`)",
			sql)
	})
	t.Run("mysql, a column named * or holding a dot", func(t *testing.T) {
		n := oddNames{schema: "s", table: "t"}
		sql, _, err := upsertBuilder(t, "mysql", n, "id").BuildInsertQuery([]map[string]any{{"*": "1", "a.b": "2"}})
		require.NoError(t, err)
		require.Equal(t,
			"INSERT INTO `s`.`t` (`*`, `a.b`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `*`=VALUES(`*`),`a.b`=VALUES(`a.b`)",
			sql)
	})
	t.Run("ordinary names", func(t *testing.T) {
		n := oddNames{schema: "public", table: "users"}
		rows := []map[string]any{
			{"id": 1, "name": "A", "email": "e"}, {"id": 2, "name": "B", "email": "f"},
		}
		sql, args, err := upsertBuilder(t, "mysql", n, "id", "name").BuildInsertQuery(rows)
		require.NoError(t, err)
		require.Equal(t,
			"INSERT INTO `public`.`users` (`email`, `id`, `name`) VALUES (?, ?, ?), (?, ?, ?) "+
				"ON DUPLICATE KEY UPDATE `email`=VALUES(`email`),`id`=VALUES(`id`),`name`=VALUES(`name`)",
			sql)
		require.Equal(t, []any{"e", int64(1), "A", "f", int64(2), "B"}, args)

		// The conflict columns are quoted like every other column.
		sql, args, err = upsertBuilder(t, "pgx", n, "id", "name").BuildInsertQuery(rows)
		require.NoError(t, err)
		require.Equal(t,
			`INSERT INTO "public"."users" ("email", "id", "name") VALUES ($1, $2, $3), ($4, $5, $6) `+
				`ON CONFLICT ("id", "name") DO UPDATE SET "email"=EXCLUDED."email"`,
			sql)
		require.Equal(t, []any{"e", int64(1), "A", "f", int64(2), "B"}, args)
	})
	t.Run("no row, and mysql rows without a column, are refused as before", func(t *testing.T) {
		n := oddNames{schema: "public", table: "users"}
		for _, driver := range []string{"pgx", "mysql"} {
			_, _, err := upsertBuilder(t, driver, n, "id").BuildInsertQuery(nil)
			require.ErrorContains(t, err, "no rows to insert")
		}
		_, _, err := upsertBuilder(t, "mysql", n, "id").BuildInsertQuery([]map[string]any{{}, {}})
		require.Error(t, err)
	})
}

func Test_InsertBuilder_SqlServerRowsWithoutAColumn(t *testing.T) {
	build := func(t *testing.T, schema, table string, rows []map[string]any) string {
		t.Helper()
		builder, err := GetInsertBuilder(slog.New(slog.DiscardHandler), "sqlserver", schema, table, nil)
		require.NoError(t, err)
		sql, args, err := builder.BuildInsertQuery(rows)
		require.NoError(t, err)
		require.Empty(t, args)
		return sql
	}
	t.Run("one statement for each row", func(t *testing.T) {
		require.Equal(t,
			`INSERT INTO "dbo"."users" DEFAULT VALUES;INSERT INTO "dbo"."users" DEFAULT VALUES;`,
			build(t, "dbo", "users", []map[string]any{{}, {}}))
	})
	t.Run("an ordinary name gives the text it always gave", func(t *testing.T) {
		require.Equal(t,
			`INSERT INTO "dbo"."users" DEFAULT VALUES;`,
			build(t, "dbo", "users", []map[string]any{{}}))
	})
	t.Run("an empty schema names the table alone", func(t *testing.T) {
		require.Equal(t,
			`INSERT INTO "users" DEFAULT VALUES;`,
			build(t, "", "users", []map[string]any{{}}))
	})
	t.Run("the names are written as one identifier each", func(t *testing.T) {
		require.Equal(t,
			`INSERT INTO "sch]ema"."we""i]rd\" DEFAULT VALUES;`,
			build(t, "sch]ema", `we"i]rd\`, []map[string]any{{}}))
	})
	t.Run("no row gives no statement", func(t *testing.T) {
		require.Empty(t, build(t, "dbo", "users", nil))
	})
}

// An empty name and a name holding a NUL byte are refused before a statement is written.
func Test_Builders_RefuseANameNoEngineTakes(t *testing.T) {
	no := false
	size := TableSize{Rows: 200_000, Pages: 100_000}
	ranges := []KeyRange{{From: 1, To: 2}}
	logger := slog.New(slog.DiscardHandler)

	tables := map[string][2]string{
		"empty table":          {"public", ""},
		"NUL byte in a table":  {"public", "us\x00ers"},
		"NUL byte in a schema": {"pub\x00lic", "users"},
	}
	withTable := map[string]func(schema, table string) error{
		"BuildSelectQuery": func(schema, table string) error {
			_, err := BuildSelectQuery("postgres", schema, table, []string{"id"}, nil)
			return err
		},
		"BuildSelectLimitQuery": func(schema, table string) error {
			_, err := BuildSelectLimitQuery("mysql", schema, table, 1)
			return err
		},
		"BuildSampledSelectLimitQuery": func(schema, table string) error {
			_, err := BuildSampledSelectLimitQuery("sqlserver", schema, table, 1, nil)
			return err
		},
		"BuildTableSampleQuery": func(schema, table string) error {
			_, _, err := BuildTableSampleQuery("postgres", schema, table, size, 1, nil)
			return err
		},
		"BuildKeySlicesSampleQuery": func(schema, table string) error {
			_, err := BuildKeySlicesSampleQuery("mysql", schema, table, "id", ranges, 1, nil)
			return err
		},
		"BuildKeySlicesCountQuery": func(schema, table string) error {
			_, err := BuildKeySlicesCountQuery("mysql", schema, table, "id", ranges)
			return err
		},
		"BuildTruncateQuery": func(schema, table string) error {
			_, err := BuildTruncateQuery("postgres", schema, table)
			return err
		},
		"BuildInsertQuery": func(schema, table string) error {
			_, _, err := BuildInsertQuery("mysql", schema, table, []goqu.Record{{"id": 1}}, &no)
			return err
		},
		"BuildUpdateQuery": func(schema, table string) error {
			_, err := BuildUpdateQuery("sqlserver", schema, table, []string{"name"}, []string{"id"},
				map[string]any{"name": 1, "id": 1})
			return err
		},
		"postgres on conflict do update": func(schema, table string) error {
			builder, err := GetInsertBuilder(logger, "pgx", schema, table, nil, WithOnConflictDoUpdate([]string{"id"}))
			require.NoError(t, err)
			_, _, err = builder.BuildInsertQuery([]map[string]any{{"id": 1, "name": 2}})
			return err
		},
		"mysql on duplicate key update": func(schema, table string) error {
			builder, err := GetInsertBuilder(logger, "mysql", schema, table, nil, WithOnConflictDoUpdate([]string{"id"}))
			require.NoError(t, err)
			_, _, err = builder.BuildInsertQuery([]map[string]any{{"id": 1, "name": 2}})
			return err
		},
		"sqlserver rows without a column": func(schema, table string) error {
			builder, err := GetInsertBuilder(logger, "sqlserver", schema, table, nil)
			require.NoError(t, err)
			_, _, err = builder.BuildInsertQuery([]map[string]any{{}})
			return err
		},
	}
	for name, build := range withTable {
		for label, names := range tables {
			t.Run(name+"/"+label, func(t *testing.T) {
				require.Error(t, build(names[0], names[1]))
			})
		}
	}

	columns := map[string]string{"empty column": "", "NUL byte in a column": "na\x00me"}
	withColumn := map[string]func(column string) error{
		"BuildSelectQuery": func(column string) error {
			_, err := BuildSelectQuery("postgres", "public", "users", []string{"id", column}, nil)
			return err
		},
		"BuildKeySlicesSampleQuery": func(column string) error {
			_, err := BuildKeySlicesSampleQuery("mysql", "public", "users", column, ranges, 1, nil)
			return err
		},
		"BuildKeySlicesCountQuery": func(column string) error {
			_, err := BuildKeySlicesCountQuery("mysql", "public", "users", column, ranges)
			return err
		},
		"BuildInsertQuery": func(column string) error {
			_, _, err := BuildInsertQuery("sqlserver", "public", "users", []goqu.Record{{"id": 1, column: 2}}, &no)
			return err
		},
		"BuildUpdateQuery, a column to set": func(column string) error {
			_, err := BuildUpdateQuery("postgres", "public", "users", []string{"name", column}, []string{"id"},
				map[string]any{"name": 1, column: 2, "id": 3})
			return err
		},
		"BuildUpdateQuery, a column of the condition": func(column string) error {
			_, err := BuildUpdateQuery("mysql", "public", "users", []string{"name"}, []string{"id", column},
				map[string]any{"name": 1, column: 2, "id": 3})
			return err
		},
		"postgres on conflict do update, a column to update": func(column string) error {
			builder, err := GetInsertBuilder(logger, "pgx", "public", "users", nil, WithOnConflictDoUpdate([]string{"id"}))
			require.NoError(t, err)
			_, _, err = builder.BuildInsertQuery([]map[string]any{{"id": 1, "name": 2, column: 3}})
			return err
		},
		"postgres on conflict do update, a conflict column": func(column string) error {
			builder, err := GetInsertBuilder(logger, "pgx", "public", "users", nil,
				WithOnConflictDoUpdate([]string{"id", column}))
			require.NoError(t, err)
			_, _, err = builder.BuildInsertQuery([]map[string]any{{"id": 1, "name": 2}})
			return err
		},
		"mysql on duplicate key update": func(column string) error {
			builder, err := GetInsertBuilder(logger, "mysql", "public", "users", nil, WithOnConflictDoUpdate([]string{"id"}))
			require.NoError(t, err)
			_, _, err = builder.BuildInsertQuery([]map[string]any{{"id": 1, column: 3}})
			return err
		},
	}
	for name, build := range withColumn {
		for label, column := range columns {
			t.Run(name+"/"+label, func(t *testing.T) {
				require.Error(t, build(column))
			})
		}
	}
}

func Test_Builders_RefuseAnUnknownDriver(t *testing.T) {
	no := false
	_, err := BuildSelectQuery("oracle", "s", "t", nil, nil)
	require.Error(t, err)
	_, err = BuildSelectLimitQuery("oracle", "s", "t", 1)
	require.Error(t, err)
	_, err = BuildSampledSelectLimitQuery("oracle", "s", "t", 1, nil)
	require.Error(t, err)
	_, err = BuildKeySlicesSampleQuery("oracle", "s", "t", "id", []KeyRange{{From: 1, To: 2}}, 1, nil)
	require.Error(t, err)
	_, err = BuildKeySlicesCountQuery("oracle", "s", "t", "id", []KeyRange{{From: 1, To: 2}})
	require.Error(t, err)
	_, err = BuildTruncateQuery("oracle", "s", "t")
	require.Error(t, err)
	_, _, err = BuildInsertQuery("oracle", "s", "t", []goqu.Record{{"a": 1}}, &no)
	require.Error(t, err)
	_, err = BuildUpdateQuery("oracle", "s", "t", []string{"a"}, []string{"id"}, map[string]any{"a": 1, "id": 1})
	require.Error(t, err)
}
