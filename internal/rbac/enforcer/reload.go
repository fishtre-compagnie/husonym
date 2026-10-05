package enforcer

import (
	"context"
	"time"
)

// LoadPolicy reads the rules again from their store and replaces those held in memory. If the
// read fails, the rules held stay as they were.
func (e *Enforcer) LoadPolicy() error {
	e.reloading.Lock()
	defer e.reloading.Unlock()
	return e.inner.LoadPolicy()
}

// reloadEvery reads the rules again at each period, until ctx ends. A reload that fails is
// tried again at the next period: the rules held meanwhile are those of the last one that
// succeeded.
func (e *Enforcer) reloadEvery(ctx context.Context, period time.Duration) {
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.LoadPolicy(); err != nil && ctx.Err() == nil {
				e.logger.WarnContext(ctx, "unable to read the access rules again, keeping those known", "error", err)
			}
		}
	}
}
