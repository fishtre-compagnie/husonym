package accounthooks

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

// The names below are in the histories of the runs in flight and of the root workflows
// that start the workflow: a run recorded under another name could not continue.
func Test_Register_KeepsTheRegisteredNames(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	hooks := &fakeHooks{
		active: []*mgmtv1alpha1.AccountHook{{Id: "hook-1"}},
		hooks:  map[string]*mgmtv1alpha1.AccountHook{"hook-1": {Id: "hook-1", AccountId: "account-1"}},
	}
	Register(env, hooks, webhook.NewSender())

	var mu sync.Mutex
	var started []string
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		mu.Lock()
		defer mu.Unlock()
		started = append(started, info.ActivityType.Name)
	})

	env.ExecuteWorkflow("ProcessAccountHook", &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"GetAccountHooksByEvent", "ExecuteAccountHook"}, started)
}

// What the workflow asks of each activity: how long it may run, how it is retried.
func Test_ProcessAccountHook_ActivityOptions(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1"}}, nil)
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(&ExecuteAccountHookResponse{}, nil)

	var mu sync.Mutex
	options := map[string]activity.Info{}
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		mu.Lock()
		defer mu.Unlock()
		options[info.ActivityType.Name] = *info
	})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})
	require.NoError(t, env.GetWorkflowError())

	mu.Lock()
	defer mu.Unlock()
	lookup := options["GetAccountHooksByEvent"]
	require.Equal(t, time.Minute, lookup.StartToCloseTimeout)
	require.Zero(t, lookup.HeartbeatTimeout)

	execute := options["ExecuteAccountHook"]
	require.Equal(t, time.Minute, execute.StartToCloseTimeout)
	require.Zero(t, execute.HeartbeatTimeout)
}

// The test environment does not wait between two attempts as a server does: the intervals
// are read from the policy itself. With them the attempts of a delivery are 5, 15, 45 and
// 135 seconds apart.
func Test_RetryPolicies(t *testing.T) {
	require.Equal(t, &temporal.RetryPolicy{MaximumAttempts: 3}, lookupOptions().RetryPolicy)
	require.Equal(
		t,
		&temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 3, MaximumAttempts: 5},
		executeOptions().RetryPolicy,
	)
}

// The serialized form of the inputs and outputs is in the histories: these are its keys.
func Test_SerializedForms(t *testing.T) {
	const event = `{"name":3,"accountId":"account-1","timestamp":"2026-10-03T07:51:53Z","jobRunSucceeded":{"jobId":"job-1","jobRunId":"run-1"}}`
	tests := []struct {
		name  string
		value any
		empty any
		want  string
	}{
		{
			"the workflow's input",
			&ProcessAccountHookRequest{Event: testEvent()}, &ProcessAccountHookRequest{},
			`{"Event":` + event + `}`,
		},
		{"the workflow's input without event", &ProcessAccountHookRequest{}, &ProcessAccountHookRequest{}, `{"Event":null}`},
		{"the workflow's output", &ProcessAccountHookResponse{}, &ProcessAccountHookResponse{}, `{}`},
		{
			"the lookup's input",
			&GetAccountHooksByEventRequest{
				AccountId: "account-1",
				EventName: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
			},
			&GetAccountHooksByEventRequest{},
			`{"AccountId":"account-1","EventName":3}`,
		},
		{
			"the lookup's output",
			&GetAccountHooksByEventResponse{HookIds: []string{"hook-1", "hook-2"}}, &GetAccountHooksByEventResponse{},
			`{"HookIds":["hook-1","hook-2"]}`,
		},
		{
			"the execution's input",
			&ExecuteAccountHookRequest{HookId: "hook-1", Event: testEvent()}, &ExecuteAccountHookRequest{},
			`{"HookId":"hook-1","Event":` + event + `}`,
		},
		{"the execution's output", &ExecuteAccountHookResponse{}, &ExecuteAccountHookResponse{}, `{}`},
	}
	dc := converter.GetDefaultDataConverter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := dc.ToPayload(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.want, string(payload.GetData()))
			require.Equal(t, "json/plain", string(payload.GetMetadata()["encoding"]))

			require.NoError(t, dc.FromPayload(payload, tt.empty))
			require.Equal(t, tt.value, tt.empty)
		})
	}
}

// Every input and output recorded in the histories of earlier runs decodes into the types
// of the package, and encodes back to the bytes that were recorded.
func Test_RecordedPayloads_DecodeAndEncodeBack(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "accounthooks_replay", "testdata", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	dc := converter.GetDefaultDataConverter()
	roundTrip := func(t *testing.T, payloads *commonpb.Payloads, into any) {
		t.Helper()
		require.Len(t, payloads.GetPayloads(), 1)
		recorded := payloads.GetPayloads()[0]
		require.NoError(t, dc.FromPayload(recorded, into))
		encoded, err := dc.ToPayload(into)
		require.NoError(t, err)
		require.Equal(t, string(recorded.GetData()), string(encoded.GetData()))
	}

	seen := map[string]int{}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			reader, err := os.Open(file)
			require.NoError(t, err)
			defer reader.Close()
			history, err := client.HistoryFromJSON(reader, client.HistoryJSONOptions{})
			require.NoError(t, err)

			results := map[int64]any{} // by the id of the event that scheduled the activity
			for _, event := range history.GetEvents() {
				if started := event.GetWorkflowExecutionStartedEventAttributes(); started != nil {
					roundTrip(t, started.GetInput(), &ProcessAccountHookRequest{})
					seen["workflow input"]++
				}
				if completed := event.GetWorkflowExecutionCompletedEventAttributes(); completed != nil {
					roundTrip(t, completed.GetResult(), &ProcessAccountHookResponse{})
					seen["workflow output"]++
				}
				if scheduled := event.GetActivityTaskScheduledEventAttributes(); scheduled != nil {
					switch scheduled.GetActivityType().GetName() {
					case "GetAccountHooksByEvent":
						roundTrip(t, scheduled.GetInput(), &GetAccountHooksByEventRequest{})
						results[event.GetEventId()] = &GetAccountHooksByEventResponse{}
						seen["lookup input"]++
					case "ExecuteAccountHook":
						roundTrip(t, scheduled.GetInput(), &ExecuteAccountHookRequest{})
						results[event.GetEventId()] = &ExecuteAccountHookResponse{}
						seen["execution input"]++
					default:
						t.Fatalf("an activity of another type: %s", scheduled.GetActivityType().GetName())
					}
				}
				if completed := event.GetActivityTaskCompletedEventAttributes(); completed != nil {
					roundTrip(t, completed.GetResult(), results[completed.GetScheduledEventId()])
					seen["activity output"]++
				}
			}
		})
	}
	for _, kind := range []string{"workflow input", "workflow output", "lookup input", "execution input", "activity output"} {
		require.Positive(t, seen[kind], kind)
	}
}
