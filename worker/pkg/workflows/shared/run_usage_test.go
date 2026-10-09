package workflow_shared

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
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
	// failure is what the run knows of its error by itself, read once fn is done: fn sets it.
	failure RunFailure
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
			func() RunFailure { return r.failure },
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

const (
	categoryLicense            = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE
	categoryCanceled           = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED
	categoryConstraintViolated = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED
	categoryTimeout            = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
	categoryOther              = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
	categoryUnspecified        = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED

	stepHooks       = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_HOOKS
	stepTableSync   = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_TABLE_SYNC
	stepOther       = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER
	stepUnspecified = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED
)

func Test_TrackRunUsage_TellsTheErrorOfARunThatFails(t *testing.T) {
	database := &pgconn.PgError{Code: "23505", Message: "never-sent"}
	cases := []struct {
		name     string
		failure  RunFailure
		err      error
		category mgmtv1alpha1.RunErrorCategory
		step     mgmtv1alpha1.RunErrorStep
	}{
		{
			"the category an activity carried out, at the step the run was at",
			RunFailure{Step: stepTableSync},
			fmt.Errorf("table users: %w", runerror.Carry(fmt.Errorf("writing: %w", database))),
			categoryConstraintViolated, stepTableSync,
		},
		{
			"a refusal of the license raised in an activity",
			RunFailure{Step: stepHooks},
			fmt.Errorf("x: %w", runerror.License(errors.New("refused"))),
			categoryLicense, stepHooks,
		},
		{
			"a refusal the workflow tells by itself",
			RunFailure{Category: categoryLicense},
			errors.New("refused"),
			categoryLicense, stepOther,
		},
		{
			"what the workflow tells by itself comes before what its error carries",
			RunFailure{Category: categoryLicense, Step: stepTableSync},
			runerror.Carry(fmt.Errorf("writing: %w", database)),
			categoryLicense, stepTableSync,
		},
		{
			"an activity that timed out",
			RunFailure{Step: stepTableSync},
			fmt.Errorf("x: %w", temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)),
			categoryTimeout, stepTableSync,
		},
		{"an error that tells nothing, at no step", RunFailure{}, errors.New("x"), categoryOther, stepOther},
		{"a database error that was not carried tells nothing", RunFailure{}, database, categoryOther, stepOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newTrackedRun()
			run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
				run.failure = tc.failure
				return nil, tc.err
			})

			require.True(t, run.env.IsWorkflowCompleted())
			require.Error(t, run.env.GetWorkflowError())
			assert.True(t, run.same, "the error returned is the one of fn")
			require.Len(t, run.ended, 1)
			assert.Equal(t, runusage.OutcomeFailed, run.ended[0].Outcome)
			assert.Equal(t, tc.category, run.ended[0].ErrorCategory)
			assert.Equal(t, tc.step, run.ended[0].ErrorStep)
		})
	}
}

func Test_TrackRunUsage_ACompletedRunTellsNoError(t *testing.T) {
	run := newTrackedRun()
	run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
		// The step the run was last at, and a category it would have told had it failed.
		run.failure = RunFailure{Step: stepHooks, Category: categoryLicense}
		return &trackedResult{Value: "done"}, nil
	})

	require.NoError(t, run.env.GetWorkflowError())
	require.Len(t, run.ended, 1)
	assert.Equal(t, runusage.OutcomeCompleted, run.ended[0].Outcome)
	assert.Equal(t, categoryUnspecified, run.ended[0].ErrorCategory)
	assert.Equal(t, stepUnspecified, run.ended[0].ErrorStep)
}

func Test_TrackRunUsage_ACanceledRunIsCanceledWhateverItsError(t *testing.T) {
	t.Run("fn returns the cancellation", func(t *testing.T) {
		run := newTrackedRun()
		run.env.RegisterDelayedCallback(run.env.CancelWorkflow, time.Minute)
		run.execute(func(ctx workflow.Context, _ *RunTotals) (*trackedResult, error) {
			run.failure = RunFailure{Step: stepTableSync}
			return nil, workflow.Sleep(ctx, time.Hour)
		})

		require.Len(t, run.ended, 1)
		assert.Equal(t, runusage.OutcomeCanceled, run.ended[0].Outcome)
		assert.Equal(t, categoryCanceled, run.ended[0].ErrorCategory)
		assert.Equal(t, stepTableSync, run.ended[0].ErrorStep)
	})

	t.Run("fn returns another error of a canceled run", func(t *testing.T) {
		run := newTrackedRun()
		run.env.RegisterDelayedCallback(run.env.CancelWorkflow, time.Minute)
		run.execute(func(ctx workflow.Context, _ *RunTotals) (*trackedResult, error) {
			_ = workflow.Sleep(ctx, time.Hour)
			// A table that was stopped by the cancellation may fail on a constraint all the
			// same, and a run may tell a category of its own: the run was canceled.
			run.failure = RunFailure{Category: categoryLicense}
			return nil, runerror.Carry(fmt.Errorf("writing: %w", &pgconn.PgError{Code: "23505"}))
		})

		require.Len(t, run.ended, 1)
		assert.Equal(t, runusage.OutcomeCanceled, run.ended[0].Outcome)
		assert.Equal(t, categoryCanceled, run.ended[0].ErrorCategory)
		assert.Equal(t, stepOther, run.ended[0].ErrorStep)
	})

	// A run that canceled a context of its own has failed: nothing forces its category. The
	// one it gets is the one its error tells, which here is the cancellation it returns.
	t.Run("a run that canceled itself is told by its error", func(t *testing.T) {
		for name, tc := range map[string]struct {
			wrap     func(canceled error) error
			category mgmtv1alpha1.RunErrorCategory
		}{
			"returning the cancellation it caused": {
				func(canceled error) error { return fmt.Errorf("workflow canceled due to error: %w", canceled) },
				categoryCanceled,
			},
			"returning the error that made it cancel": {
				func(error) error { return runerror.Carry(fmt.Errorf("x: %w", &pgconn.PgError{Code: "23505"})) },
				categoryConstraintViolated,
			},
		} {
			t.Run(name, func(t *testing.T) {
				run := newTrackedRun()
				run.execute(func(ctx workflow.Context, _ *RunTotals) (*trackedResult, error) {
					inner, cancel := workflow.WithCancel(ctx)
					cancel()
					return nil, tc.wrap(workflow.Sleep(inner, time.Hour))
				})

				require.Len(t, run.ended, 1)
				assert.Equal(t, runusage.OutcomeFailed, run.ended[0].Outcome)
				assert.Equal(t, tc.category, run.ended[0].ErrorCategory)
			})
		}
	})
}

// Telling the error asks nothing more of Temporal: the same two reports around fn, and the
// run ends on the very error of fn.
func Test_TrackRunUsage_TellsTheErrorWithoutSchedulingMore(t *testing.T) {
	run := newTrackedRun()
	scheduled := []string{}
	run.env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		run.mu.Lock()
		defer run.mu.Unlock()
		scheduled = append(scheduled, info.ActivityType.Name)
	})
	timers := 0
	run.env.SetOnTimerScheduledListener(func(string, time.Duration) { timers++ })
	run.execute(func(workflow.Context, *RunTotals) (*trackedResult, error) {
		run.failure = RunFailure{Step: stepHooks}
		return nil, fmt.Errorf("x: %w", runerror.License(errors.New("refused")))
	})

	require.True(t, run.env.IsWorkflowCompleted())
	require.ErrorContains(t, run.env.GetWorkflowError(), "x: refused")
	assert.True(t, run.same)
	require.Equal(t, []string{"started", "fn", "ended"}, run.steps)
	assert.Equal(t, []string{"RecordRunStarted", "RecordRunEnded"}, scheduled)
	assert.Zero(t, timers)
	assert.Equal(t, categoryLicense, run.ended[0].ErrorCategory)
}

// runError is all that decides what is told, and reads nothing but what it is given.
func Test_runError(t *testing.T) {
	licensed := runerror.License(errors.New("refused"))
	for name, tc := range map[string]struct {
		outcome  string
		err      error
		failure  RunFailure
		category mgmtv1alpha1.RunErrorCategory
		step     mgmtv1alpha1.RunErrorStep
	}{
		"completed":            {runusage.OutcomeCompleted, nil, RunFailure{Step: stepHooks}, categoryUnspecified, stepUnspecified},
		"canceled":             {runusage.OutcomeCanceled, licensed, RunFailure{Step: stepHooks}, categoryCanceled, stepHooks},
		"canceled, at no step": {runusage.OutcomeCanceled, licensed, RunFailure{}, categoryCanceled, stepOther},
		"failed":               {runusage.OutcomeFailed, licensed, RunFailure{Step: stepTableSync}, categoryLicense, stepTableSync},
		"failed, nothing told": {runusage.OutcomeFailed, errors.New("x"), RunFailure{}, categoryOther, stepOther},
		"failed on an error of its own, because of another": {
			runusage.OutcomeFailed, errors.New("x"),
			RunFailure{Step: stepTableSync, Cause: runerror.Carry(fmt.Errorf("y: %w", &pgconn.PgError{Code: "23505"}))},
			categoryConstraintViolated, stepTableSync,
		},
		"failed because of an error that tells nothing": {
			runusage.OutcomeFailed, licensed, RunFailure{Cause: errors.New("y")}, categoryOther, stepOther,
		},
		"what the workflow tells by itself comes before the cause": {
			runusage.OutcomeFailed, errors.New("x"),
			RunFailure{Category: categoryLicense, Cause: errors.New("y")}, categoryLicense, stepOther,
		},
		"canceled, whatever the cause": {
			runusage.OutcomeCanceled, errors.New("x"), RunFailure{Cause: licensed}, categoryCanceled, stepOther,
		},
	} {
		t.Run(name, func(t *testing.T) {
			category, step := runError(tc.outcome, tc.err, tc.failure)
			assert.Equal(t, tc.category, category)
			assert.Equal(t, tc.step, step)
		})
	}
}

// brittleError is an error whose methods read a member: a nil pointer of it under a wrapper
// is an error that is not nil, and that panics as soon as it is walked.
type brittleError struct{ cause error }

func (e *brittleError) Error() string { return "brittle" }
func (e *brittleError) Unwrap() error { return e.cause }

// A panic in workflow code does not fail a run, it blocks it: an error that cannot be read
// is "other", at the step the run was at.
func Test_runError_AnErrorThatCannotBeReadIsOther(t *testing.T) {
	var brittle *brittleError
	err := fmt.Errorf("x: %w", error(brittle))

	var category mgmtv1alpha1.RunErrorCategory
	var step mgmtv1alpha1.RunErrorStep
	require.NotPanics(t, func() {
		category, step = runError(runusage.OutcomeFailed, err, RunFailure{Step: stepTableSync})
	})
	assert.Equal(t, categoryOther, category)
	assert.Equal(t, stepTableSync, step)

	require.NotPanics(t, func() {
		category, step = runError(runusage.OutcomeFailed, err, RunFailure{})
	})
	assert.Equal(t, categoryOther, category)
	assert.Equal(t, stepOther, step)
}

// The id of the change is written in the histories of the runs that met it: it stays.
func Test_RunUsageChangeId(t *testing.T) {
	require.Equal(t, "run-usage-reported", runUsageReportedChangeId)
}
