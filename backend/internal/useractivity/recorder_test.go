package useractivity

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/stretchr/testify/require"
)

type call struct {
	userId string
	day    time.Time
}

type fakeStore struct {
	mu    sync.Mutex
	calls []call
	err   error
	block chan struct{}
	wrote chan struct{}
}

func newFakeStore() *fakeStore { return &fakeStore{wrote: make(chan struct{}, 16)} }

func (f *fakeStore) UserSeen(ctx context.Context, userId string, day time.Time) error {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{userId, day})
	err := f.err
	f.mu.Unlock()
	f.wrote <- struct{}{}
	return err
}

func (f *fakeStore) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func waitWrite(t *testing.T, s *fakeStore) {
	t.Helper()
	select {
	case <-s.wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("the store was not called")
	}
}

func newTestRecorder(s Store, at *time.Time) *Recorder {
	r := NewRecorder(s)
	r.now = func() time.Time { return *at }
	return r
}

func Test_Seen_TwiceTheSameDay_WritesOnce(t *testing.T) {
	s := newFakeStore()
	at := time.Date(2026, 10, 7, 23, 59, 0, 0, time.UTC)
	r := newTestRecorder(s, &at)

	r.Seen(context.Background(), "u1")
	waitWrite(t, s)
	r.Seen(context.Background(), "u1")
	r.Seen(context.Background(), "u2")
	waitWrite(t, s)

	require.Equal(t, 2, s.count())
	require.Equal(t, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), s.calls[0].day)
}

func Test_Seen_DayChanges_WritesAgain(t *testing.T) {
	s := newFakeStore()
	at := time.Date(2026, 10, 7, 23, 59, 0, 0, time.UTC)
	r := newTestRecorder(s, &at)

	r.Seen(context.Background(), "u1")
	waitWrite(t, s)
	at = at.Add(2 * time.Minute)
	r.Seen(context.Background(), "u1")
	waitWrite(t, s)

	require.Equal(t, 2, s.count())
	require.Equal(t, time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), s.calls[1].day)
}

func Test_Seen_StoreFails_NextCallRetries(t *testing.T) {
	s := newFakeStore()
	s.setErr(errors.New("boom"))
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	r := newTestRecorder(s, &at)

	r.Seen(context.Background(), "u1")
	waitWrite(t, s)
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.seen) == 0
	}, 5*time.Second, time.Millisecond)

	s.setErr(nil)
	r.Seen(context.Background(), "u1")
	waitWrite(t, s)
	require.Equal(t, 2, s.count())
}

func Test_Seen_StoreBlocked_ReturnsAtOnce(t *testing.T) {
	s := newFakeStore()
	s.block = make(chan struct{})
	t.Cleanup(func() { close(s.block) })
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	r := newTestRecorder(s, &at)

	done := make(chan struct{})
	go func() {
		r.Seen(context.Background(), "u1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Seen waited for the store")
	}
}

func Test_Seen_ConcurrentCalls_WriteOncePerUser(t *testing.T) {
	s := newFakeStore()
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	r := newTestRecorder(s, &at)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Seen(context.Background(), "u1")
		}()
	}
	wg.Wait()
	waitWrite(t, s)
	time.Sleep(20 * time.Millisecond) // lets a wrong second write show up
	require.Equal(t, 1, s.count())
}

func Test_Seen_NoStore_DoesNothing(t *testing.T) {
	var r *Recorder
	r.Seen(context.Background(), "u1")
	NewRecorder(nil).Seen(context.Background(), "u1")
}

// panickingStore panics on its first write and counts the others.
type panickingStore struct {
	mu    sync.Mutex
	calls int
	wrote chan struct{}
}

func (p *panickingStore) UserSeen(context.Context, string, time.Time) error {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	if first {
		panic("a value that must not be logged")
	}
	p.wrote <- struct{}{}
	return nil
}

// syncBuffer is a log sink the write may reach while the test reads it.
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

func Test_Seen_AWriteThatPanics_IsLoggedAndTheNextCallRetries(t *testing.T) {
	var logs syncBuffer
	s := &panickingStore{wrote: make(chan struct{}, 1)}
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	r := newTestRecorder(s, &at)
	ctx := logger_interceptor.SetLoggerContext(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))

	r.Seen(ctx, "u1")
	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.seen) == 0 && strings.Contains(logs.String(), "panicked=true")
	}, 5*time.Second, time.Millisecond)
	require.NotContains(t, logs.String(), "must not be logged")

	r.Seen(ctx, "u1")
	select {
	case <-s.wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("the user was not noted again")
	}
}
