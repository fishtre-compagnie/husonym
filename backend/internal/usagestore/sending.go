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
	// Diagnostics is whether the document of the report carries the diagnostics.
	Diagnostics bool
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

// ReportClaim says which report is due.
type ReportClaim struct {
	// From and To are the first and the last day a report may be of.
	From, To time.Time
	// NotAttemptedSince leaves out a report that was tried at that moment or later.
	NotAttemptedSince time.Time
	// PreparedBy, when it is given, leaves out a report prepared after that moment.
	PreparedBy *time.Time
	// WithoutDiagnostics leaves out a report whose document carries the diagnostics.
	WithoutDiagnostics bool
	// At is the moment of the claim, which the attempt is dated of.
	At time.Time
}

// ClaimReport marks one more attempt on the oldest unsent report that is due, and returns it;
// nil when there is none. Two calls at once never get the same report.
func (s *Store) ClaimReport(
	ctx context.Context,
	claim ReportClaim, //nolint:gocritic // hugeParam: callers hand a value they do not share
) (*StoredReport, error) {
	var preparedBy pgtype.Timestamptz
	if claim.PreparedBy != nil {
		preparedBy = toTimestamptz(*claim.PreparedBy)
	}
	row, err := s.db.Q.ClaimUsageReport(ctx, s.db.Db, db_queries.ClaimUsageReportParams{
		Now:                toTimestamptz(claim.At),
		FromDay:            utcDate(claim.From),
		ToDay:              utcDate(claim.To),
		NotAttemptedSince:  toTimestamptz(claim.NotAttemptedSince),
		PreparedBy:         preparedBy,
		WithoutDiagnostics: claim.WithoutDiagnostics,
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
			Diagnostics:   row.CarriesDiagnostics,
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
