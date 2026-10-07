package migrations_test

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

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
