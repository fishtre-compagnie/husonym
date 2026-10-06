package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	schemamanager "github.com/fishtre-compagnie/husonym/internal/schema-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// A session inherits standard_conforming_strings from its database. When it is off, a
// backslash inside '…' is an escape character. A constraint, an index, an enum label and a
// column comment that hold a backslash are created on the destination under either setting:
// the first three by the initialization, which finds them there when it runs again, and the
// comment by the reconciliation.
func Test_PostgresManager_InitializesUnderEitherStringSetting(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	ctx := context.Background()

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	source, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	require.NoError(t, err)
	defer source.Close()

	exec := func(t *testing.T, db *sql.DB, statements ...string) {
		t.Helper()
		for _, statement := range statements {
			_, err := db.ExecContext(ctx, statement)
			require.NoError(t, err, statement)
		}
	}
	// The source session reads a backslash as an ordinary character.
	var sourceSetting string
	require.NoError(t, source.QueryRowContext(ctx, "SHOW standard_conforming_strings").Scan(&sourceSetting))
	require.Equal(t, "on", sourceSetting)
	exec(t, source,
		`CREATE SCHEMA app`,
		`CREATE TYPE app.kind AS ENUM ('plain', 'back\slash', 'o''clock')`,
		`CREATE TABLE app.t (id int PRIMARY KEY, k app.kind, v int, CONSTRAINT "ck\" CHECK (v > 0))`,
		`CREATE INDEX "ix\" ON app.t (v)`,
		`COMMENT ON COLUMN app.t.v IS 'it''s\'`,
	)

	// catalog lists the constraints, the indexes, the enum labels and the column comments of
	// the schema, one per line.
	catalog := func(t *testing.T, db *sql.DB) []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `
			SELECT line FROM (
				SELECT 'constraint ' || c.conname || ': ' || pg_get_constraintdef(c.oid) AS line
				FROM pg_constraint c JOIN pg_namespace n ON n.oid = c.connamespace WHERE n.nspname = 'app'
				UNION ALL
				SELECT 'index ' || c.relname || ': ' || pg_get_indexdef(c.oid)
				FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'app' AND c.relkind = 'i'
				UNION ALL
				SELECT 'label ' || t.typname || ' ' || e.enumsortorder::text || ': ' || e.enumlabel
				FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid JOIN pg_namespace n ON n.oid = t.typnamespace
				WHERE n.nspname = 'app'
				UNION ALL
				SELECT 'comment ' || c.relname || '.' || a.attname || ': ' || d.description
				FROM pg_description d
				JOIN pg_class c ON c.oid = d.objoid
				JOIN pg_namespace n ON n.oid = c.relnamespace
				JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.objsubid
				WHERE n.nspname = 'app'
			) lines ORDER BY line`)
		require.NoError(t, err)
		defer rows.Close()
		list := []string{}
		for rows.Next() {
			var line string
			require.NoError(t, rows.Scan(&line))
			list = append(list, line)
		}
		require.NoError(t, rows.Err())
		return list
	}
	want := catalog(t, source)
	require.Equal(t, []string{
		`comment t.v: it's\`,
		`constraint ck\: CHECK ((v > 0))`,
		`constraint t_pkey: PRIMARY KEY (id)`,
		`index ix\: CREATE INDEX "ix\" ON app.t USING btree (v)`,
		`index t_pkey: CREATE UNIQUE INDEX t_pkey ON app.t USING btree (id)`,
		`label kind 1: plain`,
		`label kind 2: back\slash`,
		`label kind 3: o'clock`,
	}, want)
	wantWithoutComment := want[1:]

	connection := func(address string) *mgmtv1alpha1.Connection {
		return &mgmtv1alpha1.Connection{
			Id: uuid.NewString(),
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{
				PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.PostgresConnectionConfig_Url{Url: address},
				},
			}},
		}
	}

	for _, setting := range []string{"off", "on"} {
		t.Run("standard_conforming_strings "+setting, func(t *testing.T) {
			database := "strings_" + setting
			exec(t, source,
				"CREATE DATABASE "+database,
				"ALTER DATABASE "+database+" SET standard_conforming_strings = "+setting,
			)
			destinationURL, err := url.Parse(container.URL)
			require.NoError(t, err)
			destinationURL.Path = "/" + database
			destination, err := sql.Open(sqlmanager_shared.PostgresDriver, destinationURL.String())
			require.NoError(t, err)
			defer destination.Close()

			// A session of the destination gets the setting of its database.
			var shown string
			require.NoError(t, destination.QueryRowContext(ctx, "SHOW standard_conforming_strings").Scan(&shown))
			require.Equal(t, setting, shown)

			withSchemaManager := func(t *testing.T, fn func(manager schemamanager.SchemaManagerService)) {
				t.Helper()
				manager, err := schemamanager.NewSchemaManager(
					tchusonymapi.NewTestSqlManagerClient(),
					connectionmanager.NewUniqueSession(),
					testutil.GetTestLogger(t),
					testutil.NewFakeEELicense(testutil.WithIsValid()),
				).New(ctx, connection(container.URL), connection(destinationURL.String()), &mgmtv1alpha1.JobDestination{
					Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
						PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{InitTableSchema: true},
					}},
				})
				require.NoError(t, err)
				defer manager.CloseConnections()
				fn(manager)
			}

			// The initialization creates the constraint, the index and the enum type; the second
			// one finds each of them by its name.
			for _, pass := range []string{"first", "second"} {
				withSchemaManager(t, func(manager schemamanager.SchemaManagerService) {
					failed, err := manager.InitializeSchema(ctx, map[string]struct{}{"app.t": {}})
					require.NoError(t, err, "%s initialization", pass)
					for _, failure := range failed {
						require.Fail(t, "a statement of the "+pass+" initialization failed", "%s: %s", failure.Statement, failure.Error)
					}
				})
				require.Equal(t, wantWithoutComment, catalog(t, destination), "the destination after the %s initialization", pass)
			}

			// The comment of a column is written by the reconciliation.
			withSchemaManager(t, func(manager schemamanager.SchemaManagerService) {
				tables := map[string]*sqlmanager_shared.SchemaTable{"app.t": {Schema: "app", Table: "t"}}
				diff, err := manager.CalculateSchemaDiff(ctx, tables)
				require.NoError(t, err)
				statements, err := manager.BuildSchemaDiffStatements(ctx, diff)
				require.NoError(t, err)
				failed, err := manager.ReconcileDestinationSchema(ctx, tables, statements)
				require.NoError(t, err)
				for _, failure := range failed {
					require.Fail(t, "a statement of the reconciliation failed", "%s: %s", failure.Statement, failure.Error)
				}
			})
			require.Equal(t, want, catalog(t, destination), "the destination after the reconciliation")
		})
	}
}
