package publicapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/stretchr/testify/require"
)

type fakePendingStore struct {
	lists atomic.Int32
	err   error
}

func (f *fakePendingStore) PendingFingerprintsNowKnown(context.Context) ([]string, error) {
	f.lists.Add(1)
	return []string{"one"}, f.err
}

func (f *fakePendingStore) PurgePending(context.Context, time.Time) (int64, error) { return 0, nil }

type panickingPromoter struct{}

func (panickingPromoter) PromotePending(context.Context, string) (int, int, error) {
	panic("SECRET-PANIC-VALUE")
}

type failingPromoter struct{}

func (failingPromoter) PromotePending(context.Context, string) (int, int, error) {
	return 0, 0, errors.New("promotion is down")
}

// runUntilPasses runs the maintenance until the store was listed at least n times, then stops it.
func runUntilPasses(t *testing.T, promoter publicapi.Promoter, store *fakePendingStore, n int32) string {
	t.Helper()
	logs := &bytes.Buffer{}
	var mu = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		defer close(mu)
		publicapi.RunMaintenance(ctx, promoter, store, slog.New(slog.NewTextHandler(logs, nil)), 5*time.Millisecond)
	}()
	require.Eventually(t, func() bool { return store.lists.Load() >= n }, 5*time.Second, time.Millisecond)
	cancel()
	select {
	case <-mu:
	case <-time.After(5 * time.Second):
		t.Fatal("the maintenance did not return when its context ended")
	}
	return logs.String()
}

func Test_RunMaintenance_APanicEndsThePassNotTheLoop(t *testing.T) {
	logged := runUntilPasses(t, panickingPromoter{}, &fakePendingStore{}, 3)

	require.Contains(t, logged, "maintenance of the pending reports failed")
	require.NotContains(t, logged, "SECRET-PANIC-VALUE")
}

func Test_RunMaintenance_AFailureIsLoggedAndTheLoopGoesOn(t *testing.T) {
	store := &fakePendingStore{err: errors.New("database is down")}

	logged := runUntilPasses(t, failingPromoter{}, store, 3)

	require.Contains(t, logged, "database is down")
	require.Contains(t, logged, "promotion is down")
}
