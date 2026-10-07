package publicapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/intake"
)

// Promoter promotes the reports pending under a fingerprint whose license is now known.
type Promoter interface {
	PromotePending(ctx context.Context, fingerprint string) (stored, discarded int, err error)
}

// PendingStore is what the maintenance needs of the store.
type PendingStore interface {
	PendingFingerprintsNowKnown(ctx context.Context) ([]string, error)
	PurgePending(ctx context.Context, olderThan time.Time) (int64, error)
}

// RunMaintenance keeps the pending reports tidy: once at start, then at each tick, it promotes
// the pending reports whose license is now known and purges those kept for too long. Each pass
// runs under recover, so that a failure ends the pass and not the loop. It returns when ctx ends.
func RunMaintenance(ctx context.Context, promoter Promoter, store PendingStore, logger *slog.Logger, tick time.Duration) {
	pass(ctx, promoter, store, logger)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass(ctx, promoter, store, logger)
		}
	}
}

// pass is one run of the maintenance. A panic is logged in fixed words: what it carries is not.
func pass(ctx context.Context, promoter Promoter, store PendingStore, logger *slog.Logger) {
	defer func() {
		if recover() != nil {
			logger.Error("maintenance of the pending reports failed")
		}
	}()
	if ctx.Err() != nil {
		return
	}

	// A pass cut by the end of ctx is the server stopping, not a failure of ours.
	fail := func(what string, err error) bool {
		if err == nil {
			return false
		}
		if ctx.Err() == nil {
			logger.Error(what, "error", err.Error())
		}
		return true
	}

	var stored, discarded int
	var failed error
	fingerprints, err := store.PendingFingerprintsNowKnown(ctx)
	failedList := fail("unable to list the pending reports to promote", err)
	for _, fingerprint := range fingerprints {
		s, d, err := promoter.PromotePending(ctx, fingerprint)
		stored += s
		discarded += d
		failed = errors.Join(failed, err)
	}
	failedPromote := fail("unable to promote some pending reports", failed)

	purged, err := store.PurgePending(ctx, time.Now().Add(-intake.PendingKept))
	failedPurge := fail("unable to purge the pending reports", err)

	if !failedList && !failedPromote && !failedPurge {
		logger.Info("pending reports maintained", "stored", stored, "discarded", discarded, "purged", purged)
	}
}
