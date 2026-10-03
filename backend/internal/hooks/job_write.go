package hooks

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// CreateJobHook stores a hook for a job.
func (s *JobService) CreateJobHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.CreateJobHookRequest],
) (*connect.Response[mgmtv1alpha1.CreateJobHookResponse], error) {
	t, jobID, err := s.job(ctx, req.Msg.GetJobId(), jobNotFound())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceCreateJobHookProcedure, t, intent{})
	if err != nil {
		return nil, err
	}

	hook := req.Msg.GetHook()
	checked, err := s.checkJobHook(ctx, jobID, hook.GetConfig(), hook.GetPriority())
	if err != nil {
		return nil, err
	}
	row, err := s.db.Q.CreateJobHook(ctx, s.db.Db, db_queries.CreateJobHookParams{
		Name:            hook.GetName(),
		Description:     hook.GetDescription(),
		JobID:           jobID,
		Config:          checked.config,
		CreatedByUserID: admitted.caller.PgId(),
		UpdatedByUserID: admitted.caller.PgId(),
		Enabled:         hook.GetEnabled(),
		Priority:        checked.priority,
	})
	switch {
	case nameTaken(err, jobHookNameConstraint):
		return nil, jobHookNameTaken(hook.GetName())
	case err != nil:
		return nil, fmt.Errorf("unable to create job hook: %w", err)
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Debug("job hook created", "hookId", husonymdb.UUIDString(row.ID), "hookName", row.Name)

	dto, err := toJobHook(ctx, &row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.CreateJobHookResponse{Hook: dto}), nil
}

// UpdateJobHook replaces the name, the description, the configuration, the priority and the
// state of a hook. The job of a hook does not change.
func (s *JobService) UpdateJobHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.UpdateJobHookRequest],
) (*connect.Response[mgmtv1alpha1.UpdateJobHookResponse], error) {
	t, current, err := s.hook(ctx, req.Msg.GetId(), jobHookNotFound())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceUpdateJobHookProcedure, t, intent{})
	if err != nil {
		return nil, err
	}

	checked, err := s.checkJobHook(ctx, current.JobID, req.Msg.GetConfig(), req.Msg.GetPriority())
	if err != nil {
		return nil, err
	}
	row, err := s.db.Q.UpdateJobHook(ctx, s.db.Db, db_queries.UpdateJobHookParams{
		Name:            req.Msg.GetName(),
		Description:     req.Msg.GetDescription(),
		Config:          checked.config,
		Enabled:         req.Msg.GetEnabled(),
		Priority:        checked.priority,
		UpdatedByUserID: admitted.caller.PgId(),
		ID:              current.ID,
	})
	switch {
	case husonymdb.IsNoRows(err):
		return nil, jobHookNotFound()
	case nameTaken(err, jobHookNameConstraint):
		return nil, jobHookNameTaken(req.Msg.GetName())
	case err != nil:
		return nil, fmt.Errorf("unable to update job hook: %w", err)
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Debug("job hook updated", "hookId", husonymdb.UUIDString(row.ID), "hookName", row.Name)

	dto, err := toJobHook(ctx, &row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.UpdateJobHookResponse{Hook: dto}), nil
}

// SetJobHookEnabled turns a hook on or off. What it asks of the caller depends on what is
// asked, not on the state the hook is in; nothing is written when that state is the one
// asked.
func (s *JobService) SetJobHookEnabled(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetJobHookEnabledRequest],
) (*connect.Response[mgmtv1alpha1.SetJobHookEnabledResponse], error) {
	t, current, err := s.hook(ctx, req.Msg.GetId(), jobHookNotFound())
	if err != nil {
		return nil, err
	}
	admitted, err := s.gate.admit(
		ctx, mgmtv1alpha1connect.JobServiceSetJobHookEnabledProcedure, t, intent{arming: req.Msg.GetEnabled()},
	)
	if err != nil {
		return nil, err
	}

	row := *current
	if current.Enabled != req.Msg.GetEnabled() {
		row, err = s.db.Q.SetJobHookEnabled(ctx, s.db.Db, db_queries.SetJobHookEnabledParams{
			Enabled:         req.Msg.GetEnabled(),
			UpdatedByUserID: admitted.caller.PgId(),
			ID:              current.ID,
		})
		switch {
		case husonymdb.IsNoRows(err):
			return nil, jobHookNotFound()
		case err != nil:
			return nil, fmt.Errorf("unable to turn the job hook on or off: %w", err)
		}
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).
			Debug("job hook turned on or off", "hookId", husonymdb.UUIDString(row.ID), "enabled", row.Enabled)
	}

	dto, err := toJobHook(ctx, &row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.SetJobHookEnabledResponse{Hook: dto}), nil
}

// DeleteJobHook removes a hook. With no hook to remove, it is done already.
func (s *JobService) DeleteJobHook(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.DeleteJobHookRequest],
) (*connect.Response[mgmtv1alpha1.DeleteJobHookResponse], error) {
	t, current, err := s.hook(ctx, req.Msg.GetId(), errNothingToRemove)
	if err != nil {
		return nil, err
	}
	_, err = s.gate.admit(ctx, mgmtv1alpha1connect.JobServiceDeleteJobHookProcedure, t, intent{})
	switch {
	case errors.Is(err, errNothingToRemove):
		return connect.NewResponse(&mgmtv1alpha1.DeleteJobHookResponse{}), nil
	case err != nil:
		return nil, err
	}

	if err := s.db.Q.RemoveJobHookById(ctx, s.db.Db, current.ID); err != nil {
		return nil, fmt.Errorf("unable to remove job hook: %w", err)
	}
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Debug("job hook removed", "hookId", husonymdb.UUIDString(current.ID), "hookName", current.Name)
	return connect.NewResponse(&mgmtv1alpha1.DeleteJobHookResponse{}), nil
}
