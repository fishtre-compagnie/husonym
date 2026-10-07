package usagereport

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
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

// The day is closed at midnight UTC, and the pass waits five minutes more: the clock of the
// process and the one of the database may differ by seconds.
func Test_PrepareDue_WaitsForTheDayToBeClosed(t *testing.T) {
	midnight := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	east := time.FixedZone("east", 9*3600)
	west := time.FixedZone("west", -5*3600)
	yesterday := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		now  time.Time
		want []time.Time
	}{
		"just before midnight, the day before yesterday is long closed": {
			now: midnight.Add(-time.Second), want: []time.Time{yesterday.AddDate(0, 0, -1)},
		},
		"at midnight":                   {now: midnight},
		"four minutes 59 past midnight": {now: midnight.Add(5*time.Minute - time.Second)},
		"five minutes past midnight":    {now: midnight.Add(5 * time.Minute), want: []time.Time{yesterday}},
		"noon":                          {now: midnight.Add(12 * time.Hour), want: []time.Time{yesterday}},
		// 09:02 on the 7th at UTC+9 is 00:02 UTC on the 7th: too early.
		"too early, told in a zone ahead of UTC": {now: time.Date(2026, 10, 7, 9, 2, 0, 0, east)},
		// 09:06 on the 7th at UTC+9 is 00:06 UTC on the 7th.
		"due, told in a zone ahead of UTC": {
			now: time.Date(2026, 10, 7, 9, 6, 0, 0, east), want: []time.Time{yesterday},
		},
		// 19:06 on the 6th at UTC-5 is 00:06 UTC on the 7th: yesterday is the 6th, not the 5th.
		"due, told in a zone behind UTC": {
			now: time.Date(2026, 10, 6, 19, 6, 0, 0, west), want: []time.Time{yesterday},
		},
		// 18:30 on the 6th at UTC-5 is 23:30 UTC on the 6th: yesterday is the 5th.
		"the evening before, told in a zone behind UTC": {
			now: time.Date(2026, 10, 6, 18, 30, 0, 0, west), want: []time.Time{yesterday.AddDate(0, 0, -1)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			builder := &fakeBuilder{sealed: &Sealed{}}
			store := newFakeStore()
			require.NoError(t, preparerOf(builder, store).PrepareDue(t.Context(), tc.now))
			require.Equal(t, tc.want, builder.days)
			require.Len(t, store.reports, len(tc.want))
		})
	}
}

// syncBuilder counts its calls under a lock, and may panic on the first of them.
type syncBuilder struct {
	mu         sync.Mutex
	calls      int
	panicFirst bool
}

func (b *syncBuilder) Build(context.Context, time.Time, time.Time) (*Sealed, error) {
	b.mu.Lock()
	b.calls++
	first := b.calls == 1
	b.mu.Unlock()
	if first && b.panicFirst {
		panic("a value that must not be logged")
	}
	return nil, ErrNoLicenseInForce
}

func (b *syncBuilder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// loopOf starts Every on a clock at noon, and stops it when the test ends.
func loopOf(t *testing.T, builder ReportBuilder, logger *slog.Logger, first, every time.Duration) {
	t.Helper()
	preparer := NewPreparer(builder, newFakeStore(), logger)
	preparer.firstPassAfter = first
	preparer.now = func() time.Time { return now.Add(12 * time.Hour) }
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		preparer.Every(ctx, every)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func Test_Every_RunsAFirstPassSoonAfterItStarts(t *testing.T) {
	builder := &syncBuilder{}
	loopOf(t, builder, slog.New(slog.DiscardHandler), time.Millisecond, time.Hour)

	require.Eventually(t, func() bool { return builder.count() == 1 }, 5*time.Second, time.Millisecond)
}

func Test_Every_ThenRunsAtEachInterval(t *testing.T) {
	builder := &syncBuilder{}
	loopOf(t, builder, slog.New(slog.DiscardHandler), time.Millisecond, time.Millisecond)

	require.Eventually(t, func() bool { return builder.count() >= 3 }, 5*time.Second, time.Millisecond)
}

func Test_Every_WaitsBeforeItsFirstPass(t *testing.T) {
	builder := &syncBuilder{}
	loopOf(t, builder, slog.New(slog.DiscardHandler), time.Hour, time.Millisecond)

	time.Sleep(20 * time.Millisecond)
	require.Zero(t, builder.count())
}

func Test_Every_APassThatPanicsDoesNotStopTheLoop(t *testing.T) {
	var logs syncBuffer
	builder := &syncBuilder{panicFirst: true}
	loopOf(t, builder, slog.New(slog.NewTextHandler(&logs, nil)), time.Millisecond, time.Millisecond)

	require.Eventually(t, func() bool { return builder.count() >= 2 }, 5*time.Second, time.Millisecond)
	require.Contains(t, logs.String(), "panicked=true")
	require.NotContains(t, logs.String(), "must not be logged")
}

// syncBuffer is a log sink a loop may write to while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
