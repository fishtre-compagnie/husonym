package querybuilder

import (
	"fmt"
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
	t.Run("postgres draws a share of the pages sized for the window", func(t *testing.T) {
		sql, ok, err := BuildTableSampleQuery(sqlmanager_shared.GoquPostgresDriver, "public.accounts", 200_000, 10)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t,
			`SELECT * FROM (SELECT * FROM "public"."accounts" TABLESAMPLE SYSTEM (0.5)) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 10`,
			sql)
	})
	t.Run("postgres share is rounded to four decimals", func(t *testing.T) {
		sql, ok, err := BuildTableSampleQuery(sqlmanager_shared.GoquPostgresDriver, "public.accounts", 3_000, 10)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, sql, "TABLESAMPLE SYSTEM (33.3333)")
	})
	t.Run("postgres share never drops to zero", func(t *testing.T) {
		sql, ok, err := BuildTableSampleQuery(sqlmanager_shared.GoquPostgresDriver, "public.accounts", 100_000_000_000, 10)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, sql, "TABLESAMPLE SYSTEM (0.0001)")
	})
	t.Run("postgres share never exceeds 100", func(t *testing.T) {
		sql, ok, err := BuildTableSampleQuery(sqlmanager_shared.GoquPostgresDriver, "public.accounts", SampleWindowSize+1, 10)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, sql, "TABLESAMPLE SYSTEM (99.9001)")
	})
	t.Run("sqlserver asks for the window in rows", func(t *testing.T) {
		sql, ok, err := BuildTableSampleQuery(sqlmanager_shared.MssqlDriver, "dbo.accounts", 200_000, 10)
		require.NoError(t, err)
		require.True(t, ok)
		require.Contains(t, sql, `TABLESAMPLE (1000 ROWS)`)
		require.Contains(t, sql, `NEWID()`)
		require.NotContains(t, sql, "TOP (1000)")
	})
	t.Run("sqlserver does not depend on the estimate", func(t *testing.T) {
		for _, rows := range []int64{-1, 0, 1, 1000, 200_000} {
			_, ok, err := BuildTableSampleQuery(sqlmanager_shared.MssqlDriver, "dbo.accounts", rows, 10)
			require.NoError(t, err)
			require.True(t, ok)
		}
	})
	t.Run("a table that fits the window is not sampled", func(t *testing.T) {
		for _, rows := range []int64{-1, 0, 1, 1000} {
			_, ok, err := BuildTableSampleQuery(sqlmanager_shared.GoquPostgresDriver, "public.accounts", rows, 10)
			require.NoError(t, err)
			require.False(t, ok)
		}
	})
	t.Run("mysql has no table sample", func(t *testing.T) {
		_, ok, err := BuildTableSampleQuery(sqlmanager_shared.MysqlDriver, "db.accounts", 200_000, 10)
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func Test_BuildKeySlicesSampleQuery(t *testing.T) {
	sql, err := BuildKeySlicesSampleQuery(sqlmanager_shared.MysqlDriver, "db.accounts", "id", []int64{5, 9000}, 10)
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(sql, "LIMIT 100"))
	require.Contains(t, sql, "`id` >= 5")
	require.Contains(t, sql, "`id` >= 9000")
	require.Contains(t, sql, "UNION ALL")
	require.True(t, strings.HasSuffix(sql, "ORDER BY RAND() ASC LIMIT 10"))

	_, err = BuildKeySlicesSampleQuery(sqlmanager_shared.MysqlDriver, "db.accounts", "id", nil, 10)
	require.Error(t, err)
}
