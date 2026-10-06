package licensestore

import (
	"context"
	"sync"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// storeOn gives a store on its own connection pool to the database, as another instance of the
// API would have.
func storeOn(ctx context.Context, t *testing.T, url string, ring license.Keyring) *Store {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return New(husonymdb.New(pool, db_queries.New()), ring)
}

func Test_Offer_TwoKeysAtOnceLeaveTheNewestInForce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()

	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))

	pub, priv := newPair(t)
	ring := license.Keyring{license.LegacyKid: pub}
	first := storeOn(ctx, t, container.URL, ring)
	second := storeOn(ctx, t, container.URL, ring)

	current, err := first.Current(ctx)
	require.NoError(t, err)
	require.Empty(t, current, "a new database holds no key")

	base := time.Now().UTC().Truncate(time.Second).Add(-24 * time.Hour)
	// Each round offers two keys at once, both after the ones of the rounds before, so that
	// every round starts from a key in force and ends on the newest of its two.
	for round := range 20 {
		issued := base.Add(time.Duration(round) * time.Hour)
		older := signedKey(t, priv, issued, issued.Add(time.Hour))
		newer := signedKey(t, priv, issued.Add(time.Minute), issued.Add(time.Hour))

		var wg sync.WaitGroup
		var olderResult, newerResult *Result
		var olderErr, newerErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			olderResult, olderErr = first.Offer(ctx, older, OriginInterface, nil)
		}()
		go func() {
			defer wg.Done()
			<-start
			newerResult, newerErr = second.Offer(ctx, newer, OriginInterface, nil)
		}()
		close(start)
		wg.Wait()

		require.NoError(t, olderErr)
		require.NoError(t, newerErr)
		// The newest is always taken; the older one is taken only if it came first.
		require.Equal(t, Accepted, newerResult.Outcome, "round %d", round)
		require.Contains(t, []Outcome{Accepted, RefusedOlder}, olderResult.Outcome, "round %d", round)

		for name, store := range map[string]*Store{"first": first, "second": second} {
			got, err := store.Current(ctx)
			require.NoError(t, err)
			require.Equal(t, newer, got, "round %d: the key in force seen from the %s store", round, name)
		}
	}
}
