package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	schemamanager "github.com/fishtre-compagnie/husonym/internal/schema-manager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The labels of an enum type have an order, which a label added before or after another one
// changes without changing the order the catalog stores them in. The destination gets the
// labels in the order of the source, and a later reconciliation finds nothing to rename.
func Test_PostgresManager_EnumLabelsKeepTheirOrder(t *testing.T) {
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

	const destinationDatabase = "enum_destination"
	destinationURL, err := url.Parse(container.URL)
	require.NoError(t, err)
	destinationURL.Path = "/" + destinationDatabase

	exec := func(t *testing.T, db *sql.DB, statements ...string) {
		t.Helper()
		for _, statement := range statements {
			_, err := db.ExecContext(ctx, statement)
			require.NoError(t, err, statement)
		}
	}
	exec(t, source,
		"CREATE DATABASE "+destinationDatabase,
		"CREATE SCHEMA app",
		"CREATE TYPE app.mood AS ENUM ('sad', 'happy', 'glad')",
		"ALTER TYPE app.mood ADD VALUE 'first' BEFORE 'happy'",
		"ALTER TYPE app.mood ADD VALUE 'second' AFTER 'happy'",
		"CREATE TYPE app.plain AS ENUM ('one', 'two', 'three')",
		"CREATE TABLE app.t (id int PRIMARY KEY, m app.mood, p app.plain)",
	)
	destination, err := sql.Open(sqlmanager_shared.PostgresDriver, destinationURL.String())
	require.NoError(t, err)
	defer destination.Close()

	labels := func(t *testing.T, db *sql.DB) []string {
		t.Helper()
		rows, err := db.QueryContext(ctx, `
			SELECT t.typname || ': ' || e.enumlabel
			FROM pg_enum e JOIN pg_type t ON t.oid = e.enumtypid JOIN pg_namespace n ON n.oid = t.typnamespace
			WHERE n.nspname = 'app' ORDER BY t.typname, e.enumsortorder`)
		require.NoError(t, err)
		defer rows.Close()
		list := []string{}
		for rows.Next() {
			var label string
			require.NoError(t, rows.Scan(&label))
			list = append(list, label)
		}
		require.NoError(t, rows.Err())
		return list
	}
	require.Equal(t, []string{
		"mood: sad", "mood: first", "mood: happy", "mood: second", "mood: glad",
		"plain: one", "plain: two", "plain: three",
	}, labels(t, source))

	// The statement that creates a type whose labels were given in their order at once reads
	// as it always did.
	datatypes, err := postgres.NewManager(pg_queries.New(), source, func() {}).GetSchemaTableDataTypes(
		ctx, []*sqlmanager_shared.SchemaTable{{Schema: "app", Table: "t"}})
	require.NoError(t, err)
	definitions := map[string]string{}
	for _, enum := range datatypes.Enums {
		definitions[enum.Name] = enum.Definition
	}
	require.Contains(t, definitions["plain"], "CREATE TYPE app.plain AS ENUM ('one', 'two', 'three');")
	assert.Contains(t, definitions["mood"], "CREATE TYPE app.mood AS ENUM ('sad', 'first', 'happy', 'second', 'glad');")

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

	withSchemaManager(t, func(manager schemamanager.SchemaManagerService) {
		failed, err := manager.InitializeSchema(ctx, map[string]struct{}{"app.t": {}})
		require.NoError(t, err)
		require.Empty(t, failed)
	})
	assert.Equal(t, labels(t, source), labels(t, destination), "the labels of the destination, once created")

	// A label the source gets later is added by the reconciliation, which renames nothing.
	exec(t, source, "ALTER TYPE app.mood ADD VALUE 'last'")
	withSchemaManager(t, func(manager schemamanager.SchemaManagerService) {
		tables := map[string]*sqlmanager_shared.SchemaTable{"app.t": {Schema: "app", Table: "t"}}
		diff, err := manager.CalculateSchemaDiff(ctx, tables)
		require.NoError(t, err)
		added, renamed := map[string][]string{}, map[string]map[string]string{}
		for _, enum := range diff.ExistsInBoth.Different.Enums {
			if len(enum.NewValues) > 0 {
				added[enum.Enum.Name] = enum.NewValues
			}
			if len(enum.ChangedValues) > 0 {
				renamed[enum.Enum.Name] = enum.ChangedValues
			}
		}
		assert.Equal(t, map[string][]string{"mood": {"last"}}, added)
		assert.Empty(t, renamed)
		statements, err := manager.BuildSchemaDiffStatements(ctx, diff)
		require.NoError(t, err)
		failed, err := manager.ReconcileDestinationSchema(ctx, tables, statements)
		require.NoError(t, err)
		for _, failure := range failed {
			assert.Fail(t, "a statement of the reconciliation failed", "%s: %s", failure.Statement, failure.Error)
		}
	})
	require.Equal(t, []string{
		"mood: sad", "mood: first", "mood: happy", "mood: second", "mood: glad", "mood: last",
		"plain: one", "plain: two", "plain: three",
	}, labels(t, source))
	require.Equal(t, labels(t, source), labels(t, destination), "the labels of the destination, once reconciled")
}
