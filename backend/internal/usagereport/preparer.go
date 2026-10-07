package usagereport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
)

// buildTimeout bounds the making of one report: the builder bounds only its optional readings,
// so a database or an orchestrator that does not answer would otherwise hold the pass.
const buildTimeout = 2 * time.Minute

// closedAfter is how long past midnight UTC the day before is taken as closed. A run counts for
// the day the database recorded its end on, and the clock of this process may differ from the
// one of the database by seconds: a run recorded at 23:59:59 must be in the report.
const closedAfter = 5 * time.Minute

// keptMonths is how long a prepared report is kept, counted from the day just prepared.
const keptMonths = 24

// ReportBuilder makes the sealed report of a day. *Builder is one.
type ReportBuilder interface {
	Build(ctx context.Context, day, now time.Time) (*Sealed, error)
}

// ReportStore keeps the report of each day. *usagestore.Store is one.
type ReportStore interface {
	Report(ctx context.Context, day time.Time) (*usagestore.StoredReport, error)
	SaveReport(ctx context.Context, report usagestore.StoredReport) (saved bool, err error)
	DeleteReportsBefore(ctx context.Context, day time.Time) error
}

// Preparer prepares the report of the day before, once a day.
type Preparer struct {
	builder ReportBuilder
	store   ReportStore
	logger  *slog.Logger

	buildTimeout time.Duration
}

func NewPreparer(builder ReportBuilder, store ReportStore, logger *slog.Logger) *Preparer {
	return &Preparer{
		builder: builder, store: store, logger: logger, buildTimeout: buildTimeout,
	}
}

// PrepareDue prepares the report of yesterday (UTC) when none is stored yet. Only yesterday is
// prepared: the state of the instance is known at the present only, so a missed day is not made
// up afterwards. Several replicas may run it at once: the store keeps the first report of a day
// and leaves the others, so there is one report whichever replica made it.
//
// Nothing is prepared in the first minutes of a day: the report of a day is made once, so it
// waits until every run of that day is sure to be recorded.
func (p *Preparer) PrepareDue(ctx context.Context, now time.Time) error {
	today := now.UTC().Truncate(24 * time.Hour)
	if now.Sub(today) < closedAfter {
		return nil
	}
	day := today.AddDate(0, 0, -1)
	dayText := day.Format(time.DateOnly)

	stored, err := p.store.Report(ctx, day)
	if err != nil {
		return fmt.Errorf("unable to look for the report of the day: %w", err)
	}
	if stored != nil {
		return nil
	}

	buildCtx, cancel := context.WithTimeout(ctx, p.buildTimeout)
	defer cancel()
	sealed, err := p.builder.Build(buildCtx, day, now)
	if errors.Is(err, ErrNoLicenseInForce) {
		p.logger.DebugContext(ctx, "no usage report is prepared: no license is in force", "day", dayText)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to make the usage report of the day: %w", err)
	}
	// The optional readings of a report are skipped when they fail, which includes a context
	// that ended: a report made past its deadline is not the one that was asked for.
	if err := buildCtx.Err(); err != nil {
		return fmt.Errorf("the usage report of the day was not made in time: %w", err)
	}

	saved, err := p.store.SaveReport(ctx, usagestore.StoredReport{
		Day:            day,
		Document:       sealed.Document,
		Seal:           sealed.Seal,
		KeyFingerprint: sealed.KeyFingerprint,
		PreparedAt:     now.UTC(),
	})
	if err != nil {
		return fmt.Errorf("unable to keep the usage report of the day: %w", err)
	}
	if !saved {
		p.logger.DebugContext(ctx, "the usage report of the day was prepared by another replica", "day", dayText)
		return nil
	}
	p.logger.InfoContext(ctx, "the usage report of the day is prepared", "day", dayText)

	if err := p.store.DeleteReportsBefore(ctx, day.AddDate(0, -keptMonths, 0)); err != nil {
		p.logger.WarnContext(ctx, "could not drop the old usage reports", "error", err)
	}
	return nil
}
