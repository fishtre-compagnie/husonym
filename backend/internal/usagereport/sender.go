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
	// WaitBeforeFirst is how long the first report of an instance that starts sending waits, once
	// it is prepared, before it is sent: the time it can be read in before it leaves.
	WaitBeforeFirst = 24 * time.Hour
	// SentDays is how many closed days back a report is still sent.
	SentDays = 30
	// retryAfter is how long a report that could not be sent waits before it is tried again.
	retryAfter = 6 * time.Hour
	// passTimeout bounds one pass of the sending: a database that does not answer ends the pass,
	// and never holds the loop the preparation runs in too.
	passTimeout = 2 * time.Minute
	// recordTimeout bounds what is recorded of a report once its request has ended, which is
	// done whatever became of the pass: a report that left is marked even as the process stops.
	recordTimeout = 5 * time.Second
)

// SendingStore keeps since when the instance sends its report, and what became of the report
// of each day. *usagestore.Store is one.
type SendingStore interface {
	SendingSince(ctx context.Context) (*time.Time, error)
	StartSending(ctx context.Context, at time.Time) error
	StopSending(ctx context.Context) error
	LastSentAt(ctx context.Context) (*time.Time, error)
	ClaimReport(ctx context.Context, claim usagestore.ReportClaim) (*usagestore.StoredReport, error)
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
	passTimeout     time.Duration
	recordTimeout   time.Duration
	now             func() time.Time
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
		waitBeforeFirst: WaitBeforeFirst, retryAfter: retryAfter,
		passTimeout: passTimeout, recordTimeout: recordTimeout, now: time.Now,
	}
}

// WaitsBeforeFirst tells whether the reports of an instance that sends since the given moment
// still wait before they are sent: they do until one was sent since.
func WaitsBeforeFirst(since time.Time, lastSent *time.Time) bool {
	return lastSent == nil || lastSent.Before(since)
}

// SendDue sends the reports that are due, from the oldest: those of the closed days since the
// instance started sending, thirty days back at most, that were not sent yet nor tried in the
// last hours. Nothing is sent without a license in force, nor in a mode that is not the one that
// sends, which also forgets since when the instance was sending: an instance that comes back to
// sending starts again, and what was prepared in between stays.
//
// Until a report was sent since the instance started sending, a report is due only a day after
// it was prepared: the first one to leave can be read for a day before it does. Each report
// waits its own day, so one that could not be sent does not let a newer one leave earlier. Once
// one was sent, those that follow go as soon as they are prepared.
//
// now is the moment of the pass, which what is due is told from; a report is claimed and marked
// at the moment it is, read from the clock. The pass is bounded: reports left when it ends go
// at a later pass.
//
// A report that cannot be sent is logged and ends the pass: it is not an error, and it is tried
// again later. Several replicas may run it at once: a report is claimed by one of them only.
//
// A report may arrive twice: once it left, it is logged, then marked, and one that could not
// be marked is sent again later. It is then the same report: same day, same bytes, same seal.
func (s *Sender) SendDue(parent context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(parent, s.passTimeout)
	defer cancel()

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
	if since == nil {
		return nil
	}
	lastSent, err := s.store.LastSentAt(ctx)
	if err != nil {
		return err
	}
	waits := WaitsBeforeFirst(*since, lastSent)

	yesterday := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	from := yesterday.AddDate(0, 0, -(SentDays - 1))
	if sinceDay := since.UTC().Truncate(24 * time.Hour); sinceDay.After(from) {
		from = sinceDay
	}

	for {
		// Nothing is claimed once the pass or the process has ended: a claim counts as an attempt.
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("the sending of the usage reports ended before all that is due was sent: %w", err)
		}
		claimedAt := s.now()
		claim := usagestore.ReportClaim{
			From: from, To: yesterday, NotAttemptedSince: claimedAt.Add(-s.retryAfter), At: claimedAt,
		}
		if waits {
			claim.PreparedBy = new(claimedAt.Add(-s.waitBeforeFirst))
		}
		report, err := s.store.ClaimReport(ctx, claim)
		if err != nil {
			return err
		}
		if report == nil {
			return nil
		}
		if err := s.transport.Post(ctx, report); err != nil {
			// A process that stops has not failed to send: the report is tried again later.
			if parent.Err() == nil {
				s.logFailure(parent, report.Day, err)
			}
			// The next report is not claimed: a claim counts as an attempt.
			return nil
		}
		if err := s.sent(parent, report); err != nil {
			return err
		}
		// A report was sent since the instance started sending: the others go as they are due.
		waits = false
	}
}

// sent logs a report that left, then marks it. The line is written first, so that a report
// that left is always told of; and the marking does not end with the pass nor with the process,
// so that a sending that is done is not done again for a mark that was not given its time.
func (s *Sender) sent(parent context.Context, report *usagestore.StoredReport) error {
	dayText := report.Day.Format(time.DateOnly)
	// The document is made of numbers and of values from closed lists: it is logged as it left.
	s.logger.InfoContext(parent, "the usage report of the day is sent", "day", dayText, "document", string(report.Document))

	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), s.recordTimeout)
	defer cancel()
	if err := s.store.MarkReportSent(ctx, report.Day, s.now()); err != nil {
		s.logger.WarnContext(parent, "the usage report of the day was sent and could not be marked as sent: it will be sent again",
			"day", dayText, "error", err)
		return fmt.Errorf("unable to record that the usage report of %s was sent: %w", dayText, err)
	}
	return nil
}

// logFailure tells of a report that could not be sent, with how many times it was tried when
// that can be read: the line is written all the same when it cannot.
func (s *Sender) logFailure(parent context.Context, day time.Time, failure error) {
	attributes := []any{"day", day.Format(time.DateOnly)}
	// The pass may have ended with the request: the reading has its own time.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), s.recordTimeout)
	defer cancel()
	if sendings, err := s.store.ListReportSendings(ctx, day, day); err == nil && len(sendings) == 1 {
		attributes = append(attributes, "attempts", sendings[0].Attempts)
	}
	s.logger.WarnContext(parent, "could not send the usage report of the day", append(attributes, "error", failure)...)
}
