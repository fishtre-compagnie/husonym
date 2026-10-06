package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// PostgreSQL reports a column default as the session writes it: with
// standard_conforming_strings off, each backslash of a string twice. The schema rows say
// which of the two the session did, so that the text can be read back as a value.
func Test_PostgresManager_ColumnDefaultTellsTheStringSetting(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	ctx := context.Background()

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	admin, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	require.NoError(t, err)
	defer admin.Close()

	cases := []struct {
		setting      string
		wantDefault  string
		writtenTwice bool
	}{
		{"on", `'none\'::text`, false},
		{"off", `'none\\'::text`, true},
	}
	for _, c := range cases {
		t.Run("standard_conforming_strings "+c.setting, func(t *testing.T) {
			database := "defaults_" + c.setting
			for _, statement := range []string{
				"CREATE DATABASE " + database,
				"ALTER DATABASE " + database + " SET standard_conforming_strings = " + c.setting,
			} {
				_, err := admin.ExecContext(ctx, statement)
				require.NoError(t, err, statement)
			}
			address, err := url.Parse(container.URL)
			require.NoError(t, err)
			address.Path = "/" + database
			db, err := sql.Open(sqlmanager_shared.PostgresDriver, address.String())
			require.NoError(t, err)
			defer db.Close()

			// E'…' is read the same way under either setting: the default is none\.
			for _, statement := range []string{
				`CREATE SCHEMA app`,
				`CREATE TABLE app.parent (code text PRIMARY KEY)`,
				`CREATE TABLE app.child (id int PRIMARY KEY, parent_code text NOT NULL DEFAULT E'none\\' REFERENCES app.parent (code))`,
			} {
				_, err := db.ExecContext(ctx, statement)
				require.NoError(t, err, statement)
			}

			manager := postgres.NewManager(pg_queries.New(), db, func() {})
			check := func(t *testing.T, rows []*sqlmanager_shared.DatabaseSchemaRow) {
				t.Helper()
				var found bool
				for _, row := range rows {
					if row.TableSchema != "app" {
						continue
					}
					require.Equal(t, c.writtenTwice, row.DefaultBackslashTwice,
						"%s.%s", row.TableName, row.ColumnName)
					if row.TableName == "child" && row.ColumnName == "parent_code" {
						found = true
						require.Equal(t, c.wantDefault, row.ColumnDefault)
					}
				}
				require.True(t, found, "app.child.parent_code is among the rows")
			}

			all, err := manager.GetDatabaseSchema(ctx)
			require.NoError(t, err)
			check(t, all)

			byTable, err := manager.GetDatabaseTableSchemasBySchemasAndTables(ctx, []*sqlmanager_shared.SchemaTable{
				{Schema: "app", Table: "parent"},
				{Schema: "app", Table: "child"},
			})
			require.NoError(t, err)
			check(t, byTable)
		})
	}
}
