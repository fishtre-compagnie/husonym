package workflow_shared

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// occupy starts a workflow under the id that the account hooks of the event would take
// now, and leaves it running: the hooks of the event then cannot be started.
func occupy(ctx workflow.Context, runId string, hook lifecycleHook) error {
	blocker := workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			WorkflowID:        getAccountHookChildWorkflowId(runId, hook.name, workflow.Now(ctx)),
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_ABANDON,
		}),
		occupant,
	)
	return blocker.GetChildWorkflowExecution().Get(ctx, nil)
}

func occupant(ctx workflow.Context) error {
	return workflow.Sleep(ctx, 24*time.Hour)
}

// lifecycleRun runs a body under HandleWorkflowEventLifecycle and returns the kinds of the
// events whose account hooks were started.
type lifecycleRun struct {
	env    *testsuite.TestWorkflowEnvironment
	mu     sync.Mutex
	events []string
}

func newLifecycleRun(t *testing.T) *lifecycleRun {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	run := &lifecycleRun{env: ts.NewTestWorkflowEnvironment()}
	run.env.RegisterWorkflow(occupant)
	run.env.RegisterWorkflow(accounthooks.ProcessAccountHook)
	run.env.OnWorkflow(accounthooks.ProcessAccountHook, mock.Anything, mock.Anything).
		Return(func(
			_ workflow.Context,
			req *accounthooks.ProcessAccountHookRequest,
		) (*accounthooks.ProcessAccountHookResponse, error) {
			run.mu.Lock()
			defer run.mu.Unlock()
			run.events = append(run.events, req.Event.Kind().String())
			return &accounthooks.ProcessAccountHookResponse{}, nil
		})
	return run
}

func (r *lifecycleRun) execute(
	before func(ctx workflow.Context) error,
	body func(ctx workflow.Context) (*string, error),
) {
	r.env.ExecuteWorkflow(func(ctx workflow.Context) (*string, error) {
		if before != nil {
			if err := before(ctx); err != nil {
				return nil, err
			}
		}
		return HandleWorkflowEventLifecycle(
			ctx,
			true,
			"job-123",
			"run-456",
			workflow.GetLogger(ctx),
			func() (string, error) { return "acc-789", nil },
			func(ctx workflow.Context, _ log.Logger) (*string, error) { return body(ctx) },
		)
	})
}

func (r *lifecycleRun) started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.events...)
}

const (
	createdKind   = "ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED"
	failedKind    = "ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED"
	succeededKind = "ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED"
)

// The account hooks do not change a run: when they cannot be started, the run goes on and
// ends as its body does.
func Test_HandleWorkflowEventLifecycle_GoesOnWhenTheHooksCannotBeStarted(t *testing.T) {
	result := "success"
	bodyErr := errors.New("function failed")

	t.Run("at the start of the run", func(t *testing.T) {
		run := newLifecycleRun(t)
		bodyRan := false

		run.execute(
			func(ctx workflow.Context) error { return occupy(ctx, "run-456", jobRunCreatedHook) },
			func(workflow.Context) (*string, error) {
				bodyRan = true
				return &result, nil
			},
		)

		require.True(t, run.env.IsWorkflowCompleted())
		require.NoError(t, run.env.GetWorkflowError())
		require.True(t, bodyRan)
		var got *string
		require.NoError(t, run.env.GetWorkflowResult(&got))
		require.Equal(t, "success", *got)
		require.Equal(t, []string{succeededKind}, run.started())
	})

	t.Run("at the end of a body that succeeded", func(t *testing.T) {
		run := newLifecycleRun(t)

		run.execute(nil, func(ctx workflow.Context) (*string, error) {
			return &result, occupy(ctx, "run-456", jobRunSucceededHook)
		})

		require.True(t, run.env.IsWorkflowCompleted())
		require.NoError(t, run.env.GetWorkflowError())
		var got *string
		require.NoError(t, run.env.GetWorkflowResult(&got))
		require.Equal(t, "success", *got)
		require.Equal(t, []string{createdKind}, run.started())
	})

	t.Run("at the end of a body that failed", func(t *testing.T) {
		run := newLifecycleRun(t)

		run.execute(nil, func(ctx workflow.Context) (*string, error) {
			if err := occupy(ctx, "run-456", jobRunFailedHook); err != nil {
				return nil, err
			}
			return nil, bodyErr
		})

		require.True(t, run.env.IsWorkflowCompleted())
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, run.env.GetWorkflowError(), &appErr)
		require.Equal(t, "function failed", appErr.Message(), "the error of the body, and nothing else")
		require.Equal(t, []string{createdKind}, run.started())
	})
}

// Runs started before the answer was tolerated replay as they ran: hooks that cannot be
// started end such a run with the error of the start.
func Test_HandleWorkflowEventLifecycle_EarlierRunsEndWhenTheHooksCannotBeStarted(t *testing.T) {
	result := "success"
	bodyErr := errors.New("function failed")
	earlier := func(run *lifecycleRun) {
		run.env.OnGetVersion(lifecycleHookStartToleratedChangeId, workflow.DefaultVersion, 1).
			Return(workflow.DefaultVersion)
	}

	t.Run("at the start of the run", func(t *testing.T) {
		run := newLifecycleRun(t)
		earlier(run)
		bodyRan := false

		run.execute(
			func(ctx workflow.Context) error { return occupy(ctx, "run-456", jobRunCreatedHook) },
			func(workflow.Context) (*string, error) {
				bodyRan = true
				return &result, nil
			},
		)

		require.True(t, run.env.IsWorkflowCompleted())
		require.ErrorContains(t, run.env.GetWorkflowError(), "already started")
		require.False(t, bodyRan)
		require.Empty(t, run.started())
	})

	t.Run("at the end of a body that succeeded", func(t *testing.T) {
		run := newLifecycleRun(t)
		earlier(run)

		run.execute(nil, func(ctx workflow.Context) (*string, error) {
			return &result, occupy(ctx, "run-456", jobRunSucceededHook)
		})

		require.True(t, run.env.IsWorkflowCompleted())
		require.ErrorContains(t, run.env.GetWorkflowError(), "already started")
	})

	t.Run("at the end of a body that failed", func(t *testing.T) {
		run := newLifecycleRun(t)
		earlier(run)

		run.execute(nil, func(ctx workflow.Context) (*string, error) {
			if err := occupy(ctx, "run-456", jobRunFailedHook); err != nil {
				return nil, err
			}
			return nil, bodyErr
		})

		require.True(t, run.env.IsWorkflowCompleted())
		require.ErrorContains(t, run.env.GetWorkflowError(), "function failed")
		require.ErrorContains(t, run.env.GetWorkflowError(), "already started")
	})
}

// The id of the change is written in the histories of the runs that met it: it stays.
func Test_LifecycleHookStartToleratedChangeId(t *testing.T) {
	require.Equal(t, "lifecycle-hook-start-tolerated", lifecycleHookStartToleratedChangeId)
}

// A run whose hooks start reads no version: its history holds no marker of the change, and
// the runs recorded earlier replay as they are.
func Test_HandleWorkflowEventLifecycle_ReadsNoVersionWhenTheHooksStart(t *testing.T) {
	run := newLifecycleRun(t)
	versionRead := false
	run.env.OnGetVersion(lifecycleHookStartToleratedChangeId, workflow.DefaultVersion, 1).
		Return(func(string, workflow.Version, workflow.Version) workflow.Version {
			versionRead = true
			return 1
		}).Maybe()
	result := "success"

	run.execute(nil, func(workflow.Context) (*string, error) { return &result, nil })

	require.NoError(t, run.env.GetWorkflowError())
	require.Equal(t, []string{createdKind, succeededKind}, run.started())
	require.False(t, versionRead)
}

// A run that is cancelled while its body runs ends cancelled, and the account hooks of its
// end are not started: no event tells a receiver that the run failed.
func Test_HandleWorkflowEventLifecycle_ACancelledRunStartsNoHookAtItsEnd(t *testing.T) {
	run := newLifecycleRun(t)
	versionRead := false
	run.env.OnGetVersion(lifecycleHookStartToleratedChangeId, workflow.DefaultVersion, 1).
		Return(func(string, workflow.Version, workflow.Version) workflow.Version {
			versionRead = true
			return 1
		}).Maybe()
	run.env.RegisterDelayedCallback(run.env.CancelWorkflow, time.Minute)

	run.execute(nil, func(ctx workflow.Context) (*string, error) {
		return nil, workflow.Sleep(ctx, time.Hour)
	})

	require.True(t, run.env.IsWorkflowCompleted())
	var canceled *temporal.CanceledError
	require.ErrorAs(t, run.env.GetWorkflowError(), &canceled)
	require.Equal(t, []string{createdKind}, run.started())
	// A cancellation is not a start that failed: nothing is recorded for it.
	require.False(t, versionRead)
}
