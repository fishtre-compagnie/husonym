package accounthook_workflow

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/runevents"
	execute_hook_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/account_hooks/activities/execute"
	hooks_by_event_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/account_hooks/activities/hooks-by-event"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func Test_ProcessAccountHook(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	var hooksByEventActivity *hooks_by_event_activity.Activity

	env.OnActivity(hooksByEventActivity.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&hooks_by_event_activity.RunHooksByEventResponse{
			HookIds: []string{"hook1", "hook2"},
		}, nil).Once()

	var executeHookActivity *execute_hook_activity.Activity
	env.OnActivity(executeHookActivity.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(&execute_hook_activity.ExecuteHookResponse{}, nil).Twice()

	env.RegisterWorkflow(ProcessAccountHook)

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{
		Event: runevents.Run{AccountID: "123", JobID: "456", RunID: "789"}.Created(time.Now()),
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result *ProcessAccountHookResponse
	err := env.GetWorkflowResult(&result)
	require.NoError(t, err)
	require.Equal(t, &ProcessAccountHookResponse{}, result)

	env.AssertExpectations(t)
}

// Without an event there is nothing to tell the hooks: the workflow fails at once, and
// asks for no activity.
func Test_ProcessAccountHook_NilEvent(t *testing.T) {
	requests := map[string]*ProcessAccountHookRequest{
		"no request": nil,
		"no event":   {},
	}
	for name, req := range requests {
		t.Run(name, func(t *testing.T) {
			var ts testsuite.WorkflowTestSuite
			env := ts.NewTestWorkflowEnvironment()

			var hooksByEventActivity *hooks_by_event_activity.Activity
			env.OnActivity(hooksByEventActivity.GetAccountHooksByEvent, mock.Anything, mock.Anything).
				Return(&hooks_by_event_activity.RunHooksByEventResponse{}, nil).Never()

			env.RegisterWorkflow(ProcessAccountHook)

			env.ExecuteWorkflow(ProcessAccountHook, req)

			require.True(t, env.IsWorkflowCompleted())
			workflowErr := env.GetWorkflowError()
			require.Error(t, workflowErr)
			require.Contains(t, workflowErr.Error(), "event is required")
			var applicationErr *temporal.ApplicationError
			require.ErrorAs(t, workflowErr, &applicationErr)
			require.True(t, applicationErr.NonRetryable())

			env.AssertExpectations(t)
		})
	}
}

func Test_ProcessAccountHook_Error(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	var hooksByEventActivity *hooks_by_event_activity.Activity
	env.OnActivity(hooksByEventActivity.GetAccountHooksByEvent, mock.Anything, mock.Anything).
		Return(&hooks_by_event_activity.RunHooksByEventResponse{
			HookIds: []string{"hook1", "hook2"},
		}, nil).Once()

	var executeHookActivity *execute_hook_activity.Activity
	env.OnActivity(executeHookActivity.ExecuteAccountHook, mock.Anything, mock.Anything).
		Return(nil, errors.New("error"))

	env.RegisterWorkflow(ProcessAccountHook)

	env.ExecuteWorkflow(ProcessAccountHook, &ProcessAccountHookRequest{
		Event: runevents.Run{AccountID: "123", JobID: "456", RunID: "789"}.Created(time.Now()),
	})

	env.AssertExpectations(t)

	require.True(t, env.IsWorkflowCompleted())

	workflowErr := env.GetWorkflowError()
	require.Error(t, workflowErr)
	require.Contains(t, workflowErr.Error(), "error executing hook:")
	// The way temporal wraps these they show up more than the number of hooks
	require.GreaterOrEqual(t, strings.Count(workflowErr.Error(), "error executing hook:"), 2)
}
