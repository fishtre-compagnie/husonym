package usagereport

import (
	"context"
	"log/slog"
	"time"
)

// firstPassAfter is how long after it starts the loop makes its first pass, so that a process
// restarted more often than the interval still prepares and sends its report.
const firstPassAfter = 2 * time.Minute

// Daily is the loop of the usage report: each pass prepares the report that is due, sends those
// that are due, then asks for the license that succeeds the one of the instance when that is due.
type Daily struct {
	preparer *Preparer
	sender   *Sender
	renewer  *Renewer
	logger   *slog.Logger

	firstPassAfter time.Duration
	now            func() time.Time
}

// NewDaily takes a nil sender for an instance that only prepares its report, and a nil renewer
// for one that does not ask for its license.
func NewDaily(preparer *Preparer, sender *Sender, renewer *Renewer, logger *slog.Logger) *Daily {
	return &Daily{
		preparer: preparer, sender: sender, renewer: renewer, logger: logger,
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

// Pass runs one preparation, one sending, then one ask for the license. Each stands alone: one
// that fails or panics is logged, and prevents neither the others nor the next pass.
func (d *Daily) Pass(ctx context.Context) {
	now := d.now()
	d.attempt(ctx, "could not prepare the usage report of the day", func() error {
		return d.preparer.PrepareDue(ctx, now)
	})
	if d.sender != nil {
		d.attempt(ctx, "could not send the usage report of the instance", func() error {
			return d.sender.SendDue(ctx, now)
		})
	}
	if d.renewer != nil {
		d.attempt(ctx, "could not ask for the license that succeeds the one of the instance", func() error {
			return d.renewer.AskIfDue(ctx)
		})
	}
}

// attempt runs one step of a pass. A panic ends that step and not the process: the usage report
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
