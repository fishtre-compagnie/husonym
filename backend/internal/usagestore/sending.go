package usagestore

import (
	"context"
	"errors"
	"fmt"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReportSending is what became of the report of a day.
type ReportSending struct {
	Day        time.Time
	PreparedAt time.Time
	// SentAt is nil while the report has not been sent; LastAttemptAt is nil while none was tried.
	SentAt, LastAttemptAt *time.Time
	Attempts              int32
}

// SendingSince gives since when the instance sends its usage report, nil when it does not.
func (s *Store) SendingSince(ctx context.Context) (*time.Time, error) {
	since, err := s.db.Q.GetSendingSince(ctx, s.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to read since when the report is sent: %w", err)
	}
	return optionalTime(since), nil
}

// StartSending records that the instance sends its usage report from the given time. An instance
// that already sends keeps its first date.
func (s *Store) StartSending(ctx context.Context, at time.Time) error {
	return s.db.Q.StartUsageSending(ctx, s.db.Db, toTimestamptz(at))
}

// StopSending records that the instance does not send its usage report.
func (s *Store) StopSending(ctx context.Context) error {
	return s.db.Q.StopUsageSending(ctx, s.db.Db)
}

// ClaimReport marks one more attempt on the oldest unsent report whose day is in [from, to] and
// that was not attempted since notAttemptedSince, and returns it; nil when there is none. Two
// calls at once never get the same report.
func (s *Store) ClaimReport(ctx context.Context, from, to, notAttemptedSince, now time.Time) (*StoredReport, error) {
	row, err := s.db.Q.ClaimUsageReport(ctx, s.db.Db, db_queries.ClaimUsageReportParams{
		Now:               toTimestamptz(now),
		FromDay:           utcDate(from),
		ToDay:             utcDate(to),
		NotAttemptedSince: toTimestamptz(notAttemptedSince),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // no report due is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("unable to claim a report to send: %w", err)
	}
	return &StoredReport{
		Day:            row.Day.Time,
		Document:       []byte(row.Document),
		Seal:           row.Seal,
		KeyFingerprint: row.KeyFingerprint,
		PreparedAt:     row.PreparedAt.Time,
	}, nil
}

// MarkReportSent records that the report of a day was sent. A report already sent keeps the
// date it was sent on.
func (s *Store) MarkReportSent(ctx context.Context, day, at time.Time) error {
	return s.db.Q.MarkUsageReportSent(ctx, s.db.Db, db_queries.MarkUsageReportSentParams{
		Day:    utcDate(day),
		SentAt: toTimestamptz(at),
	})
}

// ListReportSendings gives what became of the reports of the days in [from, to], the newest first.
func (s *Store) ListReportSendings(ctx context.Context, from, to time.Time) ([]ReportSending, error) {
	rows, err := s.db.Q.ListUsageReportSendings(ctx, s.db.Db, db_queries.ListUsageReportSendingsParams{
		Day:   utcDate(from),
		Day_2: utcDate(to),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the reports sent: %w", err)
	}
	sendings := make([]ReportSending, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		sendings = append(sendings, ReportSending{
			Day:           row.Day.Time,
			PreparedAt:    row.PreparedAt.Time,
			SentAt:        optionalTime(row.SentAt),
			LastAttemptAt: optionalTime(row.LastAttemptAt),
			Attempts:      row.Attempts,
		})
	}
	return sendings, nil
}

// LastSentAt gives when a report was last sent, nil when none ever was.
func (s *Store) LastSentAt(ctx context.Context) (*time.Time, error) {
	at, err := s.db.Q.GetLastUsageReportSentAt(ctx, s.db.Db)
	if err != nil {
		return nil, fmt.Errorf("unable to read when a report was last sent: %w", err)
	}
	return optionalTime(at), nil
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
