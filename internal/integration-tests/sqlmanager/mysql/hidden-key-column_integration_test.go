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

// A user granted some columns of a table sees the keys of that table in the catalog, without
// the names of the key columns it was not granted. The table is still read: the statement of
// such a key carries an empty column name, which the destination answers alone.
func Test_MysqlManager_ReadsATableWhoseKeyColumnTheUserCannotSee(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := context.Background()
	container, err := tcmysql.NewMysqlTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	const schema = "hiddenkey"
	for _, statement := range []string{
		"CREATE DATABASE " + schema,
		"CREATE TABLE " + schema + ".parent (id INT NOT NULL, PRIMARY KEY (id))",
		"CREATE TABLE " + schema + ".t (id INT NOT NULL, a VARCHAR(20), b INT, parent_id INT, " +
			"PRIMARY KEY (id), CONSTRAINT t_parent FOREIGN KEY (parent_id) REFERENCES " + schema + ".parent (id))",
		"CREATE USER 'columns_only'@'%' IDENTIFIED BY 'columns_only'",
		"GRANT SELECT (a, b) ON " + schema + ".t TO 'columns_only'@'%'",
	} {
		_, err := container.DB.ExecContext(ctx, statement)
		require.NoError(t, err, statement)
	}

	dsn, err := mysqldriver.ParseDSN(container.URL)
	require.NoError(t, err)
	// No default database: the account holds nothing on the container's own.
	dsn.User, dsn.Passwd, dsn.DBName = "columns_only", "columns_only", ""
	userDB, err := sql.Open(sqlmanager_shared.MysqlDriver, dsn.FormatDSN())
	require.NoError(t, err)
	defer userDB.Close()

	statements, err := mysql.NewManager(mysql_queries.New(), userDB, func() {}).GetTableInitStatements(
		ctx, []*sqlmanager_shared.SchemaTable{{Schema: schema, Table: "t"}})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	// The user sees the two columns it was granted, and each key without its column.
	require.Contains(t, statements[0].CreateTableStatement, "`a`")
	require.Contains(t, statements[0].CreateTableStatement, "`b`")
	require.NotContains(t, statements[0].CreateTableStatement, "`id`")
	keys := map[sqlmanager_shared.ConstraintType][]string{}
	for _, alter := range statements[0].AlterTableStatements {
		keys[alter.ConstraintType] = append(keys[alter.ConstraintType], alter.Statement)
	}
	require.Len(t, keys[sqlmanager_shared.PrimaryConstraintType], 1)
	require.Contains(t, keys[sqlmanager_shared.PrimaryConstraintType][0],
		"ALTER TABLE `"+schema+"`.`t` ADD PRIMARY KEY (``);")
	require.Len(t, keys[sqlmanager_shared.ForeignConstraintType], 1)
	require.Contains(t, keys[sqlmanager_shared.ForeignConstraintType][0],
		"ALTER TABLE `"+schema+"`.`t` ADD CONSTRAINT `t_parent` FOREIGN KEY (``) REFERENCES ")
}
