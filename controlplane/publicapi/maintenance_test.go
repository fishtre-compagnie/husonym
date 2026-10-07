package publicapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/stretchr/testify/require"
)

// maintenanceNow is what the maintenance of the tests reads the time from.
var maintenanceNow = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return maintenanceNow }

type fakePendingStore struct {
	lists       atomic.Int32
	purges      atomic.Int32
	err         error
	purgePanics bool

	mu        sync.Mutex
	olderThan []time.Time
}

func (f *fakePendingStore) PendingFingerprintsNowKnown(context.Context) ([]string, error) {
	f.lists.Add(1)
	return []string{"one"}, f.err
}

func (f *fakePendingStore) PurgePending(_ context.Context, olderThan time.Time) (int64, error) {
	f.mu.Lock()
	f.olderThan = append(f.olderThan, olderThan)
	f.mu.Unlock()
	f.purges.Add(1)
	if f.purgePanics {
		panic("SECRET-PURGE-VALUE")
	}
	return 0, nil
}

type panickingPromoter struct{}

func (panickingPromoter) PromotePending(context.Context, string) (int, int, error) {
	panic("SECRET-PANIC-VALUE")
}

type failingPromoter struct{}

func (failingPromoter) PromotePending(context.Context, string) (int, int, error) {
	return 0, 0, errors.New("promotion is down")
}

type countingPromoter struct{ calls atomic.Int32 }

func (c *countingPromoter) PromotePending(context.Context, string) (int, int, error) {
	c.calls.Add(1)
	return 1, 0, nil
}

// runUntil runs the maintenance until done says so, then stops it and gives what it logged.
func runUntil(t *testing.T, promoter publicapi.Promoter, store *fakePendingStore, done func() bool) string {
	t.Helper()
	logs := &bytes.Buffer{}
	returned := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer close(returned)
		publicapi.RunMaintenance(ctx, promoter, store, slog.New(slog.NewTextHandler(logs, nil)), fixedNow, 5*time.Millisecond)
	}()
	require.Eventually(t, done, 5*time.Second, time.Millisecond)
	cancel()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("the maintenance did not return when its context ended")
	}
	return logs.String()
}

// runUntilPasses runs the maintenance until the store was listed at least n times, then stops it.
func runUntilPasses(t *testing.T, promoter publicapi.Promoter, store *fakePendingStore, n int32) string {
	t.Helper()
	return runUntil(t, promoter, store, func() bool { return store.lists.Load() >= n })
}

// blockingStore waits for the context to end, and fails with its error.
type blockingStore struct{ started chan struct{} }

func (b *blockingStore) PendingFingerprintsNowKnown(ctx context.Context) ([]string, error) {
	close(b.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*blockingStore) PurgePending(ctx context.Context, _ time.Time) (int64, error) {
	return 0, ctx.Err()
}

func Test_RunMaintenance_APassCutByCancellation_LogsNoError(t *testing.T) {
	logs := &bytes.Buffer{}
	store := &blockingStore{started: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		publicapi.RunMaintenance(ctx, panickingPromoter{}, store, slog.New(slog.NewTextHandler(logs, nil)), fixedNow, time.Hour)
	}()
	<-store.started
	cancel()
	<-done

	require.NotContains(t, logs.String(), "level=ERROR")
	require.NotContains(t, logs.String(), "maintained")
}

func Test_RunMaintenance_AFailedStep_LogsNoSummary(t *testing.T) {
	store := &fakePendingStore{err: errors.New("database is down")}

	logged := runUntilPasses(t, failingPromoter{}, store, 2)

	require.NotContains(t, logged, "maintained")
}

func Test_RunMaintenance_APanicEndsThePassNotTheLoop(t *testing.T) {
	logged := runUntilPasses(t, panickingPromoter{}, &fakePendingStore{}, 3)

	require.Contains(t, logged, "maintenance of the pending reports failed")
	require.NotContains(t, logged, "SECRET-PANIC-VALUE")
	require.NotContains(t, logged, "maintained")
}

// The promotion and the purge each stand alone: what one panics with does not skip the other.
func Test_RunMaintenance_APanicInThePromotion_DoesNotSkipThePurge(t *testing.T) {
	store := &fakePendingStore{}

	logged := runUntil(t, panickingPromoter{}, store, func() bool { return store.purges.Load() >= 2 })

	require.Contains(t, logged, "maintenance of the pending reports failed")
	require.Contains(t, logged, "step=promotion")
	require.NotContains(t, logged, "SECRET-PANIC-VALUE")
	require.NotContains(t, logged, "maintained")
}

func Test_RunMaintenance_APanicInThePurge_LeavesThePromotionAndTheLoop(t *testing.T) {
	store := &fakePendingStore{purgePanics: true}
	promoter := &countingPromoter{}

	logged := runUntil(t, promoter, store, func() bool { return promoter.calls.Load() >= 3 })

	require.Contains(t, logged, "maintenance of the pending reports failed")
	require.Contains(t, logged, "step=purge")
	require.NotContains(t, logged, "SECRET-PURGE-VALUE")
	require.NotContains(t, logged, "maintained")
}

// The purge removes what is older than the time reports are kept, counted from the clock given.
func Test_RunMaintenance_ThePurgeReadsTheClockItIsGiven(t *testing.T) {
	store := &fakePendingStore{}

	logged := runUntil(t, &countingPromoter{}, store, func() bool { return store.purges.Load() >= 1 })

	store.mu.Lock()
	defer store.mu.Unlock()
	require.True(t, maintenanceNow.Add(-intake.PendingKept).Equal(store.olderThan[0]),
		"purged what is older than %s", store.olderThan[0])
	require.Contains(t, logged, "pending reports maintained")
}

func Test_RunMaintenance_AFailureIsLoggedAndTheLoopGoesOn(t *testing.T) {
	store := &fakePendingStore{err: errors.New("database is down")}

	logged := runUntilPasses(t, failingPromoter{}, store, 3)

	require.Contains(t, logged, "database is down")
	require.Contains(t, logged, "promotion is down")
}
