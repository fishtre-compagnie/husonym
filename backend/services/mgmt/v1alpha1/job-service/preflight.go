package v1alpha1_jobservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	preflight_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/preflight/workflow"
	"go.temporal.io/api/enums/v1"
	temporalclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// preflightTimeout bounds the pre-flight check of a job: the plan of a run is computed from
// the schemas alone, and the connections asked what their roles need, which takes seconds.
const preflightTimeout = 3 * time.Minute

// PreflightJob tells what a run of the job would meet, before any run. The check runs on a
// worker, as a workflow of its own: only the worker computes the plan of a run, and only it
// knows the engine a job without one runs on. Nothing is read from the tables nor written.
func (s *Service) PreflightJob(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.PreflightJobRequest],
) (*connect.Response[mgmtv1alpha1.PreflightJobResponse], error) {
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	logger = logger.With("jobId", req.Msg.GetJobId())
	jobResp, err := s.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: req.Msg.GetJobId()}))
	if err != nil {
		return nil, err
	}
	job := jobResp.Msg.GetJob()
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceJob(ctx, userdata.NewDomainEntity(job.GetAccountId(), job.GetId()), rbac.JobAction_View); err != nil {
		return nil, err
	}
	// The worker logs in to each connection of the job with what it stores: asked of those
	// who may see it, as every other call that opens a connection is.
	connectionIds := []string{}
	sourceId, err := getJobSourceConnectionId(job.GetSource())
	if err != nil {
		return nil, err
	}
	if sourceId != nil && *sourceId != "" {
		connectionIds = append(connectionIds, *sourceId)
	}
	for _, destination := range job.GetDestinations() {
		connectionIds = append(connectionIds, destination.GetConnectionId())
	}
	for _, connectionId := range connectionIds {
		entity := userdata.NewDomainEntity(job.GetAccountId(), connectionId)
		if err := user.EnforceConnection(ctx, entity, rbac.ConnectionAction_ViewSensitive); err != nil {
			return nil, err
		}
	}
	if job.GetJobType().GetPiiDetect() != nil {
		return nil, husonymerrors.NewBadRequest("a PII detection job writes nothing: it has no pre-flight check")
	}

	checkCtx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()
	var result preflight_workflow.Response
	err = s.temporalmgr.RunWorkflow(
		checkCtx,
		job.GetAccountId(),
		&temporalclient.StartWorkflowOptions{
			// One check of a job at a time: a call made while one runs waits for it.
			ID:                       preflight_workflow.WorkflowId(job.GetId()),
			WorkflowIDConflictPolicy: enums.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING,
			WorkflowExecutionTimeout: preflightTimeout,
		},
		preflight_workflow.New().JobPreflight,
		&preflight_workflow.Request{JobId: job.GetId()},
		&result,
		logger,
	)
	if err != nil {
		return nil, preflightError(checkCtx, err, logger)
	}
	return connect.NewResponse(&mgmtv1alpha1.PreflightJobResponse{
		Report:    result.Report,
		CheckedAt: timestamppb.Now(),
	}), nil
}

// preflightError says why a check did not end, without the chain of the workflow: the
// identifiers of the workflow and the worker are for the logs, the reason for the caller.
func preflightError(ctx context.Context, err error, logger *slog.Logger) error {
	logger.Warn("the pre-flight check did not end", "error", err)
	var timeout *temporal.TimeoutError
	var failure *temporal.ApplicationError
	switch {
	case errors.Is(err, clientmanager.ErrNoWorker):
		return husonymerrors.NewFailedPrecondition(
			"no worker serves this account: the pre-flight check runs on one, start it and check again")
	case ctx.Err() != nil, errors.As(err, &timeout):
		return connect.NewError(connect.CodeDeadlineExceeded,
			fmt.Errorf("the pre-flight check did not end within %s", preflightTimeout))
	case errors.As(err, &failure):
		return connect.NewError(connect.CodeUnavailable,
			fmt.Errorf("the pre-flight check could not end: %s", failure.Message()))
	default:
		return husonymerrors.NewInternalError("the pre-flight check could not end")
	}
}
