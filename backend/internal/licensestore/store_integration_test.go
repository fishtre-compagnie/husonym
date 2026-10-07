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
	"github.com/jackc/pgx/v5/pgtype"
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

// migratedDatabase starts a PostgreSQL holding the schema of the API.
func migratedDatabase(ctx context.Context, t *testing.T) *tcpostgres.PostgresTestContainer {
	t.Helper()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	return container
}

// Test_Offer_WaitsForTheKeyBeingWritten is the one that shows the lock and the isolation level
// at work: an offer made while another key is being written has to wait for it and then see it.
// Without the lock, or with the lock taken after the read, the offer reads the table before the
// other key is committed and accepts the older key; under repeatable read or serializable its
// snapshot predates the commit and it does the same.
func Test_Offer_WaitsForTheKeyBeingWritten(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container := migratedDatabase(ctx, t)

	pub, priv := newPair(t)
	store := storeOn(ctx, t, container.URL, license.Keyring{license.LegacyKid: pub})

	issued := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	older := signedKey(t, priv, issued, issued.Add(time.Hour))
	newer := signedKey(t, priv, issued.Add(time.Minute), issued.Add(time.Hour))

	// Another writer, on a connection of its own, holds the lock and has written the newer key
	// without committing it.
	queries := db_queries.New()
	writer, err := container.DB.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Rollback(context.Background()) })
	require.NoError(t, queries.LockLicenseKeys(ctx, writer))
	_, err = queries.InsertLicenseKey(ctx, writer, db_queries.InsertLicenseKeyParams{
		Key:       newer,
		LicenseID: "lic-newer",
		IssuedAt:  pgtype.Timestamptz{Time: issued.Add(time.Minute), Valid: true},
		Origin:    string(OriginInterface),
	})
	require.NoError(t, err)

	done := make(chan struct{})
	var result *Result
	var offerErr error
	go func() {
		defer close(done)
		result, offerErr = store.Offer(ctx, older, OriginInterface, nil)
	}()

	// The offer is waiting once an advisory lock is asked for and not granted.
	require.Eventually(t, func() bool {
		var waiting int
		err := container.DB.QueryRow(ctx,
			"SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted").Scan(&waiting)
		return err == nil && waiting > 0
	}, 10*time.Second, 20*time.Millisecond, "the offer never waited for the lock")
	select {
	case <-done:
		t.Fatal("the offer returned while the other key was still being written")
	default:
	}

	require.NoError(t, writer.Commit(ctx))
	<-done
	require.NoError(t, offerErr)
	require.Equal(t, RefusedOlder, result.Outcome)
	current, err := store.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, newer, current)
}

func Test_Offer_TheSameKeyAtOnceIsStoredOnce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container := migratedDatabase(ctx, t)

	pub, priv := newPair(t)
	ring := license.Keyring{license.LegacyKid: pub}
	stores := []*Store{storeOn(ctx, t, container.URL, ring), storeOn(ctx, t, container.URL, ring)}

	issued := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	value := signedKey(t, priv, issued, issued.Add(time.Hour))

	results := make([]*Result, len(stores))
	errs := make([]error, len(stores))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, store := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = store.Offer(ctx, value, OriginInterface, nil)
		}()
	}
	close(start)
	wg.Wait()

	// Without the lock, both would insert and one would fail on the unique key.
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.ElementsMatch(t, []Outcome{Accepted, Unchanged}, []Outcome{results[0].Outcome, results[1].Outcome})
}

func Test_Offer_TwoKeysAtOnceLeaveTheNewestInForce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container := migratedDatabase(ctx, t)

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
