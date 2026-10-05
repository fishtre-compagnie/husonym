package sqlmanager_shared

import (
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v7"
)

// CatalogReadAttempts bounds how often a read of the catalog is tried.
const CatalogReadAttempts = 4

// CatalogRetryOptions are the options a manager reads a changing catalog again with: it waits
// 50 ms before the second try, and twice as long at each one after.
func CatalogRetryOptions() []backoff.RetryOption {
	wait := backoff.NewExponentialBackOff()
	wait.InitialInterval = 50 * time.Millisecond
	wait.Multiplier = 2
	wait.RandomizationFactor = 0
	return []backoff.RetryOption{
		backoff.WithBackOff(wait),
		backoff.WithMaxTries(CatalogReadAttempts),
		backoff.WithNotify(func(err error, _ time.Duration) {
			slog.Default().Warn("the catalog changed under its read: reading it again", "error", err)
		}),
	}
}
