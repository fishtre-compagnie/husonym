package v1alpha1_jobservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	preflight_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/preflight/workflow"
	"github.com/google/uuid"
	temporalclient "go.temporal.io/sdk/client"
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
	if job.GetJobType().GetPiiDetect() != nil {
		return nil, husonymerrors.NewBadRequest("a PII detection job writes nothing: it has no pre-flight check")
	}

	ctx, cancel := context.WithTimeout(ctx, preflightTimeout)
	defer cancel()
	var result preflight_workflow.Response
	err = s.temporalmgr.RunWorkflow(
		ctx,
		job.GetAccountId(),
		&temporalclient.StartWorkflowOptions{
			ID:                       fmt.Sprintf("preflight-%s-%s", job.GetId(), uuid.NewString()),
			WorkflowExecutionTimeout: preflightTimeout,
		},
		preflight_workflow.New().JobPreflight,
		&preflight_workflow.Request{JobId: job.GetId()},
		&result,
		logger,
	)
	switch {
	case errors.Is(err, clientmanager.ErrNoWorker):
		return nil, husonymerrors.NewFailedPrecondition(
			"no worker serves this account: the pre-flight check runs on one, start it and check again")
	case errors.Is(err, context.DeadlineExceeded):
		return nil, connect.NewError(connect.CodeDeadlineExceeded,
			fmt.Errorf("the pre-flight check did not end within %s", preflightTimeout))
	case err != nil:
		return nil, fmt.Errorf("the pre-flight check failed: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.PreflightJobResponse{
		Report:    result.Report,
		CheckedAt: timestamppb.Now(),
	}), nil
}
