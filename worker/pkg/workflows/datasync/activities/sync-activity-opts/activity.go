package syncactivityopts_activity

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type Activity struct {
	jobclient mgmtv1alpha1connect.JobServiceClient
}

func New(
	jobclient mgmtv1alpha1connect.JobServiceClient,
) *Activity {
	return &Activity{
		jobclient: jobclient,
	}
}

type RetrieveActivityOptionsRequest struct {
	JobId string
}
type RetrieveActivityOptionsResponse struct {
	SyncActivityOptions  *workflow.ActivityOptions
	AccountId            string
	RequestedRecordCount *uint64
	Destinations         []*mgmtv1alpha1.JobDestination
	// RunTimeout is the time limit the job sets on a run, nil when it sets none. The workflow
	// stops the run there, and still puts the destination back in order.
	RunTimeout *time.Duration `json:",omitempty"`
}

func (a *Activity) RetrieveActivityOptions(
	ctx context.Context,
	req *RetrieveActivityOptionsRequest,
) (*RetrieveActivityOptionsResponse, error) {
	activityInfo := activity.GetInfo(ctx)
	logger := log.With(
		activity.GetLogger(ctx),
		"jobId", req.JobId,
		"WorkflowID", activityInfo.WorkflowExecution.ID,
		"RunID", activityInfo.WorkflowExecution.RunID,
	)
	logger.Debug("retrieving activity options")

	jobResp, err := a.jobclient.GetJob(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: req.JobId}),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to get job by id: %w", err)
	}
	job := jobResp.Msg.GetJob()
	return &RetrieveActivityOptionsResponse{
		SyncActivityOptions:  getSyncActivityOptionsFromJob(job),
		AccountId:            job.GetAccountId(),
		RequestedRecordCount: getRequestedRecordCount(job),
		Destinations:         job.GetDestinations(),
		RunTimeout:           getRunTimeout(job),
	}, nil
}

func getRunTimeout(job *mgmtv1alpha1.Job) *time.Duration {
	timeout := job.GetWorkflowOptions().GetRunTimeout()
	if timeout <= 0 {
		return nil
	}
	d := time.Duration(timeout)
	return &d
}

func getRequestedRecordCount(job *mgmtv1alpha1.Job) *uint64 {
	switch config := job.GetSource().GetOptions().GetConfig().(type) {
	case *mgmtv1alpha1.JobSourceOptions_AiGenerate:
		return zeroToNilPointer(getAiGeneratedRequestedCount(config.AiGenerate))
	case *mgmtv1alpha1.JobSourceOptions_Generate:
		return zeroToNilPointer(getGenerateRequestedCount(config.Generate))
	default:
		return nil
	}
}

func getAiGeneratedRequestedCount(config *mgmtv1alpha1.AiGenerateSourceOptions) uint64 {
	if config == nil {
		config = &mgmtv1alpha1.AiGenerateSourceOptions{}
	}
	total := uint64(0)
	for _, schema := range config.GetSchemas() {
		for _, table := range schema.GetTables() {
			count := table.GetRowCount()
			if count > 0 {
				total += uint64(count)
			}
		}
	}
	return total
}

func getGenerateRequestedCount(config *mgmtv1alpha1.GenerateSourceOptions) uint64 {
	if config == nil {
		config = &mgmtv1alpha1.GenerateSourceOptions{}
	}
	total := uint64(0)
	for _, schema := range config.GetSchemas() {
		for _, table := range schema.GetTables() {
			count := table.GetRowCount()
			if count > 0 {
				total += uint64(count)
			}
		}
	}
	return total
}

// if the input is less than or equal to 0, returns nil
func zeroToNilPointer[T uint64 | int64](value T) *T {
	if value <= 0 {
		return nil
	}
	return &value
}

// The options of the activity syncing a page of a table. They are the worker's own: the
// timeouts and attempts a job may still carry (JobSyncOptions) are no longer read.
//
// A page has no time limit of its own: it runs as long as it moves, which its heartbeats tell,
// and the time limit of the run bounds it (the workflow's). Temporal asks for a limit all the
// same: a day, far past any page.
//
// A page that fails on what may pass by itself, a connection lost or a deadlock, or whose
// worker dies, is attempted again, twice at most; one that fails on its query is not (see the
// sync activity).
const (
	pageStartToCloseTimeout = 24 * time.Hour
	pageHeartbeatTimeout    = 1 * time.Minute
	pageMaxAttempts         = 3
	pageFirstRetryAfter     = 30 * time.Second
)

func getSyncActivityOptionsFromJob(_ *mgmtv1alpha1.Job) *workflow.ActivityOptions {
	return &workflow.ActivityOptions{
		StartToCloseTimeout: pageStartToCloseTimeout,
		HeartbeatTimeout:    pageHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    pageFirstRetryAfter,
			BackoffCoefficient: 2,
			MaximumAttempts:    pageMaxAttempts,
		},
	}
}
