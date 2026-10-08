package migrations_test

import (
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
	} {
		var exists bool
		require.NoError(t, container.DB.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_schema = 'controlplane' AND table_name = $1)`, table).Scan(&exists))
		require.True(t, exists, "table %s", table)
	}
}
