package sqlmanager_postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
)

// The manager reads the catalog of the whole database. A migration in another schema, which
// drops a type while the catalog is read, fails a bare query with "cache lookup failed": the
// manager reads again, and a run that starts meanwhile does not fail for it.
func Test_PostgresManager_ReadsTheCatalogWhileItChanges(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })

	reader, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	require.NoError(t, err)
	defer reader.Close()
	migrator, err := sql.Open(sqlmanager_shared.PostgresDriver, container.URL)
	require.NoError(t, err)
	defer migrator.Close()

	// Another session migrates tables of its own, without end: it adds a column of a new type,
	// with a default, then drops the column and the type. The tables stay.
	_, err = migrator.ExecContext(ctx, `
		CREATE SCHEMA migrated;
		CREATE TABLE migrated.t0 (id int PRIMARY KEY);
		CREATE TABLE migrated.t1 (id int PRIMARY KEY);
		CREATE TABLE migrated.t2 (id int PRIMARY KEY);
		CREATE TABLE migrated.t3 (id int PRIMARY KEY);`)
	require.NoError(t, err)
	migrated := make(chan error, 1)
	var migrations atomic.Int64
	go func() {
		for i := 0; ctx.Err() == nil; i++ {
			table, status := fmt.Sprintf("migrated.t%d", i%4), fmt.Sprintf("migrated.status%d", i%4)
			_, err := migrator.ExecContext(ctx, fmt.Sprintf(`
				CREATE TYPE %[2]s AS ENUM ('on', 'off');
				ALTER TABLE %[1]s ADD COLUMN s %[2]s DEFAULT 'on';`, table, status))
			if err == nil {
				_, err = migrator.ExecContext(ctx, fmt.Sprintf(`
					ALTER TABLE %[1]s DROP COLUMN s;
					DROP TYPE %[2]s;`, table, status))
			}
			if err != nil && ctx.Err() == nil {
				migrated <- err
				return
			}
			migrations.Add(1)
			// A migration is a moment, not a storm: the catalog holds still between two.
			select {
			case <-ctx.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
		migrated <- nil
	}()

	manager := postgres.NewManager(pg_queries.New(), reader, func() {})
	bare := pg_queries.New()
	// Read until the bare query has failed a few times: the catalog did change under reads.
	const wanted = 3
	bareFailures, reads := 0, 0
	deadline := time.Now().Add(60 * time.Second)
	for bareFailures < wanted {
		require.True(t, time.Now().Before(deadline),
			"the bare query failed %d times in %d reads: the catalog no longer changes under it", bareFailures, reads)
		_, err := manager.GetDatabaseSchema(ctx)
		require.NoError(t, err, "the manager failed on a catalog that changed under its read")
		reads++
		// The same query, read once: it is the one that fails, now and then.
		if _, err := bare.GetDatabaseSchema(ctx, reader); err != nil {
			require.ErrorContains(t, err, "cache lookup failed")
			bareFailures++
		}
	}
	cancel()
	require.NoError(t, <-migrated)
	t.Logf("%d reads through the manager during %d migrations; the bare query failed %d times",
		reads, migrations.Load(), bareFailures)
}
