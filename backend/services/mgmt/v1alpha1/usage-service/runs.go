package v1alpha1_usageservice

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// RecordRunStarted keeps a run that has begun, with the account and the kind of its job. The
// worker only knows the job: what the job is, is read here.
//
// A job that is not there is a job deleted since the run began, and nothing is kept for it.
func (s *Service) RecordRunStarted(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.RecordRunStartedRequest],
) (*connect.Response[mgmtv1alpha1.RecordRunStartedResponse], error) {
	if err := s.allow(ctx); err != nil {
		return nil, err
	}
	job, found, err := s.jobOf(ctx, req.Msg.GetJobId())
	if err != nil || !found {
		return connect.NewResponse(&mgmtv1alpha1.RecordRunStartedResponse{}), err
	}

	err = s.store.RunStarted(ctx, usagestore.RunStart{
		RunId:     req.Msg.GetRunId(),
		AccountId: husonymdb.UUIDString(job.AccountID),
		JobId:     req.Msg.GetJobId(),
		Kind:      kindOf(&job),
		StartedAt: req.Msg.GetStartedAt().AsTime(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to keep the start of the run: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.RecordRunStartedResponse{}), nil
}

// RecordRunEnded keeps a run that has ended. The row is created when the start was never told,
// unless the job is gone: the row of such a run is closed if it exists, and not created.
func (s *Service) RecordRunEnded(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.RecordRunEndedRequest],
) (*connect.Response[mgmtv1alpha1.RecordRunEndedResponse], error) {
	if err := s.allow(ctx); err != nil {
		return nil, err
	}
	status, ok := statusOf(req.Msg.GetOutcome())
	if !ok {
		return nil, husonymerrors.NewBadRequest("the outcome of the run is not one the service knows")
	}
	job, found, err := s.jobOf(ctx, req.Msg.GetJobId())
	if err != nil {
		return nil, err
	}
	if !found {
		// The job was deleted while the run ran. The account and the kind are not known, so no
		// row is created, but a row already there is closed.
		err = s.store.CloseRun(
			ctx, req.Msg.GetRunId(), status, req.Msg.GetEndedAt().AsTime(),
			req.Msg.GetRowsRead(), req.Msg.GetRowsDiscarded(), req.Msg.GetRetries(), 0, "",
		)
		if err != nil {
			return nil, fmt.Errorf("unable to close the run of a job that is gone: %w", err)
		}
		return connect.NewResponse(&mgmtv1alpha1.RecordRunEndedResponse{}), nil
	}

	err = s.store.RunEnded(ctx, usagestore.RunEnd{
		RunId:         req.Msg.GetRunId(),
		AccountId:     husonymdb.UUIDString(job.AccountID),
		JobId:         req.Msg.GetJobId(),
		Kind:          kindOf(&job),
		StartedAt:     req.Msg.GetStartedAt().AsTime(),
		EndedAt:       req.Msg.GetEndedAt().AsTime(),
		Status:        status,
		RowsRead:      req.Msg.GetRowsRead(),
		RowsDiscarded: req.Msg.GetRowsDiscarded(),
		Retries:       req.Msg.GetRetries(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to keep the end of the run: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.RecordRunEndedResponse{}), nil
}

// allow lets only the worker through.
func (s *Service) allow(ctx context.Context) error {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return err
	}
	return s.cfg.WorkerOnly.Allow(user)
}

// jobOf reads the job a run belongs to. It says so when there is none.
func (s *Service) jobOf(ctx context.Context, jobId string) (db_queries.HusonymApiJob, bool, error) {
	jobUuid, err := husonymdb.ToUuid(jobId)
	if err != nil {
		return db_queries.HusonymApiJob{}, false, husonymerrors.NewBadRequest("the job id is not a uuid")
	}
	job, err := s.db.Q.GetJobById(ctx, s.db.Db, jobUuid)
	if husonymdb.IsNoRows(err) {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).InfoContext(
			ctx,
			"the job of a run is gone",
			"jobId", jobId,
		)
		return db_queries.HusonymApiJob{}, false, nil
	}
	if err != nil {
		return db_queries.HusonymApiJob{}, false, fmt.Errorf("unable to read the job of the run: %w", err)
	}
	return job, true, nil
}

// kindOf says what the job does: detect PII, generate from the options of its source, and
// otherwise synchronize. A job whose type cannot be read is a synchronization, as the license
// counts it.
func kindOf(job *db_queries.HusonymApiJob) usagestore.JobKind {
	config := &mgmtv1alpha1.JobTypeConfig{}
	if len(job.JobtypeConfig) > 0 {
		if err := json.Unmarshal(job.JobtypeConfig, config); err == nil && config.GetPiiDetect() != nil {
			return usagestore.JobKindPiiDetect
		}
	}
	switch options := job.ConnectionOptions; {
	case options == nil:
		return usagestore.JobKindSync
	case options.AiGenerateOptions != nil:
		return usagestore.JobKindAiGenerate
	case options.GenerateOptions != nil:
		return usagestore.JobKindGenerate
	}
	return usagestore.JobKindSync
}

func statusOf(outcome mgmtv1alpha1.RunOutcome) (usagestore.Status, bool) {
	switch outcome {
	case mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED:
		return usagestore.StatusCompleted, true
	case mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED:
		return usagestore.StatusFailed, true
	case mgmtv1alpha1.RunOutcome_RUN_OUTCOME_CANCELED:
		return usagestore.StatusCanceled, true
	case mgmtv1alpha1.RunOutcome_RUN_OUTCOME_UNSPECIFIED:
		return "", false
	}
	return "", false
}
