package usagereport

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/stretchr/testify/require"
)

type fakeBuilder struct {
	calls  int
	days   []time.Time
	sealed *Sealed
	err    error
	// block makes Build wait for its context to end, then return what ended it.
	block bool
	// ignoreContext makes a blocking Build return a sealed report whatever ended its context.
	ignoreContext bool
}

func (f *fakeBuilder) Build(ctx context.Context, day, _ time.Time) (*Sealed, error) {
	f.calls++
	f.days = append(f.days, day)
	if f.block {
		<-ctx.Done()
		if f.ignoreContext {
			return f.sealed, nil
		}
		return nil, ctx.Err()
	}
	return f.sealed, f.err
}

type fakeStore struct {
	reports map[string]usagestore.StoredReport
	// lose makes SaveReport find the day taken by another replica.
	lose          bool
	deletedBefore []time.Time
}

func newFakeStore() *fakeStore { return &fakeStore{reports: map[string]usagestore.StoredReport{}} }

func (f *fakeStore) Report(_ context.Context, day time.Time) (*usagestore.StoredReport, error) {
	if r, ok := f.reports[day.Format(time.DateOnly)]; ok {
		return &r, nil
	}
	return nil, nil //nolint:nilnil // none stored
}

func (f *fakeStore) SaveReport(_ context.Context, r usagestore.StoredReport) (bool, error) {
	if f.lose {
		return false, nil
	}
	f.reports[r.Day.Format(time.DateOnly)] = r
	return true, nil
}

func (f *fakeStore) DeleteReportsBefore(_ context.Context, day time.Time) error {
	f.deletedBefore = append(f.deletedBefore, day)
	return nil
}

var now = time.Date(2026, 10, 7, 0, 5, 12, 0, time.UTC)

func preparerOf(b *fakeBuilder, s *fakeStore) *Preparer {
	return NewPreparer(b, s, slog.New(slog.DiscardHandler))
}

func Test_PrepareDue_PreparesYesterdayWhenNoneIsStored(t *testing.T) {
	builder := &fakeBuilder{sealed: &Sealed{Document: []byte(`{}`), Seal: "seal", KeyFingerprint: "fp"}}
	store := newFakeStore()

	require.NoError(t, preparerOf(builder, store).PrepareDue(t.Context(), now))

	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	require.Equal(t, []time.Time{day}, builder.days)
	got := store.reports["2026-10-06"]
	require.Equal(t, []byte(`{}`), got.Document)
	require.Equal(t, "seal", got.Seal)
	require.Equal(t, "fp", got.KeyFingerprint)
	require.Equal(t, now, got.PreparedAt)
}

func Test_PrepareDue_AReportAlreadyStoredIsLeftAlone(t *testing.T) {
	builder := &fakeBuilder{sealed: &Sealed{}}
	store := newFakeStore()
	store.reports["2026-10-06"] = usagestore.StoredReport{Day: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}

	require.NoError(t, preparerOf(builder, store).PrepareDue(t.Context(), now))
	require.Zero(t, builder.calls)
	require.Empty(t, store.deletedBefore)
}

func Test_PrepareDue_NoLicenseIsNotAnError(t *testing.T) {
	builder := &fakeBuilder{err: ErrNoLicenseInForce}
	store := newFakeStore()

	require.NoError(t, preparerOf(builder, store).PrepareDue(t.Context(), now))
	require.Empty(t, store.reports)
}

func Test_PrepareDue_ABuilderFailureIsReturnedAndNothingIsStored(t *testing.T) {
	builder := &fakeBuilder{err: errors.New("boom")}
	store := newFakeStore()

	require.ErrorContains(t, preparerOf(builder, store).PrepareDue(t.Context(), now), "boom")
	require.Empty(t, store.reports)
	require.Empty(t, store.deletedBefore)
}

func Test_PrepareDue_DropsTheReportsOlderThan24MonthsFromTheDayPrepared(t *testing.T) {
	builder := &fakeBuilder{sealed: &Sealed{}}
	store := newFakeStore()

	require.NoError(t, preparerOf(builder, store).PrepareDue(t.Context(), now))
	require.Equal(t, []time.Time{time.Date(2024, 10, 6, 0, 0, 0, 0, time.UTC)}, store.deletedBefore)
}

func Test_PrepareDue_ADayTakenByAnotherReplicaIsNotAnError(t *testing.T) {
	builder := &fakeBuilder{sealed: &Sealed{}}
	store := newFakeStore()
	store.lose = true

	require.NoError(t, preparerOf(builder, store).PrepareDue(t.Context(), now))
	require.Empty(t, store.deletedBefore)
}

func Test_PrepareDue_ABuildThatOutlastsItsBoundIsAnErrorAndStoresNothing(t *testing.T) {
	builder := &fakeBuilder{block: true}
	store := newFakeStore()
	preparer := preparerOf(builder, store)
	preparer.buildTimeout = 20 * time.Millisecond

	err := preparer.PrepareDue(t.Context(), now)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Empty(t, store.reports)
}

func Test_PrepareDue_AReportReturnedAfterItsContextEndedIsNotStored(t *testing.T) {
	builder := &fakeBuilder{block: true, ignoreContext: true, sealed: &Sealed{Document: []byte(`{}`)}}
	store := newFakeStore()
	preparer := preparerOf(builder, store)
	preparer.buildTimeout = 20 * time.Millisecond

	require.ErrorIs(t, preparer.PrepareDue(t.Context(), now), context.DeadlineExceeded)
	require.Empty(t, store.reports)
}

func Test_Every_StopsWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		preparerOf(&fakeBuilder{}, newFakeStore()).Every(ctx, time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Every did not stop")
	}
}
