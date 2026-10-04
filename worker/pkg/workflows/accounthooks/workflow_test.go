package accounthooks

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/runevents"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

var testRun = runevents.Run{AccountID: "account-1", JobID: "job-1", RunID: "run-1"}

func testEvent() *runevents.Event {
	return testRun.Succeeded(time.Date(2026, time.October, 3, 7, 51, 53, 0, time.UTC))
}

// newWorkflowEnv returns a test environment in which the two activities are registered,
// for a test to mock.
func newWorkflowEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *Activities) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.SetTestTimeout(30 * time.Second)
	activities := NewActivities(nil, nil)
	env.RegisterActivity(activities.GetAccountHooksByEvent)
	env.RegisterActivity(activities.ExecuteAccountHook)
	return env, activities
}

func Test_ProcessAccountHook_ExecutesEachHookOfTheEvent(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	event := testEvent()

	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, &GetAccountHooksByEventRequest{
		AccountId: "account-1",
		EventName: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
	}).Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1", "hook-2"}}, nil).Once()
	for _, id := range []string{"hook-1", "hook-2"} {
		env.OnActivity(activities.ExecuteAccountHook, mock.Anything, &ExecuteAccountHookRequest{HookId: id, Event: event}).
			Return(&ExecuteAccountHookResponse{}, nil).Once()
	}

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: event})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var response *ProcessAccountHookResponse
	require.NoError(t, env.GetWorkflowResult(&response))
	require.Equal(t, &ProcessAccountHookResponse{}, response)
	env.AssertExpectations(t)
}

func Test_ProcessAccountHook_SucceedsWhenNoHookListens(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{}}, nil).Once()

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertNotCalled(t, "ExecuteAccountHook", mock.Anything, mock.Anything)
	env.AssertExpectations(t)
}

func Test_ProcessAccountHook_RefusesARequestWithoutEvent(t *testing.T) {
	for name, req := range map[string]*ProcessAccountHookRequest{
		"no request": nil,
		"no event":   {},
	} {
		t.Run(name, func(t *testing.T) {
			env, _ := newWorkflowEnv(t)

			env.ExecuteWorkflow(ProcessAccountHook, req)

			require.True(t, env.IsWorkflowCompleted())
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, env.GetWorkflowError(), &appErr)
			require.Equal(t, "event is required", appErr.Message())
			require.Equal(t, "MissingEvent", appErr.Type())
			require.True(t, appErr.NonRetryable())
			env.AssertNotCalled(t, "GetAccountHooksByEvent", mock.Anything, mock.Anything)
		})
	}
}

func Test_ProcessAccountHook_FailsWhenTheLookupFails(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	var lookups atomic.Int32
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(func(context.Context, *GetAccountHooksByEventRequest) (*GetAccountHooksByEventResponse, error) {
			lookups.Add(1)
			return nil, errors.New("the API is away")
		})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "the API is away")
	require.EqualValues(t, 3, lookups.Load(), "the lookup is attempted three times")
	env.AssertNotCalled(t, "ExecuteAccountHook", mock.Anything, mock.Anything)
}

func Test_ProcessAccountHook_AFailingHookFailsTheRunOnceTheOthersCompleted(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1", "hook-2", "hook-3"}}, nil).Once()

	var mu sync.Mutex
	completed := []string{}
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *ExecuteAccountHookRequest) (*ExecuteAccountHookResponse, error) {
			if req.HookId == "hook-1" {
				return nil, temporal.NewNonRetryableApplicationError("the receiver said no", "WebhookRejected", nil)
			}
			mu.Lock()
			defer mu.Unlock()
			completed = append(completed, req.HookId)
			return &ExecuteAccountHookResponse{}, nil
		})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	workflowErr := env.GetWorkflowError()
	require.Error(t, workflowErr)
	require.Contains(t, workflowErr.Error(), "error executing hooks: error executing hook: ")
	require.Contains(t, workflowErr.Error(), "the receiver said no")
	require.ElementsMatch(t, []string{"hook-2", "hook-3"}, completed)
}

func Test_ProcessAccountHook_NamesEachFailingHook(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1", "hook-2"}}, nil).Once()
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *ExecuteAccountHookRequest) (*ExecuteAccountHookResponse, error) {
			return nil, temporal.NewNonRetryableApplicationError("no for "+req.HookId, "WebhookRejected", nil)
		})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	text := env.GetWorkflowError().Error()
	require.True(t, strings.HasPrefix(unwrapWorkflowText(text), "error executing hooks: "), text)
	require.GreaterOrEqual(t, strings.Count(text, "error executing hook: "), 2)
	require.Contains(t, text, "no for hook-1")
	require.Contains(t, text, "no for hook-2")
}

// unwrapWorkflowText removes what the test environment writes before the error of a
// workflow.
func unwrapWorkflowText(text string) string {
	if at := strings.Index(text, "error executing hooks: "); at >= 0 {
		return text[at:]
	}
	return text
}

// Every hook is started before the first one is awaited: the first execution below only
// ends once the three of them run.
func Test_ProcessAccountHook_StartsEveryHookBeforeAwaitingAny(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1", "hook-2", "hook-3"}}, nil).Once()

	var running sync.WaitGroup
	running.Add(3)
	allRunning := make(chan struct{})
	go func() {
		running.Wait()
		close(allRunning)
	}()
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(func(ctx context.Context, _ *ExecuteAccountHookRequest) (*ExecuteAccountHookResponse, error) {
			running.Done()
			select {
			case <-allRunning:
				return &ExecuteAccountHookResponse{}, nil
			case <-time.After(10 * time.Second):
				return nil, temporal.NewNonRetryableApplicationError("the other hooks were not started", "Test", nil)
			}
		}).Times(3)

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func Test_ProcessAccountHook_SchedulesTheHooksInTheOrderOfTheLookup(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	ids := []string{"hook-b", "hook-a", "hook-c"}
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: ids}, nil).Once()
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(&ExecuteAccountHookResponse{}, nil).Times(3)

	// The activity id is the rank at which the workflow scheduled the activity.
	var mu sync.Mutex
	scheduled := map[string]string{}
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, args converter.EncodedValues) {
		if info.ActivityType.Name != "ExecuteAccountHook" {
			return
		}
		var req *ExecuteAccountHookRequest
		if err := args.Get(&req); err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		scheduled[info.ActivityID] = req.HookId
	})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string]string{"2": "hook-b", "3": "hook-a", "4": "hook-c"}, scheduled)
}

func Test_ProcessAccountHook_AttemptsADeliveryFiveTimes(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1"}}, nil).Once()
	var attempts atomic.Int32
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(func(context.Context, *ExecuteAccountHookRequest) (*ExecuteAccountHookResponse, error) {
			attempts.Add(1)
			return nil, errors.New("the receiver is away")
		})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "the receiver is away")
	require.EqualValues(t, 5, attempts.Load())
}

func Test_ProcessAccountHook_DoesNotRetryAPermanentFailure(t *testing.T) {
	env, activities := newWorkflowEnv(t)
	env.OnActivity(activities.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&GetAccountHooksByEventResponse{HookIds: []string{"hook-1"}}, nil).Once()
	var attempts atomic.Int32
	env.OnActivity(activities.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(func(context.Context, *ExecuteAccountHookRequest) (*ExecuteAccountHookResponse, error) {
			attempts.Add(1)
			return nil, temporal.NewNonRetryableApplicationError("the receiver said no", "WebhookRejected", nil)
		})

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{Event: testEvent()})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.EqualValues(t, 1, attempts.Load())
}
