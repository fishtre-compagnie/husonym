package usagereport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

const (
	// waitBeforeFirst is how long an instance that starts sending waits before it sends anything.
	waitBeforeFirst = 24 * time.Hour
	// retryAfter is how long a report that could not be sent waits before it is tried again.
	retryAfter = 6 * time.Hour
	// sentDays is how many closed days back a report is still sent.
	sentDays = 30
)

// SendingStore keeps since when the instance sends its report, and what became of the report
// of each day. *usagestore.Store is one.
type SendingStore interface {
	SendingSince(ctx context.Context) (*time.Time, error)
	StartSending(ctx context.Context, at time.Time) error
	StopSending(ctx context.Context) error
	ClaimReport(ctx context.Context, from, to, notAttemptedSince, now time.Time) (*usagestore.StoredReport, error)
	MarkReportSent(ctx context.Context, day, at time.Time) error
	ListReportSendings(ctx context.Context, from, to time.Time) ([]usagestore.ReportSending, error)
}

// KeyModeSource gives what the key in force provides for the usage report, or
// ErrNoLicenseInForce. *InstanceKey is one.
type KeyModeSource interface {
	TelemetryMode(ctx context.Context, now time.Time) (license.TelemetryMode, error)
}

// Sender sends the usage reports that are due, when the license provides for it.
type Sender struct {
	store     SendingStore
	license   license.EEInterface
	key       KeyModeSource
	setting   string
	transport Transport
	logger    *slog.Logger

	waitBeforeFirst time.Duration
	retryAfter      time.Duration
}

// NewSender takes the license of the process, which says whether anything is sent at all, the
// key of the instance, which says what it provides for the report, and the setting of the
// operator as it is written (HUSONYM_TELEMETRY), which may only lower it.
func NewSender(
	store SendingStore,
	lic license.EEInterface,
	key KeyModeSource,
	setting string,
	transport Transport,
	logger *slog.Logger,
) *Sender {
	return &Sender{
		store: store, license: lic, key: key, setting: setting, transport: transport, logger: logger,
		waitBeforeFirst: waitBeforeFirst, retryAfter: retryAfter,
	}
}

// SendDue sends the reports that are due, from the oldest: those of the closed days since the
// instance started sending, thirty days back at most, that were not sent yet nor tried in the
// last hours. Nothing is sent without a license in force, nor in a mode that is not the one that
// sends, which also forgets since when the instance was sending: an instance that comes back to
// sending starts again, and what was prepared in between stays.
//
// A report that cannot be sent is logged and ends the pass: it is not an error, and it is tried
// again later. Several replicas may run it at once: a report is claimed by one of them only.
func (s *Sender) SendDue(ctx context.Context, now time.Time) error {
	// Asked first, and of what the process holds: without a license nothing is read at all.
	if !s.license.IsValid() {
		return nil
	}
	keyMode, err := s.key.TelemetryMode(ctx, now)
	if errors.Is(err, ErrNoLicenseInForce) {
		return nil
	}
	if err != nil {
		return err
	}
	if mode, _ := telemetry.EffectiveMode(keyMode, s.setting); mode != telemetry.ModeOnline {
		if err := s.store.StopSending(ctx); err != nil {
			return fmt.Errorf("unable to record that the usage report is not sent: %w", err)
		}
		return nil
	}

	if err := s.store.StartSending(ctx, now); err != nil {
		return fmt.Errorf("unable to record since when the usage report is sent: %w", err)
	}
	since, err := s.store.SendingSince(ctx)
	if err != nil {
		return err
	}
	// Nil when another replica, started with another setting, stopped the sending in between.
	if since == nil || now.Sub(*since) < s.waitBeforeFirst {
		return nil
	}

	yesterday := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	from := yesterday.AddDate(0, 0, -(sentDays - 1))
	if sinceDay := since.UTC().Truncate(24 * time.Hour); sinceDay.After(from) {
		from = sinceDay
	}

	for {
		report, err := s.store.ClaimReport(ctx, from, yesterday, now.Add(-s.retryAfter), now)
		if err != nil {
			return err
		}
		if report == nil {
			return nil
		}
		dayText := report.Day.Format(time.DateOnly)
		if err := s.transport.Post(ctx, report); err != nil {
			// A process that stops has not failed to send: the report is tried again later.
			if ctx.Err() == nil {
				s.logger.WarnContext(ctx, "could not send the usage report of the day",
					"day", dayText, "attempts", s.attempts(ctx, report.Day), "error", err)
			}
			// The next report is not claimed: a claim counts as an attempt.
			return nil
		}
		if err := s.store.MarkReportSent(ctx, report.Day, now); err != nil {
			return fmt.Errorf("unable to record that the usage report of %s was sent: %w", dayText, err)
		}
		// The document is made of numbers and of values from closed lists: it is logged as it left.
		s.logger.InfoContext(ctx, "the usage report of the day is sent", "day", dayText, "document", string(report.Document))
	}
}

// attempts reads how many times the report of a day was tried, for the log. Zero when it
// cannot be read: the log line is written all the same.
func (s *Sender) attempts(ctx context.Context, day time.Time) int32 {
	sendings, err := s.store.ListReportSendings(ctx, day, day)
	if err != nil || len(sendings) != 1 {
		return 0
	}
	return sendings[0].Attempts
}
