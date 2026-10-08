// Package cptest holds the test helpers of the control plane.
package cptest

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/migrations"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// NewDatabase starts a PostgreSQL container, applies the migrations of the control plane and
// returns a pool on it. The pool and the container are released when the test ends.
func NewDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newDatabase(t, true)
}

// NewEmptyDatabase starts a PostgreSQL container and returns a pool on it, as NewDatabase does,
// but applies no migration: the database holds nothing of the control plane.
func NewEmptyDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newDatabase(t, false)
}

func newDatabase(t *testing.T, migrated bool) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(ctx) })
	if migrated {
		require.NoError(t, migrations.Up(ctx, container.URL, testutil.GetTestLogger(t)))
	}
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}
