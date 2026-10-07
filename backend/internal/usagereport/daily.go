package usagereport

import (
	"context"
	"log/slog"
	"time"
)

// firstPassAfter is how long after it starts the loop makes its first pass, so that a process
// restarted more often than the interval still prepares and sends its report.
const firstPassAfter = 2 * time.Minute

// Daily is the loop of the usage report: each pass prepares the report that is due, then sends
// those that are due.
type Daily struct {
	preparer *Preparer
	sender   *Sender
	logger   *slog.Logger

	firstPassAfter time.Duration
	now            func() time.Time
}

func NewDaily(preparer *Preparer, sender *Sender, logger *slog.Logger) *Daily {
	return &Daily{
		preparer: preparer, sender: sender, logger: logger,
		firstPassAfter: firstPassAfter, now: time.Now,
	}
}

// Every makes a pass soon after it starts, then at the given interval, until ctx is done. A
// pass that fails is logged and the next one tries again.
func (d *Daily) Every(ctx context.Context, every time.Duration) {
	first := time.NewTimer(d.firstPassAfter)
	defer first.Stop()
	select {
	case <-ctx.Done():
		return
	case <-first.C:
		d.Pass(ctx)
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.Pass(ctx)
		}
	}
}

// Pass runs one preparation then one sending. Each stands alone: one that fails or panics is
// logged, and prevents neither the other nor the next pass.
func (d *Daily) Pass(ctx context.Context) {
	now := d.now()
	d.attempt(ctx, "could not prepare the usage report of the day", func() error {
		return d.preparer.PrepareDue(ctx, now)
	})
	d.attempt(ctx, "could not send the usage report of the instance", func() error {
		return d.sender.SendDue(ctx, now)
	})
}

// attempt runs one half of a pass. A panic ends that half and not the process: the usage report
// never takes the instance down. The value of the panic is not logged, it may hold anything.
func (d *Daily) attempt(ctx context.Context, failure string, do func() error) {
	defer func() {
		if recover() != nil {
			d.logger.ErrorContext(ctx, failure, "panicked", true)
		}
	}()
	if err := do(); err != nil && ctx.Err() == nil {
		d.logger.WarnContext(ctx, failure, "error", err)
	}
}
