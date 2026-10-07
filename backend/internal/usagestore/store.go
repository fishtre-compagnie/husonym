// Package usagestore keeps the usage counters of the instance in its own database: the identity
// of the instance, one row per run, and the license refusals counted per day. Nothing a customer
// entered (a name, an error message) is ever written here.
package usagestore

import (
	"context"
	"fmt"
	"slices"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgtype"
)

// Status is where a run stands. The values are the ones the table allows.
type Status string

const (
	StatusRunning    Status = "running"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusCanceled   Status = "canceled"
	StatusTerminated Status = "terminated"
	StatusTimedOut   Status = "timed_out"
)

// JobKind is what a run does. The values are the ones the table allows.
type JobKind string

const (
	JobKindSync       JobKind = "sync"
	JobKindGenerate   JobKind = "generate"
	JobKindAiGenerate JobKind = "ai_generate"
	JobKindPiiDetect  JobKind = "pii_detect"
)

// RunStart is a run that has begun.
type RunStart struct {
	RunId     string
	AccountId string
	JobId     string
	Kind      JobKind
	StartedAt time.Time
}

// RunEnd is a run that has finished. Status is completed, failed or canceled.
type RunEnd struct {
	RunId     string
	AccountId string
	JobId     string
	Kind      JobKind
	StartedAt time.Time
	EndedAt   time.Time
	Status    Status

	RowsRead      int64
	RowsDiscarded int64
	Retries       int64
}

// OpenRun is a run that was started and has not been told to end.
type OpenRun struct {
	RunId     string
	AccountId string
	StartedAt time.Time
}

// Store keeps the usage counters of the instance.
type Store struct {
	db *husonymdb.HusonymDb
}

func New(db *husonymdb.HusonymDb) *Store {
	return &Store{db: db}
}

// InstanceId gives the identity of the instance, which never changes.
func (s *Store) InstanceId(ctx context.Context) (string, error) {
	id, err := s.db.Q.GetInstanceId(ctx, s.db.Db)
	if err != nil {
		return "", err
	}
	return husonymdb.UUIDString(id), nil
}

// RunStarted records a run that has begun. A run already recorded is left as it is.
func (s *Store) RunStarted(ctx context.Context, run RunStart) error { //nolint:gocritic // hugeParam: callers hand a value they do not share
	accountId, jobId, err := toUuids(run.AccountId, run.JobId)
	if err != nil {
		return err
	}
	return s.db.Q.InsertRunUsageStarted(ctx, s.db.Db, db_queries.InsertRunUsageStartedParams{
		RunID:     run.RunId,
		AccountID: accountId,
		JobID:     jobId,
		JobKind:   string(run.Kind),
		StartedAt: toTimestamptz(run.StartedAt),
	})
}

// RunEnded records the end of a run, creating the row when its start was never recorded. A run
// that already finished is left as it is: the first end told wins.
func (s *Store) RunEnded(ctx context.Context, run RunEnd) error { //nolint:gocritic // hugeParam: callers hand a value they do not share
	switch run.Status {
	case StatusCompleted, StatusFailed, StatusCanceled:
	default:
		return fmt.Errorf("a run cannot end with the status %q", run.Status)
	}
	accountId, jobId, err := toUuids(run.AccountId, run.JobId)
	if err != nil {
		return err
	}
	return s.db.Q.UpsertRunUsageEnded(ctx, s.db.Db, db_queries.UpsertRunUsageEndedParams{
		RunID:         run.RunId,
		AccountID:     accountId,
		JobID:         jobId,
		JobKind:       string(run.Kind),
		Status:        string(run.Status),
		StartedAt:     toTimestamptz(run.StartedAt),
		EndedAt:       toTimestamptz(run.EndedAt),
		RowsRead:      run.RowsRead,
		RowsDiscarded: run.RowsDiscarded,
		Retries:       run.Retries,
	})
}

// OpenRunsStartedBefore gives the runs still open that started before the given time.
func (s *Store) OpenRunsStartedBefore(ctx context.Context, before time.Time) ([]OpenRun, error) {
	rows, err := s.db.Q.ListOpenRunUsageStartedBefore(ctx, s.db.Db, toTimestamptz(before))
	if err != nil {
		return nil, err
	}
	runs := make([]OpenRun, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, OpenRun{
			RunId:     row.RunID,
			AccountId: husonymdb.UUIDString(row.AccountID),
			StartedAt: row.StartedAt.Time,
		})
	}
	return runs, nil
}

// Settle closes a run that is still open with the status the orchestrator reports. The end is
// nil when it is not known. A run that already finished is left as it is.
func (s *Store) Settle(ctx context.Context, runId string, status Status, endedAt *time.Time) error {
	ended := pgtype.Timestamptz{}
	if endedAt != nil {
		ended = toTimestamptz(*endedAt)
	}
	return s.db.Q.SettleRunUsage(ctx, s.db.Db, db_queries.SettleRunUsageParams{
		RunID:   runId,
		Status:  string(status),
		EndedAt: ended,
	})
}

// CountRefusal adds one to the count of each gate that refused the account, on the UTC day of
// the given time. A gate the license does not know is not counted, and a gate named twice is counted once.
func (s *Store) CountRefusal(ctx context.Context, accountId string, gates []license.Gate, at time.Time) error {
	known := knownGates(gates)
	if len(known) == 0 {
		return nil
	}
	account, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return fmt.Errorf("account id: %w", err)
	}
	utc := at.UTC()
	day := pgtype.Date{Time: time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
	for _, gate := range known {
		if err := s.db.Q.IncrementGateRefusal(ctx, s.db.Db, db_queries.IncrementGateRefusalParams{
			Day:       day,
			AccountID: account,
			Gate:      string(gate),
		}); err != nil {
			return err
		}
	}
	return nil
}

// knownGates keeps the gates the license defines, each once.
func knownGates(gates []license.Gate) []license.Gate {
	all := license.AllGates()
	known := make([]license.Gate, 0, len(gates))
	for _, gate := range gates {
		if slices.Contains(all, gate) && !slices.Contains(known, gate) {
			known = append(known, gate)
		}
	}
	return known
}

func toUuids(accountId, jobId string) (account, job pgtype.UUID, err error) {
	if account, err = husonymdb.ToUuid(accountId); err != nil {
		return account, job, fmt.Errorf("account id: %w", err)
	}
	if job, err = husonymdb.ToUuid(jobId); err != nil {
		return account, job, fmt.Errorf("job id: %w", err)
	}
	return account, job, nil
}

func toTimestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
