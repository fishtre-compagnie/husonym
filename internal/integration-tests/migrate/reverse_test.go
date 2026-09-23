package migrate_test

import (
	"context"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// Down migrations are not exercised anywhere else, and this one swaps a unique
// constraint: it has to collapse two identities that differ only by their issuer, because
// a subject-only key cannot hold both.
func Test_Migrations_Reverse(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := context.Background()
	logger := testutil.GetTestLogger(t)

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(ctx) })

	const dir = "../../../backend/sql/postgresql/schema"
	require.NoError(t, neomigrate.Up(ctx, container.URL, dir, logger))

	db := husonymdb.New(container.DB, db_queries.New())

	// Two users whose subjects collide across issuers -- the state this migration exists
	// to make possible, and the one its reverse has to resolve.
	first, err := db.SetUserByIdentity(ctx, husonymdb.Identity{
		Issuer: "https://first.example.com/", Subject: "collides",
	}, nil)
	require.NoError(t, err)
	second, err := db.SetUserByIdentity(ctx, husonymdb.Identity{
		Issuer: "https://second.example.com/", Subject: "collides",
	}, nil)
	require.NoError(t, err)
	require.NotEqual(t, husonymdb.UUIDString(first.ID), husonymdb.UUIDString(second.ID))

	require.NoError(t, neomigrate.Down(ctx, container.URL, dir, logger),
		"the reverse must survive two identities sharing a subject")

	// And it is replayable: a deployment that rolls back and forward again must not be
	// left with a schema it cannot migrate.
	require.NoError(t, neomigrate.Up(ctx, container.URL, dir, logger))
}
