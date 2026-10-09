package workflow_shared

import (
	"errors"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// RunTotals is what the tables of a run counted, summed by the run workflow as each table
// finishes. Workflow code runs on one thread at a time, so plain additions are safe; the
// totals must only be added to from workflow code, never from an activity.
type RunTotals struct {
	RowsRead      int64
	RowsDiscarded int64
	Retries       int64
	// TablesUncounted is the number of tables that finished with a page, at least, not counted.
	TablesUncounted int64
	// SourceVersionMajor is the major version of the database the run reads, as the pre-flight
	// check read it; empty when it is not known.
	SourceVersionMajor string
}

// RunFailure is what the workflow of a run knows by itself of the error the run may end on,
// beyond the error. It is plain data the workflow sets as it goes, from workflow code only,
// and it is read when the run does not complete: nothing of it changes what the run does,
// nor the error it ends on.
type RunFailure struct {
	// Step is the step the run is at, set as the run enters it. Unspecified is "other": a
	// workflow that has no steps to tell sets nothing.
	Step mgmtv1alpha1.RunErrorStep
	// Category is the category of the error, when the workflow knows it by itself: a refusal
	// it returns. Unspecified, the category is read in the error (runerror.CategoryOf).
	Category mgmtv1alpha1.RunErrorCategory
	// Cause is the error the category is read in, when it is not the one the run ends on: the
	// run stopped itself on an error of its own because of this one. It is never returned.
	Cause error
}

const runUsageReportedChangeId = "run-usage-reported"

// TrackRunUsage reports the start of the run, runs fn, then reports its end whatever became of it.
//
// A run does not depend on its count: a report that does not leave is logged, and the run
// ends on exactly what fn returned. totals and failure are read once fn is done.
//
// A run that does not complete tells the category and the step of its error (runError). Both
// are read from what the run already holds, in workflow code: telling them schedules nothing,
// and adds two members to the argument of the report of the end, left out when empty.
//
// Runs started before their usage was reported hold no report in their history: they replay
// as they ran, fn and nothing else. The version is read first, before anything fn does, so
// that such a run meets it while it replays, whatever it has reached.
func TrackRunUsage[T any](
	ctx workflow.Context,
	jobId,
	runId string, // typically the temporal workflow execution id
	totals func() RunTotals,
	failure func() RunFailure,
	fn func(ctx workflow.Context) (*T, error),
) (*T, error) {
	if workflow.GetVersion(ctx, runUsageReportedChangeId, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return fn(ctx)
	}
	logger := workflow.GetLogger(ctx)
	var activities *runusage.Activities

	startedAt := workflow.Now(ctx)
	err := workflow.ExecuteActivity(
		withRunUsageActivityOptions(ctx),
		activities.RecordRunStarted,
		&runusage.RunStartedRequest{JobId: jobId, RunId: runId, StartedAt: startedAt},
	).Get(ctx, nil)
	if err != nil {
		logger.Warn("the start of the run was not reported", "error", err)
	}

	resp, fnErr := fn(ctx)

	counted := totals()
	outcome := runOutcome(ctx, fnErr)
	errorCategory, errorStep := runError(outcome, fnErr, failure())
	// The run may be failing or canceled: its context may be done already.
	detachedCtx, _ := workflow.NewDisconnectedContext(ctx)
	err = workflow.ExecuteActivity(
		withRunUsageActivityOptions(detachedCtx),
		activities.RecordRunEnded,
		&runusage.RunEndedRequest{
			JobId:         jobId,
			RunId:         runId,
			StartedAt:     startedAt,
			EndedAt:       workflow.Now(ctx),
			Outcome:       outcome,
			RowsRead:      counted.RowsRead,
			RowsDiscarded: counted.RowsDiscarded,
			Retries:       counted.Retries,

			TablesUncounted:    counted.TablesUncounted,
			SourceVersionMajor: counted.SourceVersionMajor,

			ErrorCategory: errorCategory,
			ErrorStep:     errorStep,
		},
	).Get(detachedCtx, nil)
	if err != nil {
		logger.Warn("the end of the run was not reported", "error", err)
	}

	return resp, fnErr
}

// runOutcome names what became of a run that ended on err. A run is canceled when someone
// asked for it: the context it was given is canceled then, whatever error the run returns.
// A run that cancels a context of its own, to stop what is left of it once something went
// wrong, has failed, even when the error it returns is the cancellation it caused.
//
// The outcome tells what became of the run, which can differ from the status Temporal
// closes it with: Temporal goes by the error alone.
func runOutcome(ctx workflow.Context, err error) string {
	switch {
	case err == nil:
		return runusage.OutcomeCompleted
	case errors.Is(ctx.Err(), workflow.ErrCanceled):
		return runusage.OutcomeCanceled
	default:
		return runusage.OutcomeFailed
	}
}

// runError gives the category and the step of the error of a run, for the report of its end.
// A run that completed tells neither. A canceled run is canceled whatever error it returns.
// A run that failed tells the category its workflow knows by itself, else the one read in
// the error that made it stop itself, when it tells one, else in the error it ends on. The
// step is the one the run was at, "other" when it told none.
//
// It reads what it is given and nothing else: the same answer on every replay.
//
// It is workflow code, where a panic does not fail a run but blocks it: an error that cannot
// be read is "other", at the step already known, and the run ends on its error as it would.
func runError(
	outcome string,
	err error,
	failure RunFailure,
) (category mgmtv1alpha1.RunErrorCategory, step mgmtv1alpha1.RunErrorStep) {
	if outcome == runusage.OutcomeCompleted {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED, mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED
	}
	step = failure.Step
	if step == mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED {
		step = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER
	}
	defer func() {
		if recover() != nil {
			category = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
		}
	}()
	switch {
	case outcome == runusage.OutcomeCanceled:
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED, step
	case failure.Category != mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED:
		return failure.Category, step
	case failure.Cause != nil:
		return runerror.CategoryOf(failure.Cause), step
	default:
		return runerror.CategoryOf(err), step
	}
}

func withRunUsageActivityOptions(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// The start is waited for before the run works: an API that does not answer must
		// not hold a run for long.
		StartToCloseTimeout: 15 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	})
}
