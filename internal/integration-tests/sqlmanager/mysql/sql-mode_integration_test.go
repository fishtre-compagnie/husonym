package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	mysql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mysql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	"github.com/stretchr/testify/require"
)

// A session inherits its sql_mode from the server. Under NO_BACKSLASH_ESCAPES a backslash
// inside '…' is an ordinary character. The catalog of a table is read the same way with or
// without that mode, a generated column included.
func Test_MysqlManager_ReadsTheCatalogUnderEitherBackslashMode(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := context.Background()
	container, err := tcmysql.NewMysqlTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	const schema = "sqlmode"
	for _, statement := range []string{
		"CREATE DATABASE " + schema,
		"CREATE TABLE " + schema + ".t (id INT NOT NULL, a VARCHAR(20), " +
			"g VARCHAR(40) AS (CONCAT(a, 'x')) STORED, PRIMARY KEY (id))",
	} {
		_, err := container.DB.ExecContext(ctx, statement)
		require.NoError(t, err, statement)
	}
	tables := []*sqlmanager_shared.SchemaTable{{Schema: schema, Table: "t"}}

	read := func(t *testing.T, mode string) (createTable string, generated string) {
		t.Helper()
		dsn, err := mysqldriver.ParseDSN(container.URL)
		require.NoError(t, err)
		if mode != "" {
			if dsn.Params == nil {
				dsn.Params = map[string]string{}
			}
			dsn.Params["sql_mode"] = "CONCAT(@@sql_mode, '," + mode + "')"
		}
		db, err := sql.Open(sqlmanager_shared.MysqlDriver, dsn.FormatDSN())
		require.NoError(t, err)
		defer db.Close()

		var shown string
		require.NoError(t, db.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&shown))
		if mode == "" {
			require.NotContains(t, shown, "NO_BACKSLASH_ESCAPES")
		} else {
			require.Contains(t, shown, mode)
		}

		manager := mysql.NewManager(mysql_queries.New(), db, func() {})
		statements, err := manager.GetTableInitStatements(ctx, tables)
		require.NoError(t, err)
		require.Len(t, statements, 1)

		rows, err := manager.GetDatabaseTableSchemasBySchemasAndTables(ctx, tables)
		require.NoError(t, err)
		for _, row := range rows {
			if row.ColumnName == "g" {
				require.NotNil(t, row.GeneratedExpression)
				generated = *row.GeneratedExpression
			}
		}
		return statements[0].CreateTableStatement, generated
	}

	wantCreateTable, wantGenerated := read(t, "")
	require.Equal(t, "concat(`a`,_utf8mb4'x')", wantGenerated)

	createTable, generated := read(t, "NO_BACKSLASH_ESCAPES")
	require.Equal(t, wantGenerated, generated)
	require.Equal(t, wantCreateTable, createTable)
}
