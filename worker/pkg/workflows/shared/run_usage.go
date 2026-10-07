package workflow_shared

import (
	"errors"
	"time"

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

const runUsageReportedChangeId = "run-usage-reported"

// TrackRunUsage reports the start of the run, runs fn, then reports its end whatever became of it.
//
// A run does not depend on its count: a report that does not leave is logged, and the run
// ends on exactly what fn returned. totals is read once fn is done.
//
// Runs started before their usage was reported hold no report in their history: they replay
// as they ran, fn and nothing else. The version is read first, before anything fn does, so
// that such a run meets it while it replays, whatever it has reached.
func TrackRunUsage[T any](
	ctx workflow.Context,
	jobId,
	runId string, // typically the temporal workflow execution id
	totals func() RunTotals,
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
			Outcome:       runOutcome(ctx, fnErr),
			RowsRead:      counted.RowsRead,
			RowsDiscarded: counted.RowsDiscarded,
			Retries:       counted.Retries,

			TablesUncounted:    counted.TablesUncounted,
			SourceVersionMajor: counted.SourceVersionMajor,
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
