package hooks

import (
	"context"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
)

// JobService is the logic of job hooks. Its methods are the job hook procedures of the job
// service of the contract.
type JobService struct {
	db   *husonymdb.HusonymDb
	gate gate
}

// NewJobService builds the logic of job hooks on the database of the API and on what tells
// who the caller is.
func NewJobService(db *husonymdb.HusonymDb, users userdata.Interface) *JobService {
	return &JobService{db: db, gate: gate{users: users}}
}

// job finds what a job id names. A job that does not exist gives a target without an owner.
func (s *JobService) job(ctx context.Context, id string, absent error) (target, pgtype.UUID, error) {
	jobID, err := husonymdb.ToUuid(id)
	if err != nil {
		return target{}, pgtype.UUID{}, invalidID("job")
	}
	t, err := s.ownerOfJob(ctx, jobID, absent)
	return t, jobID, err
}

func (s *JobService) ownerOfJob(ctx context.Context, jobID pgtype.UUID, absent error) (target, error) {
	accountID, err := s.db.Q.GetAccountIdFromJobId(ctx, s.db.Db, jobID)
	switch {
	case husonymdb.IsNoRows(err):
		return target{absent: absent}, nil
	case err != nil:
		return target{}, fmt.Errorf("unable to find the account of the job: %w", err)
	}
	return target{
		accountID: husonymdb.UUIDString(accountID),
		jobID:     husonymdb.UUIDString(jobID),
		absent:    absent,
	}, nil
}

// hook finds what a job hook id names, and the hook when there is one.
func (s *JobService) hook(ctx context.Context, id string, absent error) (target, *db_queries.HusonymApiJobHook, error) {
	hookID, err := husonymdb.ToUuid(id)
	if err != nil {
		return target{}, nil, invalidID("job hook")
	}
	row, err := s.db.Q.GetJobHookById(ctx, s.db.Db, hookID)
	switch {
	case husonymdb.IsNoRows(err):
		return target{absent: absent}, nil, nil
	case err != nil:
		return target{}, nil, fmt.Errorf("unable to find the job hook: %w", err)
	}
	t, err := s.ownerOfJob(ctx, row.JobID, absent)
	if err != nil {
		return target{}, nil, err
	}
	return t, &row, nil
}
