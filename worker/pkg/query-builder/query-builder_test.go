package querybuilder

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func Test_BuildSelectQuery(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		table    string
		columns  []string
		where    string
		expected string
	}{
		{
			name:     "postgres select",
			driver:   sqlmanager_shared.PostgresDriver,
			table:    "public.accounts",
			columns:  []string{"id", "name"},
			where:    "",
			expected: `SELECT "id", "name" FROM "public"."accounts";`,
		},
		{
			name:     "postgres select with where",
			driver:   sqlmanager_shared.PostgresDriver,
			table:    "public.accounts",
			columns:  []string{"id", "name"},
			where:    `"id" = 'some-id'`,
			expected: `SELECT "id", "name" FROM "public"."accounts" WHERE "id" = 'some-id';`,
		},
		{
			name:     "postgres select with where prepared",
			driver:   sqlmanager_shared.PostgresDriver,
			table:    "public.accounts",
			columns:  []string{"id", "name"},
			where:    `"id" = $1`,
			expected: `SELECT "id", "name" FROM "public"."accounts" WHERE "id" = $1;`,
		},
		{
			name:     "mysql select",
			driver:   sqlmanager_shared.MysqlDriver,
			table:    "public.accounts",
			columns:  []string{"id", "name"},
			where:    "",
			expected: "SELECT `id`, `name` FROM `public`.`accounts`;",
		},
		{
			name:     "mysql select with where",
			driver:   sqlmanager_shared.MysqlDriver,
			table:    "public.accounts",
			columns:  []string{"id", "name"},
			where:    "`id` = 'some-id'",
			expected: "SELECT `id`, `name` FROM `public`.`accounts` WHERE `id` = 'some-id';",
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", t.Name(), tt.name), func(t *testing.T) {
			where := tt.where
			sql, err := BuildSelectQuery(tt.driver, tt.table, tt.columns, &where)
			require.NoError(t, err)
			require.Equal(t, tt.expected, sql)
		})
	}
}

func Test_BuildSampledSelectLimitQuery(t *testing.T) {
	t.Run("postgres sample with limit", func(t *testing.T) {
		driver := sqlmanager_shared.GoquPostgresDriver
		table := "public.accounts"
		limit := uint(10)
		expected := `SELECT * FROM (SELECT * FROM "public"."accounts" LIMIT 1000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`

		sql, err := BuildSampledSelectLimitQuery(driver, table, limit)
		require.NoError(t, err)
		require.Equal(t, expected, sql)
	})

	t.Run("postgres sample with schema.table format", func(t *testing.T) {
		driver := sqlmanager_shared.GoquPostgresDriver
		table := "schema.table_name"
		limit := uint(100)
		expected := `SELECT * FROM (SELECT * FROM "schema"."table_name" LIMIT 1000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 100`

		sql, err := BuildSampledSelectLimitQuery(driver, table, limit)
		require.NoError(t, err)
		require.Equal(t, expected, sql)
	})

	t.Run("mysql sample with limit", func(t *testing.T) {
		driver := sqlmanager_shared.MysqlDriver
		table := "public.accounts"
		limit := uint(10)
		expected := "SELECT * FROM (SELECT * FROM `public`.`accounts` LIMIT 1000) AS `husonym_sample` ORDER BY RAND() ASC LIMIT 10"

		sql, err := BuildSampledSelectLimitQuery(driver, table, limit)
		require.NoError(t, err)
		require.Equal(t, expected, sql)
	})

	t.Run("mysql sample with schema.table format", func(t *testing.T) {
		driver := sqlmanager_shared.MysqlDriver
		table := "schema.table_name"
		limit := uint(100)
		expected := "SELECT * FROM (SELECT * FROM `schema`.`table_name` LIMIT 1000) AS `husonym_sample` ORDER BY RAND() ASC LIMIT 100"

		sql, err := BuildSampledSelectLimitQuery(driver, table, limit)
		require.NoError(t, err)
		require.Equal(t, expected, sql)
	})
	t.Run("mssql sample with limit", func(t *testing.T) {
		driver := sqlmanager_shared.MssqlDriver
		table := "public.accounts"
		limit := uint(10)
		expected := `SELECT  TOP (10) * FROM (SELECT  TOP (1000) * FROM "public"."accounts") AS "husonym_sample" ORDER BY NEWID() ASC`

		sql, err := BuildSampledSelectLimitQuery(driver, table, limit)
		require.NoError(t, err)
		require.Equal(t, expected, sql)
	})

	t.Run("mssql sample with schema.table format", func(t *testing.T) {
		driver := sqlmanager_shared.MssqlDriver
		table := "schema.table_name"
		limit := uint(100)
		expected := `SELECT  TOP (100) * FROM (SELECT  TOP (1000) * FROM "schema"."table_name") AS "husonym_sample" ORDER BY NEWID() ASC`

		sql, err := BuildSampledSelectLimitQuery(driver, table, limit)
		require.NoError(t, err)
		require.Equal(t, expected, sql)
	})
}

func Test_BuildUpdateQuery(t *testing.T) {
	tests := []struct {
		name           string
		driver         string
		schema         string
		table          string
		insertColumns  []string
		whereColumns   []string
		columnValueMap map[string]any
		expected       string
	}{
		{
			"Single Column postgres",
			"postgres",
			"public",
			"users",
			[]string{"name"},
			[]string{"id"},
			map[string]any{"name": "Alice", "id": 1},
			`UPDATE "public"."users" SET "name"='Alice' WHERE ("id" = 1)`,
		},
		{
			"Special characters postgres",
			"postgres",
			"public",
			"users.stage$dev",
			[]string{"name"},
			[]string{"id"},
			map[string]any{"name": "Alice", "id": 1},
			`UPDATE "public"."users.stage$dev" SET "name"='Alice' WHERE ("id" = 1)`,
		},
		{
			"Multiple Primary Keys postgres",
			"postgres",
			"public",
			"users",
			[]string{"name", "email"},
			[]string{"id", "other"},
			map[string]any{"name": "Alice", "id": 1, "email": "alice@fake.com", "other": "blah"},
			`UPDATE "public"."users" SET "email"='alice@fake.com',"name"='Alice' WHERE (("id" = 1) AND ("other" = 'blah'))`,
		},
		{
			"Single Column mysql",
			"mysql",
			"public",
			"users",
			[]string{"name"},
			[]string{"id"},
			map[string]any{"name": "Alice", "id": 1},
			"UPDATE `public`.`users` SET `name`='Alice' WHERE (`id` = 1)",
		},
		{
			"Multiple Primary Keys mysql",
			"mysql",
			"public",
			"users",
			[]string{"name", "email"},
			[]string{"id", "other"},
			map[string]any{"name": "Alice", "id": 1, "email": "alice@fake.com", "other": "blah"},
			"UPDATE `public`.`users` SET `email`='alice@fake.com',`name`='Alice' WHERE ((`id` = 1) AND (`other` = 'blah'))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := BuildUpdateQuery(
				tt.driver,
				tt.schema,
				tt.table,
				tt.insertColumns,
				tt.whereColumns,
				tt.columnValueMap,
			)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func Test_BuildInsertQuery(t *testing.T) {
	tests := []struct {
		name                string
		driver              string
		schema              string
		table               string
		records             []map[string]any
		onConflictDoNothing bool
		expected            string
		expectedArgs        []any
	}{
		{
			name:                "Single Column mysql",
			driver:              "mysql",
			schema:              "public",
			table:               "users",
			records:             []map[string]any{{"name": "Alice"}, {"name": "Bob"}},
			onConflictDoNothing: false,
			expected:            "INSERT INTO `public`.`users` (`name`) VALUES (?), (?)",
			expectedArgs:        []any{"Alice", "Bob"},
		},
		{
			name:                "Special characters mysql",
			driver:              "mysql",
			schema:              "public",
			table:               "users.stage$dev",
			records:             []map[string]any{{"name": "Alice"}, {"name": "Bob"}},
			onConflictDoNothing: false,
			expected:            "INSERT INTO `public`.`users.stage$dev` (`name`) VALUES (?), (?)",
			expectedArgs:        []any{"Alice", "Bob"},
		},
		{
			name:   "Multiple Columns mysql",
			driver: "mysql",
			schema: "public",
			table:  "users",
			records: []map[string]any{
				{"name": "Alice", "email": "alice@fake.com"},
				{"name": "Bob", "email": "bob@fake.com"},
			},
			onConflictDoNothing: true,
			expected:            "INSERT INTO `public`.`users` (`email`, `name`) VALUES (?, ?), (?, ?) ON DUPLICATE KEY UPDATE `email`=`email`",
			expectedArgs:        []any{"alice@fake.com", "Alice", "bob@fake.com", "Bob"},
		},
		{
			name:                "Single Column postgres",
			driver:              "postgres",
			schema:              "public",
			table:               "users",
			records:             []map[string]any{{"name": "Alice"}, {"name": "Bob"}},
			onConflictDoNothing: false,
			expected:            `INSERT INTO "public"."users" ("name") VALUES ($1), ($2)`,
			expectedArgs:        []any{"Alice", "Bob"},
		},
		{
			name:   "Multiple Columns postgres",
			driver: "postgres",
			schema: "public",
			table:  "users",
			records: []map[string]any{
				{"name": "Alice", "email": "alice@fake.com"},
				{"name": "Bob", "email": "bob@fake.com"},
			},
			onConflictDoNothing: true,
			expected:            `INSERT INTO "public"."users" ("email", "name") VALUES ($1, $2), ($3, $4) ON CONFLICT DO NOTHING`,
			expectedArgs:        []any{"alice@fake.com", "Alice", "bob@fake.com", "Bob"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			goquRows := toGoquRecords(tt.records)
			actual, args, err := BuildInsertQuery(
				tt.driver,
				tt.schema,
				tt.table,
				goquRows,
				&tt.onConflictDoNothing,
			)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
			require.Equal(t, tt.expectedArgs, args)
		})
	}
}

func Test_BuildTableSampleQuery(t *testing.T) {
	const pg, mssql = sqlmanager_shared.GoquPostgresDriver, sqlmanager_shared.MssqlDriver
	build := func(t *testing.T, driver string, size TableSize) string {
		t.Helper()
		sql, ok, err := BuildTableSampleQuery(driver, "public.accounts", size, 10)
		require.NoError(t, err)
		require.True(t, ok)
		return sql
	}

	t.Run("postgres thins the rows of the pages it draws when they hold more than the window", func(t *testing.T) {
		// 107 rows a page: SampleMinPages pages are 2.6738 percent and hold 5 348 rows.
		require.Equal(t,
			`SELECT * FROM (SELECT * FROM "public"."accounts" TABLESAMPLE SYSTEM (2.6738) WHERE RANDOM() < 0.187 LIMIT 4000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`,
			build(t, pg, TableSize{Rows: 200_000, Pages: 1870}))
	})
	t.Run("postgres keeps every row of the pages that hold the window", func(t *testing.T) {
		// 2 rows a page: the window is on 500 pages, 0.5 percent.
		require.Equal(t,
			`SELECT * FROM (SELECT * FROM "public"."accounts" TABLESAMPLE SYSTEM (0.5) LIMIT 4000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`,
			build(t, pg, TableSize{Rows: 200_000, Pages: 100_000}))
	})
	t.Run("postgres bounds the rows inside the sample, after the thinning", func(t *testing.T) {
		require.Equal(t, 4*SampleWindowSize, SampleRowsBound)
		bounded := regexp.MustCompile(
			`^SELECT \* FROM \(SELECT \* FROM "public"\."accounts" TABLESAMPLE SYSTEM \([0-9.]+\)( WHERE RANDOM\(\) < [0-9.]+)? LIMIT 4000\) AS "husonym_sample" ORDER BY RANDOM\(\) ASC LIMIT 10$`)
		for _, size := range []TableSize{
			{Rows: 200_000, Pages: 1870},
			{Rows: 200_000, Pages: 100_000},
			{Rows: 5_000, Pages: 27},
			{Rows: SampleWindowSize + 1, Pages: 1},
			{Rows: 100_000_000_000, Pages: 1_000_000_000},
		} {
			require.Regexp(t, bounded, build(t, pg, size))
		}
	})
	t.Run("the share is rounded to four decimals", func(t *testing.T) {
		require.Contains(t, build(t, pg, TableSize{Rows: 3_000, Pages: 3_000}), "TABLESAMPLE SYSTEM (33.3333) LIMIT")
		require.Contains(t, build(t, mssql, TableSize{Rows: 3_000, Pages: 3_000}), "TABLESAMPLE (33.3333 PERCENT))")
	})
	t.Run("the share never drops to zero", func(t *testing.T) {
		huge := TableSize{Rows: 100_000_000_000, Pages: 100_000_000_000}
		require.Contains(t, build(t, pg, huge), "TABLESAMPLE SYSTEM (0.0001) LIMIT")
		require.Contains(t, build(t, mssql, huge), "TABLESAMPLE (0.0001 PERCENT))")
	})
	t.Run("the share never exceeds 100", func(t *testing.T) {
		// Fewer pages than SampleMinPages: all of them are read and one row in five is kept.
		few := TableSize{Rows: 5_000, Pages: 27}
		require.Contains(t, build(t, pg, few), "TABLESAMPLE SYSTEM (100) WHERE RANDOM() < 0.2 LIMIT 4000")
		require.Contains(t, build(t, mssql, few), "TABLESAMPLE (100 PERCENT))")
	})
	t.Run("the share covers the window when its pages are more than SampleMinPages", func(t *testing.T) {
		require.Contains(t, build(t, pg, TableSize{Rows: 200_000, Pages: 40_000}), "TABLESAMPLE SYSTEM (0.5) LIMIT")
	})
	t.Run("sqlserver orders every row of the pages it draws", func(t *testing.T) {
		// 235 rows a page: SampleMinPages pages are 5.8685 percent and hold 11 737 rows.
		require.Equal(t,
			`SELECT  TOP (10) * FROM (SELECT * FROM "public"."accounts" TABLESAMPLE (5.8685 PERCENT)) AS "husonym_sample" ORDER BY NEWID() ASC`,
			build(t, mssql, TableSize{Rows: 200_000, Pages: 852}))
		// 2 rows a page: the window is on 500 pages, 0.5 percent.
		require.Equal(t,
			`SELECT  TOP (10) * FROM (SELECT * FROM "public"."accounts" TABLESAMPLE (0.5 PERCENT)) AS "husonym_sample" ORDER BY NEWID() ASC`,
			build(t, mssql, TableSize{Rows: 200_000, Pages: 100_000}))
	})
	t.Run("sqlserver filters no row", func(t *testing.T) {
		for _, size := range []TableSize{
			{Rows: 200_000, Pages: 852}, {Rows: 200_000, Pages: 100_000}, {Rows: 5_000, Pages: 27},
		} {
			sql := build(t, mssql, size)
			require.NotContains(t, sql, "WHERE")
			require.NotContains(t, sql, "CHECKSUM")
			require.NotContains(t, sql, "physloc")
		}
	})
	t.Run("sqlserver does not cut the sample before the random order", func(t *testing.T) {
		require.Equal(t, 1, strings.Count(build(t, mssql, TableSize{Rows: 200_000, Pages: 852}), "TOP ("))
	})
	t.Run("a table that fits the window, or whose size is unknown, is not sampled", func(t *testing.T) {
		for _, driver := range []string{pg, mssql} {
			for _, size := range []TableSize{
				{Rows: -1, Pages: 10}, {Rows: 0, Pages: 0}, {Rows: 1, Pages: 1}, {Rows: SampleWindowSize, Pages: 10},
				{Rows: 200_000, Pages: 0}, {Rows: 200_000, Pages: -1},
			} {
				_, ok, err := BuildTableSampleQuery(driver, "public.accounts", size, 10)
				require.NoError(t, err)
				require.False(t, ok, "%s %+v", driver, size)
			}
		}
	})
	t.Run("mysql has no table sample", func(t *testing.T) {
		_, ok, err := BuildTableSampleQuery(sqlmanager_shared.MysqlDriver, "db.accounts", TableSize{Rows: 200_000, Pages: 1870}, 10)
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func Test_BuildKeySlicesSampleQuery(t *testing.T) {
	ranges := []KeyRange{{From: 5, To: 8999}, {From: 9000, To: 20000}}
	sql, err := BuildKeySlicesSampleQuery(sqlmanager_shared.MysqlDriver, "db.accounts", "id", ranges, 10)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(sql, "LIMIT 100"))
	require.Contains(t, sql, "`id` >= 5")
	require.Contains(t, sql, "`id` <= 8999")
	require.Contains(t, sql, "`id` >= 9000")
	require.Contains(t, sql, "`id` <= 20000")
	require.Contains(t, sql, "UNION ALL")
	require.True(t, strings.HasSuffix(sql, "ORDER BY RAND() ASC LIMIT 10"))

	_, err = BuildKeySlicesSampleQuery(sqlmanager_shared.MysqlDriver, "db.accounts", "id", nil, 10)
	require.Error(t, err)
}
