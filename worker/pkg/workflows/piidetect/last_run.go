package piidetect

import (
	"context"
	"encoding/json"
	"slices"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
)

type GetLastSuccessfulWorkflowIdRequest struct {
	AccountId string
	JobId     string
}

type GetLastSuccessfulWorkflowIdResponse struct {
	// WorkflowId is the id of the run to start from, nil when there is none.
	WorkflowId *string
}

// GetLastSuccessfulWorkflowId looks, among the recent runs of the schedule of the job,
// for the latest one that stored an index naming a table at least: an incremental run
// starts from it.
//
// It never fails for what it could not look up. Without an answer every table is
// scanned, which costs time and misses nothing.
func (a *Activities) GetLastSuccessfulWorkflowId(
	ctx context.Context,
	req *GetLastSuccessfulWorkflowIdRequest,
) (*GetLastSuccessfulWorkflowIdResponse, error) {
	logger := activity.GetLogger(ctx)
	const fullRun = "the earlier run of the job could not be looked up: every table will be scanned"

	description, err := a.schedules.GetHandle(ctx, req.JobId).Describe(ctx)
	if err != nil {
		logger.Warn(fullRun, "jobId", req.JobId, "error", err)
		return &GetLastSuccessfulWorkflowIdResponse{}, nil
	}

	actions := slices.Clone(description.Info.RecentActions)
	slices.SortStableFunc(actions, func(a, b client.ScheduleActionResult) int {
		return b.ActualTime.Compare(a.ActualTime)
	})
	for _, action := range actions {
		// An action may have started no workflow.
		if action.StartWorkflowResult == nil {
			continue
		}
		workflowId := action.StartWorkflowResult.WorkflowID
		stored, err := a.jobs.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
			Id: &mgmtv1alpha1.RunContextKey{
				AccountId:  req.AccountId,
				JobRunId:   workflowId,
				ExternalId: report.JobReportExternalId(req.JobId),
			},
		}))
		if connect.CodeOf(err) == connect.CodeNotFound {
			continue
		}
		if err != nil {
			logger.Warn(fullRun, "jobId", req.JobId, "error", err)
			return &GetLastSuccessfulWorkflowIdResponse{}, nil
		}
		var index report.JobReport
		if err := json.Unmarshal(stored.Msg.GetValue(), &index); err != nil {
			logger.Warn(fullRun, "jobId", req.JobId, "error", err)
			return &GetLastSuccessfulWorkflowIdResponse{}, nil
		}
		if len(index.SuccessfulTableReports) > 0 {
			return &GetLastSuccessfulWorkflowIdResponse{WorkflowId: &workflowId}, nil
		}
	}
	return &GetLastSuccessfulWorkflowIdResponse{}, nil
}
