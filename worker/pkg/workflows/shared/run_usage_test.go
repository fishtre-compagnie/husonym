package workflow_shared

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

type trackedResult struct {
	Value string
}

// trackedRun is a run under TrackRunUsage in a test environment, and what the API was told
// of it.
type trackedRun struct {
	env *testsuite.TestWorkflowEnvironment

	mu sync.Mutex
	// steps is what happened, in order: "started", "fn" and "ended".
	steps   []string
	started []*runusage.RunStartedRequest
	ended   []*runusage.RunEndedRequest
	// startErr and endErr are what the two activities answer.
	startErr error
	endErr   error
	// same tells whether TrackRunUsage returned the very values fn returned.
	same bool
}

func newTrackedRun() *trackedRun {
	run := &trackedRun{env: (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()}
	var activities *runusage.Activities
	run.env.OnActivity(activities.RecordRunStarted, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *runusage.RunStartedRequest) error {
			run.mu.Lock()
			defer run.mu.Unlock()
			run.steps = append(run.steps, "started")
			run.started = append(run.started, req)
			return run.startErr
		}).Maybe()
	run.env.OnActivity(activities.RecordRunEnded, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *runusage.RunEndedRequest) error {
			run.mu.Lock()
			defer run.mu.Unlock()
			run.steps = append(run.steps, "ended")
			run.ended = append(run.ended, req)
			return run.endErr
		}).Maybe()
	return run
}

// execute runs fn under TrackRunUsage, the totals being read from the pointer fn is handed.
func (r *trackedRun) execute(fn func(ctx workflow.Context, totals *RunTotals) (*trackedResult, error)) {
	r.env.ExecuteWorkflow(func(ctx workflow.Context) (*trackedResult, error) {
		totals := &RunTotals{}
		var fnResult *trackedResult
		var fnErr error
		result, err := TrackRunUsage(ctx, "job-1", "run-1",
			func() RunTotals { return *totals },
			func(ctx workflow.Context) (*trackedResult, error) {
				r.mu.Lock()
				r.steps = append(r.steps, "fn")
				r.mu.Unlock()
				fnResult, fnErr = fn(ctx, totals)
				return fnResult, fnErr
			},
		)
		//nolint:errorlint // the very error of fn is expected, not one that wraps it
		r.same = result == fnResult && err == fnErr
		return result, err
	})
}

func Test_TrackRunUsage_ReportsTheStartThenTheEndOfARunThatCompletes(t *testing.T) {
	run := newTrackedRun()
	run.execute(func(ctx workflow.Context, totals *RunTotals) (*trackedResult, error) {
		if err := workflow.Sleep(ctx, 10*time.Minute); err != nil {
			return nil, err
		}
		*totals = RunTotals{RowsRead: 42, RowsDiscarded: 3, Retries: 2, TablesUncounted: 4, SourceVersionMajor: "16"}
		return &trackedResult{Value: "done"}, nil
	})

	require.True(t, run.env.IsWorkflowCompleted())
	require.NoError(t, run.env.GetWorkflowError())
	var result trackedResult
	require.NoError(t, run.env.GetWorkflowResult(&result))
	assert.Equal(t, "done", result.Value)
	assert.True(t, run.same)

	require.Equal(t, []string{"started", "fn", "ended"}, run.steps)
	started, ended := run.started[0], run.ended[0]
	assert.Equal(t, "job-1", started.JobId)
	assert.Equal(t, "run-1", started.RunId)
	assert.False(t, started.StartedAt.IsZero())

	assert.Equal(t, "job-1", ended.JobId)
	assert.Equal(t, "run-1", ended.RunId)
	assert.True(t, started.StartedAt.Equal(ended.StartedAt), "the end carries the start it reported")
	assert.Equal(t, 10*time.Minute, ended.EndedAt.Sub(ended.StartedAt))
	assert.Equal(t, runusage.OutcomeCompleted, ended.Outcome)
	// The totals are read once fn is done: they are what it counted.
	assert.Equal(t, int64(42), ended.RowsRead)
	assert.Equal(t, int64(3), ended.RowsDiscarded)
	assert.Equal(t, int64(2), ended.Retries)
	assert.Equal(t, int64(4), ended.TablesUncounted)
	assert.Equal(t, "16", ended.SourceVersionMajor)
}

func Test_TrackRunUsage_ReportsARunThatFails(t *testing.T) {
	run := newTrackedRun()
	run.execute(func(_ workflow.Context, totals *RunTotals) (*trackedResult, error) {
		totals.RowsRead = 7
		return nil, temporal.NewApplicationError("TestFailure", "Test")
	})

	require.True(t, run.env.IsWorkflowCompleted())
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, run.env.GetWorkflowError(), &applicationErr)
	assert.Equal(t, "TestFailure", applicationErr.Message())
	assert.True(t, run.same, "the error returned is the one of fn")

	require.Equal(t, []string{"started", "fn", "ended"}, run.steps)
	assert.Equal(t, runusage.OutcomeFailed, run.ended[0].Outcome)
	assert.Equal(t, int64(7), run.ended[0].RowsRead)
}

// The context of a canceled run starts nothing more: the end leaves on a context of its
// own, and the run ends canceled as it did before it reported anything.
func Test_TrackRunUsage_ReportsARunThatIsCanceled(t *testing.T) {
	t.Run("fn returns the cancellation", func(t *testing.T) {
		run := newTrackedRun()
		run.env.RegisterDelayedCallback(run.env.CancelWorkflow, time.Minute)
		run.execute(func(ctx workflow.Context, _ *RunTotals) (*trackedResult, error) {
			return nil, workflow.Sleep(ctx, time.Hour)
		})

		require.True(t, run.env.IsWorkflowCompleted())
		var canceledErr *temporal.CanceledError
		require.ErrorAs(t, run.env.GetWorkflowError(), &canceledErr)
		assert.True(t, run.same)

		require.Equal(t, []string{"started", "fn", "ended"}, run.steps)
		assert.Equal(t, runusage.OutcomeCanceled, run.ended[0].Outcome)
		assert.Equal(t, time.Minute, run.ended[0].EndedAt.Sub(run.ended[0].StartedAt))
	})

	t.Run("fn returns another error of a canceled run", func(t *testing.T) {
		run := newTrackedRun()
		run.env.RegisterDelayedCallback(run.env.CancelWorkflow, time.Minute)
		run.execute(func(ctx workflow.Context, _ *RunTotals) (*trackedResult, error) {
			_ = workflow.Sleep(ctx, time.Hour)
			return nil, errors.New("a table did not finish")
		})

		require.True(t, run.env.IsWorkflowCompleted())
		require.ErrorContains(t, run.env.GetWorkflowError(), "a table did not finish")
		assert.True(t, run.same)

		require.Equal(t, []string{"started", "fn", "ended"}, run.steps)
		assert.Equal(t, runusage.OutcomeCanceled, run.ended[0].Outcome)
	})
}

// A run that stops what is left of it by canceling a context of its own, and returns the
// cancellation it caused, has failed: nobody asked for it to be canceled.
func Test_TrackRunUsage_ReportsAsFailedARunThatCanceledItself(t *testing.T) {
	run := newTrackedRun()
	run.execute(func(ctx workflow.Context, _ *RunTotals) (*trackedResult, error) {
		inner, cancel := workflow.WithCancel(ctx)
		cancel()
		err := workflow.Sleep(inner, time.Hour)
		return nil, fmt.Errorf("workflow canceled due to error or stop signal: %w", err)
	})

	require.True(t, run.env.IsWorkflowCompleted())
	require.True(t, temporal.IsCanceledError(run.env.GetWorkflowError()), "%v", run.env.GetWorkflowError())
	assert.True(t, run.same)

	require.Equal(t, []string{"started", "fn", "ended"}, run.steps)
	assert.Equal(t, runusage.OutcomeFailed, run.ended[0].Outcome)
}

// The start is waited for before the run works: neither report may hold a run for long.
func Test_TrackRunUsage_GivesItsReportsLittleTime(t *testing.T) {
	run := newTrackedRun()
	timeouts := map[string]time.Duration{}
	run.env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		run.mu.Lock()
		defer run.mu.Unlock()
		timeouts[info.ActivityType.Name] = info.StartToCloseTimeout
	})
	run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
		return &trackedResult{Value: "done"}, nil
	})

	require.NoError(t, run.env.GetWorkflowError())
	require.Equal(t, map[string]time.Duration{
		"RecordRunStarted": 15 * time.Second,
		"RecordRunEnded":   15 * time.Second,
	}, timeouts)
}

// The start is asked three times, then the run goes on without it.
func Test_TrackRunUsage_RunsWhenTheStartIsNotReported(t *testing.T) {
	run := newTrackedRun()
	run.startErr = errors.New("the API is away")
	run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
		return &trackedResult{Value: "done"}, nil
	})

	require.True(t, run.env.IsWorkflowCompleted())
	require.NoError(t, run.env.GetWorkflowError())
	var result trackedResult
	require.NoError(t, run.env.GetWorkflowResult(&result))
	assert.Equal(t, "done", result.Value)
	assert.True(t, run.same)

	require.Equal(t, []string{"started", "started", "started", "fn", "ended"}, run.steps)
	assert.Equal(t, runusage.OutcomeCompleted, run.ended[0].Outcome)
}

// The end is asked three times, and the run ends on what fn returned whatever the answer.
func Test_TrackRunUsage_KeepsTheResultWhenTheEndIsNotReported(t *testing.T) {
	t.Run("a run that completes", func(t *testing.T) {
		run := newTrackedRun()
		run.endErr = errors.New("the API is away")
		run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
			return &trackedResult{Value: "done"}, nil
		})

		require.True(t, run.env.IsWorkflowCompleted())
		require.NoError(t, run.env.GetWorkflowError())
		var result trackedResult
		require.NoError(t, run.env.GetWorkflowResult(&result))
		assert.Equal(t, "done", result.Value)
		assert.True(t, run.same)
		require.Equal(t, []string{"started", "fn", "ended", "ended", "ended"}, run.steps)
	})

	t.Run("a run that fails", func(t *testing.T) {
		run := newTrackedRun()
		run.endErr = errors.New("the API is away")
		run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
			return nil, temporal.NewApplicationError("TestFailure", "Test")
		})

		require.True(t, run.env.IsWorkflowCompleted())
		var applicationErr *temporal.ApplicationError
		require.ErrorAs(t, run.env.GetWorkflowError(), &applicationErr)
		assert.Equal(t, "TestFailure", applicationErr.Message())
		assert.True(t, run.same)
	})
}

// A run started before its usage was reported replays as it ran: fn, and nothing else.
func Test_TrackRunUsage_EarlierRunsReportNothing(t *testing.T) {
	run := newTrackedRun()
	run.env.OnGetVersion(runUsageReportedChangeId, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
		return &trackedResult{Value: "done"}, nil
	})

	require.True(t, run.env.IsWorkflowCompleted())
	require.NoError(t, run.env.GetWorkflowError())
	assert.True(t, run.same)
	require.Equal(t, []string{"fn"}, run.steps)
}

// The id of the change is written in the histories of the runs that met it: it stays.
func Test_RunUsageChangeId(t *testing.T) {
	require.Equal(t, "run-usage-reported", runUsageReportedChangeId)
}
