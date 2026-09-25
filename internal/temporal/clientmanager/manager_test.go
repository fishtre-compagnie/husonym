package clientmanager

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	temporalclient "go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
)

// fakeFactory hands out the clients of a test.
type fakeFactory struct {
	workflowClient  temporalclient.Client
	namespaceClient temporalclient.NamespaceClient
}

func (f *fakeFactory) CreateNamespaceClient(context.Context, *TemporalConfig, *slog.Logger) (temporalclient.NamespaceClient, error) {
	return f.namespaceClient, nil
}

func (f *fakeFactory) CreateWorkflowClient(context.Context, *TemporalConfig, *slog.Logger) (temporalclient.Client, error) {
	return f.workflowClient, nil
}

func newTestManager(t *testing.T) (*ClientManager, *temporalmocks.Client) {
	t.Helper()
	configs := NewMockConfigProvider(t)
	configs.EXPECT().GetConfig(mock.Anything, "account").
		Return(&TemporalConfig{Namespace: "default", SyncJobQueueName: "sync-job"}, nil)
	client := temporalmocks.NewClient(t)
	client.On("ScheduleClient").Return(temporalmocks.NewScheduleClient(t)).Maybe()
	return NewClientManager(configs, &fakeFactory{workflowClient: client, namespaceClient: temporalmocks.NewNamespaceClient(t)}), client
}

// Nobody polls the queue: the workflow is not started, since it would wait for a worker
// until it timed out.
func Test_RunWorkflow_NoWorker(t *testing.T) {
	manager, client := newTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{}, nil)

	var result string
	err := manager.RunWorkflow(context.Background(), "account", &temporalclient.StartWorkflowOptions{ID: "wf"},
		"Workflow", "arg", &result, slog.Default())
	require.ErrorIs(t, err, ErrNoWorker)
	client.AssertNotCalled(t, "ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A worker polls the queue: the workflow runs there, and its result comes back.
func Test_RunWorkflow_Result(t *testing.T) {
	manager, client := newTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{{Identity: "worker"}}}, nil)
	run := temporalmocks.NewWorkflowRun(t)
	run.On("Get", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		result, ok := args.Get(1).(*string)
		require.True(t, ok)
		*result = "done"
	}).Return(nil)
	client.On("ExecuteWorkflow", mock.Anything,
		mock.MatchedBy(func(opts temporalclient.StartWorkflowOptions) bool {
			return opts.ID == "wf" && opts.TaskQueue == "sync-job"
		}), "Workflow", "arg").Return(run, nil)

	var result string
	require.NoError(t, manager.RunWorkflow(context.Background(), "account",
		&temporalclient.StartWorkflowOptions{ID: "wf"}, "Workflow", "arg", &result, slog.Default()))
	require.Equal(t, "done", result)
}

// The workflow fails: so does the call.
func Test_RunWorkflow_Failure(t *testing.T) {
	manager, client := newTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{{Identity: "worker"}}}, nil)
	run := temporalmocks.NewWorkflowRun(t)
	run.On("Get", mock.Anything, mock.Anything).Return(errors.New("activity failed"))
	client.On("ExecuteWorkflow", mock.Anything, mock.Anything, "Workflow", "arg").Return(run, nil)

	var result string
	require.ErrorContains(t, manager.RunWorkflow(context.Background(), "account",
		&temporalclient.StartWorkflowOptions{ID: "wf"}, "Workflow", "arg", &result, slog.Default()), "activity failed")
}
