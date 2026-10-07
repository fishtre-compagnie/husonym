package clientmanager

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	temporalclient "go.temporal.io/sdk/client"
	temporalmocks "go.temporal.io/sdk/mocks"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
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

// A worker stopped a while ago is still listed by the server: it serves nothing.
func Test_RunWorkflow_StoppedWorker(t *testing.T) {
	manager, client := newTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{{
			Identity: "stopped", LastAccessTime: timestamppb.New(time.Now().Add(-3 * time.Minute)),
		}}}, nil)

	var result string
	err := manager.RunWorkflow(context.Background(), "account", &temporalclient.StartWorkflowOptions{ID: "wf"},
		"Workflow", "arg", &result, slog.Default())
	require.ErrorIs(t, err, ErrNoWorker)
}

// The caller stops waiting: the error says so, whatever the wait became.
func Test_RunWorkflow_CallerGivesUp(t *testing.T) {
	manager, client := newTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{{Identity: "worker", LastAccessTime: timestamppb.Now()}}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	run := temporalmocks.NewWorkflowRun(t)
	run.On("Get", mock.Anything, mock.Anything).Run(func(mock.Arguments) { cancel() }).
		Return(errors.New("rpc error: code = DeadlineExceeded"))
	client.On("ExecuteWorkflow", mock.Anything, mock.Anything, "Workflow", "arg").Return(run, nil)

	var result string
	err := manager.RunWorkflow(ctx, "account", &temporalclient.StartWorkflowOptions{ID: "wf"},
		"Workflow", "arg", &result, slog.Default())
	require.ErrorIs(t, err, context.Canceled)
}

// A worker polls the queue: the workflow runs there, and its result comes back.
func Test_RunWorkflow_Result(t *testing.T) {
	manager, client := newTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{{Identity: "worker", LastAccessTime: timestamppb.Now()}}}, nil)
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
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{{Identity: "worker", LastAccessTime: timestamppb.Now()}}}, nil)
	run := temporalmocks.NewWorkflowRun(t)
	run.On("Get", mock.Anything, mock.Anything).Return(errors.New("activity failed"))
	client.On("ExecuteWorkflow", mock.Anything, mock.Anything, "Workflow", "arg").Return(run, nil)

	var result string
	require.ErrorContains(t, manager.RunWorkflow(context.Background(), "account",
		&temporalclient.StartWorkflowOptions{ID: "wf"}, "Workflow", "arg", &result, slog.Default()), "activity failed")
}

// A schedule the orchestrator refuses gives back the client it was asked with, as one it
// creates does.
func Test_CreateSchedule_GivesItsClientBack(t *testing.T) {
	for name, refusal := range map[string]error{"created": nil, "refused": errors.New("invalid cron expression")} {
		t.Run(name, func(t *testing.T) {
			configs := NewMockConfigProvider(t)
			configs.EXPECT().GetConfig(mock.Anything, "account").
				Return(&TemporalConfig{Namespace: "default", SyncJobQueueName: "sync-job"}, nil)
			schedules := temporalmocks.NewScheduleClient(t)
			handle := temporalmocks.NewScheduleHandle(t)
			handle.On("GetID").Return("schedule-1").Maybe()
			schedules.On("Create", mock.Anything, mock.Anything).Return(handle, refusal)
			client := temporalmocks.NewClient(t)
			client.On("ScheduleClient").Return(schedules)
			manager := NewClientManager(configs, &fakeFactory{workflowClient: client, namespaceClient: temporalmocks.NewNamespaceClient(t)})

			_, err := manager.CreateSchedule(
				context.Background(),
				"account",
				&temporalclient.ScheduleOptions{ID: "schedule-1"},
				slog.Default(),
			)
			require.Equal(t, refusal, err)

			held := 0
			for _, cached := range manager.clientCache.clients {
				held += cached.referenceCount
			}
			require.Zero(t, held, "the client asked with is still held")
		})
	}
}

// newDefaultTestManager gives a manager whose default configuration names the queue "sync-job",
// and which is never asked the configuration of an account.
func newDefaultTestManager(t *testing.T) (*ClientManager, *temporalmocks.Client) {
	t.Helper()
	configs := NewMockConfigProvider(t)
	configs.EXPECT().DefaultConfig().
		Return(&TemporalConfig{Namespace: "default", SyncJobQueueName: "sync-job"})
	client := temporalmocks.NewClient(t)
	client.On("ScheduleClient").Return(temporalmocks.NewScheduleClient(t)).Maybe()
	return NewClientManager(configs, &fakeFactory{workflowClient: client, namespaceClient: temporalmocks.NewNamespaceClient(t)}), client
}

// The workers of the default queue are the distinct identities that asked it for work lately: a
// worker polls with several pollers, and the server still lists one that stopped.
func Test_CountDefaultQueueWorkers(t *testing.T) {
	manager, client := newDefaultTestManager(t)
	fresh := timestamppb.Now()
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{Pollers: []*taskqueuepb.PollerInfo{
			{Identity: "worker-a", LastAccessTime: fresh},
			{Identity: "worker-a", LastAccessTime: fresh},
			{Identity: "worker-b", LastAccessTime: fresh},
			{Identity: "stopped", LastAccessTime: timestamppb.New(time.Now().Add(-3 * time.Minute))},
		}}, nil)

	workers, err := manager.CountDefaultQueueWorkers(context.Background(), slog.Default())
	require.NoError(t, err)
	require.Equal(t, 2, workers)
}

func Test_CountDefaultQueueWorkers_NoWorker(t *testing.T) {
	manager, client := newDefaultTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(&workflowservice.DescribeTaskQueueResponse{}, nil)

	workers, err := manager.CountDefaultQueueWorkers(context.Background(), slog.Default())
	require.NoError(t, err)
	require.Zero(t, workers)
}

func Test_CountDefaultQueueWorkers_Failure(t *testing.T) {
	manager, client := newDefaultTestManager(t)
	client.On("DescribeTaskQueue", mock.Anything, "sync-job", enums.TASK_QUEUE_TYPE_WORKFLOW).
		Return(nil, errors.New("unavailable"))

	_, err := manager.CountDefaultQueueWorkers(context.Background(), slog.Default())
	require.ErrorContains(t, err, "unavailable")
}

// fakeWorkflowService answers GetSystemInfo and nothing else.
type fakeWorkflowService struct {
	workflowservice.WorkflowServiceClient
	version string
	err     error
}

func (f *fakeWorkflowService) GetSystemInfo(
	context.Context, *workflowservice.GetSystemInfoRequest, ...grpc.CallOption,
) (*workflowservice.GetSystemInfoResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &workflowservice.GetSystemInfoResponse{ServerVersion: f.version}, nil
}

// The version is given as the server spells it: what the report keeps of it is not decided here.
func Test_DefaultServerVersion(t *testing.T) {
	manager, client := newDefaultTestManager(t)
	client.On("WorkflowService").Return(&fakeWorkflowService{version: "1.25.2"})

	version, err := manager.DefaultServerVersion(context.Background(), slog.Default())
	require.NoError(t, err)
	require.Equal(t, "1.25.2", version)
}

func Test_DefaultServerVersion_Failure(t *testing.T) {
	manager, client := newDefaultTestManager(t)
	client.On("WorkflowService").Return(&fakeWorkflowService{err: errors.New("unavailable")})

	_, err := manager.DefaultServerVersion(context.Background(), slog.Default())
	require.ErrorContains(t, err, "unavailable")
}
