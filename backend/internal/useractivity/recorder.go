// Package useractivity notes the last day each signed-in user was seen, off the request path.
package useractivity

import (
	"context"
	"sync"
	"time"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
)

// Store keeps the last day a user was seen. *usagestore.Store is one.
type Store interface {
	UserSeen(ctx context.Context, userId string, day time.Time) error
}

// writeTimeout bounds a write: a database that does not answer holds nothing but its goroutine.
var writeTimeout = 2 * time.Second

// Recorder notes a user once a day, per process.
type Recorder struct {
	store Store
	now   func() time.Time

	mu   sync.Mutex
	day  time.Time
	seen map[string]struct{}
}

func NewRecorder(store Store) *Recorder {
	return &Recorder{store: store, now: time.Now, seen: map[string]struct{}{}}
}

// Seen notes that the user was seen today. It never blocks the caller and never fails it.
//
// The users already noted today (UTC) are kept in memory, so that the store is asked once per
// user per day per replica; the memory is emptied when the day changes. The write runs in its
// own goroutine, on a context detached from the request and bounded in time. A write that fails
// or panics is logged and the user is forgotten, to be noted again by a later call. The value of
// a panic is not logged, it may hold anything.
func (r *Recorder) Seen(ctx context.Context, userId string) {
	if r == nil || r.store == nil || userId == "" {
		return
	}
	day := r.today()
	if !r.mark(day, userId) {
		return
	}
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	writeCtx := context.WithoutCancel(ctx)
	go func() {
		writeCtx, cancel := context.WithTimeout(writeCtx, writeTimeout)
		defer cancel()
		defer func() {
			if recover() != nil {
				logger.Error("unable to note the day a user was seen", "panicked", true)
				r.forget(day, userId)
			}
		}()
		if err := r.store.UserSeen(writeCtx, userId, day); err != nil {
			logger.Warn("unable to note the day a user was seen", "error", err.Error())
			r.forget(day, userId)
		}
	}()
}

func (r *Recorder) today() time.Time {
	t := r.now().UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// mark reports whether the user was not yet noted on the day, and notes it.
func (r *Recorder) mark(day time.Time, userId string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !day.Equal(r.day) {
		r.day = day
		r.seen = map[string]struct{}{}
	}
	if _, ok := r.seen[userId]; ok {
		return false
	}
	r.seen[userId] = struct{}{}
	return true
}

func (r *Recorder) forget(day time.Time, userId string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if day.Equal(r.day) {
		delete(r.seen, userId)
	}
}
