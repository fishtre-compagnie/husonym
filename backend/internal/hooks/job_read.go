package hooks

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

// GetJobHooks gives the hooks of a job, in the order they run: by priority, then by age.
func (s *JobService) GetJobHooks(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobHooksRequest],
) (*connect.Response[mgmtv1alpha1.GetJobHooksResponse], error) {
	t, jobID, err := s.job(ctx, req.Msg.GetJobId(), jobNotFound())
	if err != nil {
		return nil, err
	}
	if _, err := s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceGetJobHooksProcedure, t, intent{}); err != nil {
		return nil, err
	}
	rows, err := s.db.Q.GetJobHooksByJob(ctx, s.db.Db, jobID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the hooks of the job: %w", err)
	}
	hooks, err := toJobHooks(ctx, rows)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobHooksResponse{Hooks: hooks}), nil
}

// GetJobHook gives one hook.
func (s *JobService) GetJobHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobHookRequest],
) (*connect.Response[mgmtv1alpha1.GetJobHookResponse], error) {
	t, row, err := s.hook(ctx, req.Msg.GetId(), jobHookNotFound())
	if err != nil {
		return nil, err
	}
	if _, err := s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceGetJobHookProcedure, t, intent{}); err != nil {
		return nil, err
	}
	hook, err := toJobHook(ctx, row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetJobHookResponse{Hook: hook}), nil
}

// IsJobHookNameAvailable says whether no hook of the job has this name. It is a hint for
// whoever picks a name: only creating the hook tells for sure.
func (s *JobService) IsJobHookNameAvailable(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.IsJobHookNameAvailableRequest],
) (*connect.Response[mgmtv1alpha1.IsJobHookNameAvailableResponse], error) {
	t, jobID, err := s.job(ctx, req.Msg.GetJobId(), jobNotFound())
	if err != nil {
		return nil, err
	}
	if _, err := s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceIsJobHookNameAvailableProcedure, t, intent{}); err != nil {
		return nil, err
	}
	available, err := s.db.Q.IsJobHookNameAvailable(ctx, s.db.Db, db_queries.IsJobHookNameAvailableParams{
		JobID: jobID,
		Name:  req.Msg.GetName(),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to tell whether the job hook name is free: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.IsJobHookNameAvailableResponse{IsAvailable: available}), nil
}

// GetActiveJobHooksByTiming gives the hooks of a job that are on, for the timing asked or for
// both when none is, in the order they run: by priority, then by age. It is what a run asks
// before and after its sync.
func (s *JobService) GetActiveJobHooksByTiming(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetActiveJobHooksByTimingRequest],
) (*connect.Response[mgmtv1alpha1.GetActiveJobHooksByTimingResponse], error) {
	t, jobID, err := s.job(ctx, req.Msg.GetJobId(), jobNotFound())
	if err != nil {
		return nil, err
	}
	if _, err := s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceGetActiveJobHooksByTimingProcedure, t, intent{}); err != nil {
		return nil, err
	}

	var rows []db_queries.HusonymApiJobHook
	switch timing := req.Msg.GetTiming(); timing {
	case mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_UNSPECIFIED:
		rows, err = s.db.Q.GetActiveJobHooks(ctx, s.db.Db, jobID)
	case mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC:
		rows, err = s.db.Q.GetActivePreSyncJobHooks(ctx, s.db.Db, jobID)
	case mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC:
		rows, err = s.db.Q.GetActivePostSyncJobHooks(ctx, s.db.Db, jobID)
	default:
		return nil, husonymerrors.NewBadRequest(fmt.Sprintf("invalid hook timing: %d", timing))
	}
	if err != nil {
		return nil, fmt.Errorf("unable to list the active hooks of the job: %w", err)
	}
	hooks, err := toJobHooks(ctx, rows)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetActiveJobHooksByTimingResponse{Hooks: hooks}), nil
}
