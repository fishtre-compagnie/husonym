package usagereport

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// sealedBuilder always makes the same report, and is safe for concurrent use.
type sealedBuilder struct{ sealed *Sealed }

func (b sealedBuilder) Build(context.Context, time.Time, time.Time) (*Sealed, error) {
	return b.sealed, nil
}

// Two replicas that prepare the same day at once leave one report, and neither fails.
func Test_PrepareDue_TwoReplicasAtOnceLeaveOneReport(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(t.Context()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := usagestore.New(husonymdb.New(pool, db_queries.New()))

	builder := sealedBuilder{&Sealed{Document: []byte(`{"a":1}`), Seal: "seal", KeyFingerprint: "fp"}}
	logger := slog.New(slog.DiscardHandler)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = NewPreparer(builder, store, logger).PrepareDue(ctx, now)
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	var count int
	require.NoError(t, container.DB.QueryRow(ctx, `SELECT count(*) FROM husonym_api.usage_reports`).Scan(&count))
	require.Equal(t, 1, count)
}
