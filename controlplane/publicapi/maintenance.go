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

	fingerprints, err := store.PendingFingerprintsNowKnown(ctx)
	if err != nil {
		logger.Error("unable to list the pending reports to promote", "error", err.Error())
	}
	var stored, discarded int
	var failed error
	for _, fingerprint := range fingerprints {
		s, d, err := promoter.PromotePending(ctx, fingerprint)
		stored += s
		discarded += d
		failed = errors.Join(failed, err)
	}
	if failed != nil {
		logger.Error("unable to promote some pending reports", "error", failed.Error())
	}

	purged, err := store.PurgePending(ctx, time.Now().Add(-intake.PendingKept))
	if err != nil {
		logger.Error("unable to purge the pending reports", "error", err.Error())
	}
	logger.Info("pending reports maintained", "stored", stored, "discarded", discarded, "purged", purged)
}
