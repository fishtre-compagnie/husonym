package usagereport

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/require"
)

// syncBuilder counts its calls under a lock, and may panic on the first of them or fail them all.
type syncBuilder struct {
	mu         sync.Mutex
	calls      int
	panicFirst bool
	err        error
}

func (b *syncBuilder) Build(context.Context, time.Time, time.Time) (*Sealed, error) {
	b.mu.Lock()
	b.calls++
	first := b.calls == 1
	b.mu.Unlock()
	if first && b.panicFirst {
		panic("a value that must not be logged")
	}
	if b.err != nil {
		return nil, b.err
	}
	return nil, ErrNoLicenseInForce
}

func (b *syncBuilder) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// syncKeyMode counts, under a lock, how many times the sending asked what the key provides.
type syncKeyMode struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (k *syncKeyMode) TelemetryMode(context.Context, time.Time) (license.TelemetryMode, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls++
	return license.TelemetryOnline, k.err
}

func (k *syncKeyMode) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.calls
}

// daily is a loop on a clock at noon, with what its two halves are made of.
type daily struct {
	builder   *syncBuilder
	key       *syncKeyMode
	store     *fakeSendingStore
	transport *fakeTransport
	logs      *syncBuffer
	loop      *Daily
}

func newDaily(builder *syncBuilder) *daily {
	d := &daily{
		builder:   builder,
		key:       &syncKeyMode{},
		store:     &fakeSendingStore{},
		transport: &fakeTransport{failing: map[string]error{}},
		logs:      &syncBuffer{},
	}
	logger := slog.New(slog.NewTextHandler(d.logs, nil))
	noon := func() time.Time { return now.Add(12 * time.Hour) }
	sender := NewSender(d.store, &fakeLicense{inForce: true}, d.key, "", true, d.transport, logger)
	sender.now = noon
	d.loop = NewDaily(NewPreparer(builder, newFakeStore(), logger), sender, nil, logger)
	d.loop.now = noon
	return d
}

// start runs Every, and stops it when the test ends.
func (d *daily) start(t *testing.T, first, every time.Duration) {
	t.Helper()
	d.loop.firstPassAfter = first
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		d.loop.Every(ctx, every)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func Test_Every_StopsWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		newDaily(&syncBuilder{}).loop.Every(ctx, time.Hour)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Every did not stop")
	}
}

func Test_Every_RunsAFirstPassSoonAfterItStarts(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.start(t, time.Millisecond, time.Hour)

	require.Eventually(t, func() bool { return d.builder.count() == 1 && d.key.count() == 1 }, 5*time.Second, time.Millisecond)
}

func Test_Every_ThenRunsAtEachInterval(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.start(t, time.Millisecond, time.Millisecond)

	require.Eventually(t, func() bool { return d.builder.count() >= 3 && d.key.count() >= 3 }, 5*time.Second, time.Millisecond)
}

func Test_Every_WaitsBeforeItsFirstPass(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.start(t, time.Hour, time.Millisecond)

	time.Sleep(20 * time.Millisecond)
	require.Zero(t, d.builder.count())
	require.Zero(t, d.key.count())
}

func Test_NewDaily_WaitsTwoMinutesBeforeItsFirstPass(t *testing.T) {
	require.Equal(t, 2*time.Minute, newDaily(&syncBuilder{}).loop.firstPassAfter)
}

func Test_Every_APreparationThatPanicsStopsNeitherTheSendingNorTheLoop(t *testing.T) {
	d := newDaily(&syncBuilder{panicFirst: true})
	d.start(t, time.Millisecond, time.Millisecond)

	require.Eventually(t, func() bool { return d.builder.count() >= 2 }, 5*time.Second, time.Millisecond)
	require.Contains(t, d.logs.String(), "panicked=true")
	require.NotContains(t, d.logs.String(), "must not be logged")
}

func Test_Pass_PreparesThenSends(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.store.withReports("2026-10-06").sendingSince(now.AddDate(0, 0, -5))

	d.loop.Pass(t.Context())
	require.Equal(t, 1, d.builder.count())
	require.Equal(t, []string{"2026-10-06"}, d.transport.days())
}

func Test_Pass_APreparationThatFailsDoesNotPreventTheSending(t *testing.T) {
	d := newDaily(&syncBuilder{err: errors.New("boom")})
	d.store.withReports("2026-10-06").sendingSince(now.AddDate(0, 0, -5))

	d.loop.Pass(t.Context())
	require.Equal(t, []string{"2026-10-06"}, d.transport.days())
	require.Contains(t, d.logs.String(), "could not prepare the usage report of the day")
}

func Test_Pass_APreparationThatPanicsDoesNotPreventTheSending(t *testing.T) {
	d := newDaily(&syncBuilder{panicFirst: true})
	d.store.withReports("2026-10-06").sendingSince(now.AddDate(0, 0, -5))

	d.loop.Pass(t.Context())
	require.Equal(t, []string{"2026-10-06"}, d.transport.days())
	require.Contains(t, d.logs.String(), "panicked=true")
	require.NotContains(t, d.logs.String(), "must not be logged")
}

func Test_Pass_ASendingThatFailsIsLoggedAndTheNextPassPreparesAndSendsAgain(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.key.err = errors.New("boom")

	d.loop.Pass(t.Context())
	d.loop.Pass(t.Context())
	require.Equal(t, 2, d.builder.count())
	require.Equal(t, 2, d.key.count())
	require.Contains(t, d.logs.String(), "could not send the usage report of the instance")
	require.Contains(t, d.logs.String(), "boom")
}

func Test_Pass_ASendingThatPanicsEndsNeitherTheProcessNorTheNextPass(t *testing.T) {
	d := newDaily(&syncBuilder{})
	d.store.panicOnStart = true

	d.loop.Pass(t.Context())
	d.loop.Pass(t.Context())
	require.Equal(t, 2, d.builder.count())
	require.Equal(t, 2, d.key.count())
	require.Contains(t, d.logs.String(), "could not send the usage report of the instance")
	require.Contains(t, d.logs.String(), "panicked=true")
	require.NotContains(t, d.logs.String(), "must not be logged")
}

func Test_Pass_AContextThatEndedIsNotLoggedAsAFailure(t *testing.T) {
	d := newDaily(&syncBuilder{err: errors.New("boom")})
	d.key.err = errors.New("boom")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	d.loop.Pass(ctx)
	require.Empty(t, d.logs.String())
}

func Test_Pass_WithoutASenderOnlyPrepares(t *testing.T) {
	builder := &syncBuilder{}
	var logs syncBuffer
	loop := NewDaily(NewPreparer(builder, newFakeStore(), slog.New(slog.NewTextHandler(&logs, nil))), nil, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	loop.now = func() time.Time { return now.Add(12 * time.Hour) }

	loop.Pass(t.Context())
	loop.Pass(t.Context())
	require.Equal(t, 2, builder.count())
	require.Empty(t, logs.String())
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
