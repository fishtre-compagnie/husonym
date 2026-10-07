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
// the pending reports whose license is now known and purges those kept for too long, counted
// from the time now gives. A failure ends a step of a pass, not the loop. It returns when ctx
// ends.
func RunMaintenance(
	ctx context.Context, promoter Promoter, store PendingStore, logger *slog.Logger,
	now func() time.Time, tick time.Duration,
) {
	pass(ctx, promoter, store, logger, now)
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pass(ctx, promoter, store, logger, now)
		}
	}
}

// pass is one run of the maintenance: the promotion, then the purge. Each runs under its own
// recover, so that what one panics with does not skip the other.
func pass(
	ctx context.Context, promoter Promoter, store PendingStore, logger *slog.Logger, now func() time.Time,
) {
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
	promoted := guarded(logger, "promotion", func() bool {
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
		return !failedList && !failedPromote
	})

	var purged int64
	purgedAll := guarded(logger, "purge", func() bool {
		var err error
		purged, err = store.PurgePending(ctx, now().Add(-intake.PendingKept))
		return !fail("unable to purge the pending reports", err)
	})

	if promoted && purgedAll {
		logger.Info("pending reports maintained", "stored", stored, "discarded", discarded, "purged", purged)
	}
}

// guarded runs a step of the maintenance and says whether it went well. A panic is logged in
// fixed words and the name of the step: what it carries is not.
func guarded(logger *slog.Logger, name string, step func() bool) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
			logger.Error("maintenance of the pending reports failed", "step", name)
		}
	}()
	return step()
}
