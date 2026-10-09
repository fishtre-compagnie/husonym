package usagestore

import (
	"context"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// runOfStatus makes a run that stands at the given status, by the path the API takes for it: an
// end the worker tells for completed, failed and canceled, a settlement for the two statuses only
// the orchestrator gives, and a start alone for a run still running.
func runOfStatus(ctx context.Context, t *testing.T, store *Store, runId string, status Status, told RunError) {
	t.Helper()
	started := october6
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: started,
	}))
	switch status {
	case StatusRunning:
	case StatusCompleted, StatusFailed, StatusCanceled:
		require.NoError(t, store.RunEnded(ctx, RunEnd{
			RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync,
			StartedAt: started, EndedAt: started.Add(time.Minute), Status: status, Error: told,
		}))
	case StatusTerminated, StatusTimedOut:
		require.NoError(t, store.Settle(ctx, runId, status, nil))
	}
}

// forgetPairs empties the category and the step of every row, as an API of the version before
// the two columns leaves them when it records the end of a run.
func forgetPairs(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer) {
	t.Helper()
	_, err := container.DB.Exec(ctx, `UPDATE husonym_api.run_usage SET error_category = NULL, error_step = NULL`)
	require.NoError(t, err)
}

// recordAllOn moves the moment every end was recorded; a run still running keeps none.
func recordAllOn(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, at time.Time) {
	t.Helper()
	_, err := container.DB.Exec(ctx,
		`UPDATE husonym_api.run_usage SET recorded_at = $1 WHERE recorded_at IS NOT NULL`, at)
	require.NoError(t, err)
}

func Test_ErrorsOfDay_CountsForTheDayTheEndWasRecorded(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	today := october6.Add(12 * time.Hour)
	constraint := RunError{Category: "constraint_violated", Step: "table_sync"}
	runOfStatus(ctx, t, store, "constraint-1", StatusFailed, constraint)
	runOfStatus(ctx, t, store, "constraint-2", StatusFailed, constraint)
	runOfStatus(ctx, t, store, "timeout", StatusFailed, RunError{Category: "timeout", Step: "preflight"})
	runOfStatus(ctx, t, store, "completed", StatusCompleted, RunError{})
	runOfStatus(ctx, t, store, "running", StatusRunning, RunError{})
	runOfStatus(ctx, t, store, "settled", StatusTimedOut, RunError{})
	runOfStatus(ctx, t, store, "canceled", StatusCanceled, RunError{Step: "hooks"})
	recordAllOn(ctx, t, container, today)

	// Ended today, and learned of the day before: the day is the one the end was recorded on.
	endRun(t, container, store, "yesterday", today.Add(-time.Minute), today, func(r *RunEnd) {
		r.Status, r.Error = StatusFailed, RunError{Category: "license", Step: "hooks"}
	})
	recordOn(t, container, "yesterday", today.Add(-24*time.Hour))

	errors, err := store.ErrorsOfDay(ctx, today)
	require.NoError(t, err)
	require.Equal(t, []ErrorCount{
		{Category: "canceled", Step: "hooks", Count: 1},
		{Category: "constraint_violated", Step: "table_sync", Count: 2},
		{Category: "timeout", Step: "other", Count: 1},
		{Category: "timeout", Step: "preflight", Count: 1},
	}, errors)

	before, err := store.ErrorsOfDay(ctx, today.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, []ErrorCount{{Category: "license", Step: "hooks", Count: 1}}, before)

	none, err := store.ErrorsOfDay(ctx, today.Add(24*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// A row that holds a status and no pair is what an API of the version before writes while both
// versions serve. Such a run is read with the rule the store writes with when nothing is told:
// for every status, the row without its pair counts exactly as the row the store wrote.
func Test_ErrorsOfDay_ARowWithoutItsPairCountsAsTheStoreWouldHaveWrittenIt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	day := october6.Add(12 * time.Hour)
	for _, status := range Statuses() {
		t.Run(string(status), func(t *testing.T) {
			_, err := container.DB.Exec(ctx, `DELETE FROM husonym_api.run_usage`)
			require.NoError(t, err)
			runOfStatus(ctx, t, store, "run", status, RunError{})
			recordAllOn(ctx, t, container, day)

			written, err := store.ErrorsOfDay(ctx, day)
			require.NoError(t, err)
			want := []ErrorCount{}
			if pair := errorOf(status, RunError{}); pair != (RunError{}) {
				want = append(want, ErrorCount{Category: pair.Category, Step: pair.Step, Count: 1})
			}
			require.Equal(t, want, written)

			forgetPairs(ctx, t, container)
			require.Empty(t, readRun(ctx, t, container, "run").pairOf())
			read, err := store.ErrorsOfDay(ctx, day)
			require.NoError(t, err)
			require.Equal(t, written, read)
		})
	}
}

// The errors of a day are the ones of the runs counted that day: whatever the rows hold, they
// add up to the runs of the day that neither completed nor still run.
func Test_ErrorsOfDay_AddUpToTheRunsOfTheDayThatDidNotComplete(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	day := october6.Add(12 * time.Hour)
	for _, status := range Statuses() {
		runOfStatus(ctx, t, store, "told-"+string(status), status, RunError{Category: "license", Step: "hooks"})
		runOfStatus(ctx, t, store, "untold-"+string(status), status, RunError{})
		runOfStatus(ctx, t, store, "old-"+string(status), status, RunError{})
	}
	recordAllOn(ctx, t, container, day)
	_, err := container.DB.Exec(ctx,
		`UPDATE husonym_api.run_usage SET error_category = NULL, error_step = NULL WHERE run_id LIKE 'old-%'`)
	require.NoError(t, err)
	// The day before and the day after hold runs of their own, which are not of this day.
	for name, at := range map[string]time.Time{"before": october6.Add(-time.Second), "after": october6.Add(24 * time.Hour)} {
		endRun(t, container, store, name, at.Add(-time.Minute), at, func(r *RunEnd) { r.Status = StatusFailed })
	}

	runs, err := store.RunsOfDay(ctx, day)
	require.NoError(t, err)
	var notCompleted int64
	for _, run := range runs.ByStatus {
		if run.Status != StatusCompleted {
			notCompleted += run.Count
		}
	}
	require.Equal(t, int64(12), notCompleted, "four statuses that did not complete, three runs each")

	errors, err := store.ErrorsOfDay(ctx, day)
	require.NoError(t, err)
	var counted int64
	for _, row := range errors {
		counted += row.Count
	}
	require.Equal(t, notCompleted, counted)
	require.Equal(t, []ErrorCount{
		{Category: "canceled", Step: "hooks", Count: 1},
		{Category: "canceled", Step: "other", Count: 2},
		{Category: "license", Step: "hooks", Count: 1},
		{Category: "other", Step: "other", Count: 5},
		{Category: "timeout", Step: "other", Count: 3},
	}, errors)
}
