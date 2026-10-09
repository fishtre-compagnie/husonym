package usagestore

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5"
)

// RunCount is how many runs of a kind ended with a status.
type RunCount struct {
	Kind   JobKind
	Status Status
	Count  int64
}

// DayRuns is what the runs counted on a UTC day add up to. A run counts for the day on which
// the API recorded its end, whichever way it learned of it (the end the worker told, or the
// settling of a run that told none): the runs of a day are closed at midnight, and nothing
// recorded later belongs to it. A run still running counts for no day. The runs of several days
// add up the same way: RunsBetween gives them as one DayRuns.
type DayRuns struct {
	ByStatus []RunCount
	// Durations are in seconds, from the start of a run to its end, and never negative. They
	// are nil when no run of the day has an end.
	DurationMedian, DurationP95      *int64
	RowsRead, RowsDiscarded, Retries int64
	// WithUncountedRows is how many runs had a table that reported no row count.
	WithUncountedRows int64
}

// SourceEngineRuns is how many runs of the day a job made on a source of one major version.
type SourceEngineRuns struct {
	JobId        string
	VersionMajor string
	Runs         int64
}

// ErrorCount is how many runs did not complete for a category of error, at a step.
type ErrorCount struct {
	Category ErrorCategory
	Step     ErrorStep
	Count    int64
}

// GateCount is how many times a gate refused, over every account.
type GateCount struct {
	Gate  license.Gate
	Count int64
}

// StoredReport is the usage report of a day, kept as it was prepared.
type StoredReport struct {
	Day time.Time
	// Document is the exact JSON, byte for byte.
	Document       []byte
	Seal           string
	KeyFingerprint string
	PreparedAt     time.Time
}

// RunsOfDay adds up the runs counted on the UTC day of the given time.
func (s *Store) RunsOfDay(ctx context.Context, day time.Time) (*DayRuns, error) {
	return s.RunsBetween(ctx, day, day.UTC().AddDate(0, 0, 1))
}

// RunsBetween adds up the runs counted on the UTC days from the day of from to the day before the
// one of before: the first days of two months give the runs of a month. The durations are the
// median and the 95th percentile over all those days.
func (s *Store) RunsBetween(ctx context.Context, from, before time.Time) (*DayRuns, error) {
	fromDay, beforeDay := utcDate(from), utcDate(before)
	byStatus, err := s.db.Q.CountRunUsageByStatusBetween(ctx, s.db.Db, db_queries.CountRunUsageByStatusBetweenParams{
		FromDay: fromDay, BeforeDay: beforeDay,
	})
	if err != nil {
		return nil, err
	}
	sums, err := s.db.Q.SumRunUsageBetween(ctx, s.db.Db, db_queries.SumRunUsageBetweenParams{
		FromDay: fromDay, BeforeDay: beforeDay,
	})
	if err != nil {
		return nil, err
	}
	runs := &DayRuns{
		ByStatus:          make([]RunCount, 0, len(byStatus)),
		RowsRead:          sums.RowsRead,
		RowsDiscarded:     sums.RowsDiscarded,
		Retries:           sums.Retries,
		WithUncountedRows: sums.WithUncountedRows,
	}
	for _, row := range byStatus {
		runs.ByStatus = append(runs.ByStatus, RunCount{Kind: JobKind(row.JobKind), Status: Status(row.Status), Count: row.Runs})
	}
	if sums.RunsWithEnd > 0 {
		runs.DurationMedian, runs.DurationP95 = &sums.DurationMedian, &sums.DurationP95
	}
	return runs, nil
}

// SourceVersionsOfDay gives, per job and major version, the runs of the day that knew the version
// of their source.
func (s *Store) SourceVersionsOfDay(ctx context.Context, day time.Time) ([]SourceEngineRuns, error) {
	rows, err := s.db.Q.CountRunUsageBySourceVersionOfDay(ctx, s.db.Db, utcDate(day))
	if err != nil {
		return nil, err
	}
	versions := make([]SourceEngineRuns, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, SourceEngineRuns{
			JobId:        husonymdb.UUIDString(row.JobID),
			VersionMajor: row.SourceVersionMajor.String,
			Runs:         row.Runs,
		})
	}
	return versions, nil
}

// ErrorsOfDay gives, per category and step, the runs counted on the UTC day that did not complete.
// They are the runs RunsOfDay counts for that day, each once.
func (s *Store) ErrorsOfDay(ctx context.Context, day time.Time) ([]ErrorCount, error) {
	rows, err := s.db.Q.CountRunUsageErrorsOfDay(ctx, s.db.Db, utcDate(day))
	if err != nil {
		return nil, err
	}
	counted := make(map[RunError]int64, len(rows))
	for _, row := range rows {
		// Rows of several statuses, and rows that hold no pair, meet under one pair.
		if pair := errorRead(Status(row.Status), row.ErrorCategory, row.ErrorStep); pair != (RunError{}) {
			counted[pair] += row.Runs
		}
	}
	counts := make([]ErrorCount, 0, len(counted))
	for pair, count := range counted {
		counts = append(counts, ErrorCount{Category: pair.Category, Step: pair.Step, Count: count})
	}
	slices.SortFunc(counts, func(a, b ErrorCount) int {
		return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.Step, b.Step))
	})
	return counts, nil
}

// RefusalsOfDay adds up, over every account, the refusals of each gate on the UTC day.
func (s *Store) RefusalsOfDay(ctx context.Context, day time.Time) ([]GateCount, error) {
	return s.RefusalsBetween(ctx, day, day.UTC().AddDate(0, 0, 1))
}

// RefusalsBetween adds up, over every account, the refusals of each gate on the UTC days from the
// day of from to the day before the one of before.
func (s *Store) RefusalsBetween(ctx context.Context, from, before time.Time) ([]GateCount, error) {
	rows, err := s.db.Q.SumGateRefusalsBetween(ctx, s.db.Db, db_queries.SumGateRefusalsBetweenParams{
		FromDay: utcDate(from), BeforeDay: utcDate(before),
	})
	if err != nil {
		return nil, err
	}
	counts := make([]GateCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, GateCount{Gate: license.Gate(row.Gate), Count: row.Refusals})
	}
	return counts, nil
}

// SaveReport keeps the report of a day. A day that already has one is left as it is: saved tells
// whether the stored report is the one of this call.
func (s *Store) SaveReport(
	ctx context.Context,
	report StoredReport, //nolint:gocritic // hugeParam: callers hand a value they do not share
) (saved bool, err error) {
	rows, err := s.db.Q.InsertUsageReport(ctx, s.db.Db, db_queries.InsertUsageReportParams{
		Day:            utcDate(report.Day),
		Document:       string(report.Document),
		Seal:           report.Seal,
		KeyFingerprint: report.KeyFingerprint,
		PreparedAt:     toTimestamptz(report.PreparedAt),
	})
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

// Report gives the report kept for a day, or nil when there is none.
func (s *Store) Report(ctx context.Context, day time.Time) (*StoredReport, error) {
	row, err := s.db.Q.GetUsageReport(ctx, s.db.Db, utcDate(day))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // no report is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read the report of the day: %w", err)
	}
	return &StoredReport{
		Day:            row.Day.Time,
		Document:       []byte(row.Document),
		Seal:           row.Seal,
		KeyFingerprint: row.KeyFingerprint,
		PreparedAt:     row.PreparedAt.Time,
	}, nil
}

// ReportsBetween gives the reports kept for the UTC days from the day of from to the day before
// the one of before, the oldest first.
func (s *Store) ReportsBetween(ctx context.Context, from, before time.Time) ([]StoredReport, error) {
	rows, err := s.db.Q.ListUsageReportsBetween(ctx, s.db.Db, db_queries.ListUsageReportsBetweenParams{
		FromDay: utcDate(from), BeforeDay: utcDate(before),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to read the reports of the days: %w", err)
	}
	reports := make([]StoredReport, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		reports = append(reports, StoredReport{
			Day:            row.Day.Time,
			Document:       []byte(row.Document),
			Seal:           row.Seal,
			KeyFingerprint: row.KeyFingerprint,
			PreparedAt:     row.PreparedAt.Time,
		})
	}
	return reports, nil
}

// DeleteReportsBefore drops the reports of the days before the given UTC day.
func (s *Store) DeleteReportsBefore(ctx context.Context, day time.Time) error {
	return s.db.Q.DeleteUsageReportsBefore(ctx, s.db.Db, utcDate(day))
}
