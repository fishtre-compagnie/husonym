package connectionchecks_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcmysql "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/mysql"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

// The checks against a real server: what the probes answer, and whether a remedy, run as it is
// written, is enough — the one thing a mock cannot say.
func Test_ConnectionChecks_Mysql(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	for name, image := range map[string]string{"mysql": "", "mariadb": "mariadb:11.4"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var opts []tcmysql.Option
			if image != "" {
				opts = append(opts, tcmysql.WithImage(image))
			}
			server, err := tcmysql.NewMysqlTestContainer(ctx, opts...)
			require.NoError(t, err)
			t.Cleanup(func() { _ = server.TearDown(ctx) })
			admin := server.DB

			for _, statement := range []string{
				"CREATE DATABASE checks",
				"CREATE TABLE checks.orders (id INT PRIMARY KEY, note TEXT)",
				"CREATE USER 'bare'@'%' IDENTIFIED BY 'bare'",
			} {
				_, err := admin.ExecContext(ctx, statement)
				require.NoError(t, err, statement)
			}
			dsn, err := mysqldriver.ParseDSN(server.URL)
			require.NoError(t, err)
			dsn.User, dsn.Passwd, dsn.DBName = "bare", "bare", ""
			bare, err := sql.Open("mysql", dsn.FormatDSN())
			require.NoError(t, err)
			t.Cleanup(func() { _ = bare.Close() })

			orders := []*connectionchecks.Table{{Schema: "checks", Table: "orders"}}
			absent := []*connectionchecks.Table{{Schema: "checks", Table: "nowhere"}}

			// An account that holds nothing on a table sees none of its columns: asked
			// without them, it is still told what it lacks, and how to get it.
			findings, err := connectionchecks.Source(ctx, bare, connectionchecks.MySQL, "reporting", orders)
			require.NoError(t, err)
			require.Len(t, findings, 1)
			require.Equal(t, connectionchecks.CheckReadable, findings[0].Check)
			_, err = admin.ExecContext(ctx, findings[0].Remedy)
			require.NoError(t, err, "the remedy runs as written: %s", findings[0].Remedy)
			findings, err = connectionchecks.Source(ctx, bare, connectionchecks.MySQL, "reporting", orders)
			require.NoError(t, err)
			require.Empty(t, findings, "and it is enough")

			// A table that is not there looks, to an account holding nothing on its database,
			// like one it may not read: MySQL hides the tables of such a database.
			findings, err = connectionchecks.Source(ctx, bare, connectionchecks.MySQL, "reporting", absent)
			require.NoError(t, err)
			require.Len(t, findings, 1)
			require.Equal(t, connectionchecks.CheckReadable, findings[0].Check)
			// Holding something on the database, the account is told it is absent, with nothing
			// to grant.
			_, err = admin.ExecContext(ctx, "GRANT SELECT ON checks.* TO 'bare'@'%'")
			require.NoError(t, err)
			findings, err = connectionchecks.Source(ctx, bare, connectionchecks.MySQL, "reporting", absent)
			require.NoError(t, err)
			require.Len(t, findings, 1)
			require.Equal(t, connectionchecks.CheckTableExists, findings[0].Check)
			require.Empty(t, findings[0].Remedy)

			// Written, the table lacks the rest; each remedy, run in turn, clears its finding.
			columns := []*connectionchecks.Table{{Schema: "checks", Table: "orders", Columns: []string{"id", "note"}}}
			destination := func() []*connectionchecks.Finding {
				findings, err := connectionchecks.Destination(ctx, bare, connectionchecks.MySQL, "staging", columns,
					connectionchecks.DestinationOptions{Truncates: true})
				require.NoError(t, err)
				return findings
			}
			findings = destination()
			require.NotEmpty(t, findings)
			for _, finding := range findings {
				require.NotEmpty(t, finding.Remedy, finding.Message)
				_, err := admin.ExecContext(ctx, finding.Remedy)
				require.NoError(t, err, finding.Remedy)
			}
			require.Empty(t, destination(), "every remedy together is enough")

			// A trigger defined by another account comes back only with the definer
			// privilege, which is not handed out as a remedy; held, the check passes.
			_, err = admin.ExecContext(ctx,
				"CREATE TRIGGER checks.stamp BEFORE INSERT ON checks.orders FOR EACH ROW SET NEW.note = NEW.note")
			require.NoError(t, err)
			findings = destination()
			require.Len(t, findings, 1)
			require.Equal(t, connectionchecks.CheckTriggerDefiner, findings[0].Check)
			require.Empty(t, findings[0].Remedy)
			// MySQL 8.4 has SET_ANY_DEFINER in place of SET_USER_ID; MariaDB, SET USER.
			definer := "SET_ANY_DEFINER"
			if name == "mariadb" {
				definer = "SET USER"
			}
			_, err = admin.ExecContext(ctx, fmt.Sprintf("GRANT %s ON *.* TO 'bare'@'%%'", definer))
			require.NoError(t, err)
			require.Empty(t, destination())
		})
	}
}
