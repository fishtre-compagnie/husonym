package migrations_test

import (
	"os"
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// The role that owns the database may bear the name of the schema the first migration creates.
// Its search path then starts with that schema once it exists, and a bookkeeping table looked up
// through the search path would be a second, empty one: the first migration would run again and
// fail. The bookkeeping stays in public whatever the role is called.
func Test_Up_IsRepeatable_WhenTheRoleIsNamedLikeTheSchema(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx,
		tcpostgres.WithUsername("controlplane"), tcpostgres.WithDatabase("controlplane"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(ctx) })
	logger := testutil.GetTestLogger(t)

	require.NoError(t, migrations.Up(ctx, container.URL, logger))
	require.NoError(t, migrations.Up(ctx, container.URL, logger))

	var bookkeeping []string
	rows, err := container.DB.Query(ctx,
		`SELECT schemaname FROM pg_tables WHERE tablename = 'schema_migrations' ORDER BY 1`)
	require.NoError(t, err)
	for rows.Next() {
		var schema string
		require.NoError(t, rows.Scan(&schema))
		bookkeeping = append(bookkeeping, schema)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"public"}, bookkeeping)

	var dirty bool
	require.NoError(t, container.DB.QueryRow(ctx, `SELECT dirty FROM public.schema_migrations`).Scan(&dirty))
	require.False(t, dirty)
}

// A database that already holds the stray bookkeeping table of that defect, dirty, is repaired
// by the next start.
func Test_Up_RepairsADatabaseLeftWithAStrayBookkeepingTable(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx,
		tcpostgres.WithUsername("controlplane"), tcpostgres.WithDatabase("controlplane"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(ctx) })
	logger := testutil.GetTestLogger(t)

	require.NoError(t, migrations.Up(ctx, container.URL, logger))
	// Back to what the first migration alone leaves: that defect predates the later ones.
	undoOperatorJournal(t, container)
	_, err = container.DB.Exec(ctx, `
		UPDATE public.schema_migrations SET version = 1, dirty = false;
		CREATE TABLE controlplane.schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL);
		INSERT INTO controlplane.schema_migrations VALUES (1, true);`)
	require.NoError(t, err)

	require.NoError(t, migrations.Up(ctx, container.URL, logger))

	var stray bool
	require.NoError(t, container.DB.QueryRow(ctx,
		`SELECT to_regclass('controlplane.schema_migrations') IS NOT NULL`).Scan(&stray))
	require.False(t, stray)
}

// operatorJournal tells whether the journal of the operator and the index that gives a license
// one successor at most are both there.
func operatorJournal(t *testing.T, container *tcpostgres.PostgresTestContainer) (table, index bool) {
	t.Helper()
	require.NoError(t, container.DB.QueryRow(t.Context(), `
		SELECT to_regclass('controlplane.operator_actions') IS NOT NULL,
		       to_regclass('controlplane.licenses_succeeds_license_id_idx') IS NOT NULL`).Scan(&table, &index))
	return table, index
}

// undoOperatorJournal applies the down migration of the journal of the operator. The bookkeeping
// is left to the caller.
func undoOperatorJournal(t *testing.T, container *tcpostgres.PostgresTestContainer) {
	t.Helper()
	down, err := os.ReadFile("sql/000003_adds-operator-actions.down.sql")
	require.NoError(t, err)
	_, err = container.DB.Exec(t.Context(), string(down))
	require.NoError(t, err)
}

// The journal of the operator comes with the third migration, under the role the service runs as
// and over two starts in a row; its down migration takes away what it brought and nothing else,
// and the next start brings it back.
func Test_Up_AddsTheOperatorJournal_WhenTheRoleIsNamedLikeTheSchema(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx,
		tcpostgres.WithUsername("controlplane"), tcpostgres.WithDatabase("controlplane"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(ctx) })
	logger := testutil.GetTestLogger(t)

	require.NoError(t, migrations.Up(ctx, container.URL, logger))
	require.NoError(t, migrations.Up(ctx, container.URL, logger))

	table, index := operatorJournal(t, container)
	require.True(t, table)
	require.True(t, index)
	var version int
	var dirty bool
	require.NoError(t, container.DB.QueryRow(ctx,
		`SELECT version, dirty FROM public.schema_migrations`).Scan(&version, &dirty))
	require.GreaterOrEqual(t, version, 3, "the journal comes with the third migration; later ones may follow")
	require.False(t, dirty)

	// A license with two successors is what the index refuses.
	_, err = container.DB.Exec(ctx, `
		INSERT INTO controlplane.customers (id, external_id, name)
		VALUES ('00000000-0000-0000-0000-000000000001', 'cust-1', 'Acme');
		INSERT INTO controlplane.licenses (
		    id, customer_id, encoded, key_fingerprint, kid, plan, telemetry, issued_at, expires_at,
		    signing_key_fingerprint, origin, succeeds_license_id)
		SELECT id, '00000000-0000-0000-0000-000000000001', id, id, 'v1', '', '', now(), now(), '', 'console', succeeds
		FROM (VALUES ('lic-1', NULL), ('lic-2', NULL), ('lic-3', 'lic-1')) AS l (id, succeeds);`)
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx,
		`UPDATE controlplane.licenses SET succeeds_license_id = 'lic-1' WHERE id = 'lic-2'`)
	require.ErrorContains(t, err, "licenses_succeeds_license_id_idx")

	undoOperatorJournal(t, container)
	_, err = container.DB.Exec(ctx, `UPDATE public.schema_migrations SET version = 2`)
	require.NoError(t, err)

	table, index = operatorJournal(t, container)
	require.False(t, table)
	require.False(t, index)
	var licenses int
	require.NoError(t, container.DB.QueryRow(ctx, `SELECT count(*) FROM controlplane.licenses`).Scan(&licenses))
	require.Equal(t, 3, licenses, "the down migration leaves the licenses")

	require.NoError(t, migrations.Up(ctx, container.URL, logger))
	table, index = operatorJournal(t, container)
	require.True(t, table)
	require.True(t, index)
}

func Test_Up_CreatesTheTables_AndIsRepeatable(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(ctx) })
	logger := testutil.GetTestLogger(t)

	require.NoError(t, migrations.Up(ctx, container.URL, logger))
	require.NoError(t, migrations.Up(ctx, container.URL, logger))

	for _, table := range []string{
		"customers", "licenses", "instances", "usage_reports", "pending_reports", "seal_rejections",
		"operator_actions",
	} {
		var exists bool
		require.NoError(t, container.DB.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'controlplane' AND table_name = $1)`, table).Scan(&exists))
		require.True(t, exists, "table %s", table)
	}
}
