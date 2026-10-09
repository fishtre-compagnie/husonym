package usagereport

import (
	"encoding/json"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// The errors of a report are the ones of the runs the same report counts: on a day that holds
// runs of every status, told with their error, told without, and written by an API of the version
// before the store kept an error, the rows of errors add up to the runs that did not complete, and
// the document is one its schema accepts. The day before and the day after hold runs of their own.
func Test_Build_TheErrorsAddUpToTheRunsOfTheSameReportThatDidNotComplete(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx, _ := logged(t)
	pool := migratedPool(ctx, t)

	db := husonymdb.New(pool, db_queries.New())
	roles, err := rbac.New(ctx, pool, testutil.GetTestLogger(t))
	require.NoError(t, err)
	store := usagestore.New(db)
	keyValue, ring := mintKey(t, keyExpiring(testExpiry))
	builder := NewBuilder(
		store,
		NewInventoryReader(db, licensegate.NewUsageReader(db, roles), roles, store, true),
		noOrchestrator{NewInstanceReader(db, nil)},
		&fakeLicense{inForce: true},
		&fakeKeys{value: keyValue},
		ring,
		Facts{Version: "v0.3.0", InstallKind: "helm", OS: "linux", Arch: "amd64", Diagnostics: true},
	)

	const account, job = "00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-0000000000a1"
	started := reportDay.Add(time.Hour)
	// run makes a run that stands at the status, the way the API learns of it, recorded at the moment.
	run := func(runId string, status usagestore.Status, told usagestore.RunError, recorded time.Time) {
		t.Helper()
		require.NoError(t, store.RunStarted(ctx, usagestore.RunStart{
			RunId: runId, AccountId: account, JobId: job, Kind: usagestore.JobKindSync, StartedAt: started,
		}))
		switch status {
		case usagestore.StatusRunning:
			return
		case usagestore.StatusCompleted, usagestore.StatusFailed, usagestore.StatusCanceled:
			require.NoError(t, store.RunEnded(ctx, usagestore.RunEnd{
				RunId: runId, AccountId: account, JobId: job, Kind: usagestore.JobKindSync,
				StartedAt: started, EndedAt: started.Add(time.Minute), Status: status, Error: told,
			}))
		case usagestore.StatusTerminated, usagestore.StatusTimedOut:
			require.NoError(t, store.Settle(ctx, runId, status, nil))
		}
		_, err := pool.Exec(ctx, `UPDATE husonym_api.run_usage SET recorded_at = $2 WHERE run_id = $1`, runId, recorded)
		require.NoError(t, err)
	}

	noon := reportDay.Add(12 * time.Hour)
	for _, status := range usagestore.Statuses() {
		name := string(status)
		run("told-"+name, status, usagestore.RunError{Category: "constraint_violated", Step: "table_sync"}, noon)
		run("untold-"+name, status, usagestore.RunError{}, noon)
		run("old-"+name, status, usagestore.RunError{}, noon)
	}
	_, err = pool.Exec(ctx,
		`UPDATE husonym_api.run_usage SET error_category = NULL, error_step = NULL WHERE run_id LIKE 'old-%'`)
	require.NoError(t, err)
	run("the-day-before", usagestore.StatusFailed, usagestore.RunError{}, reportDay.Add(-time.Second))
	run("the-day-after", usagestore.StatusFailed, usagestore.RunError{}, reportDay.Add(24*time.Hour))

	sealed, err := builder.Build(ctx, reportDay, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.Validate(sealed.Document))

	var report telemetry.Report
	require.NoError(t, json.Unmarshal(sealed.Document, &report))
	require.NotNil(t, report.Diagnostics)

	notCompleted := 0
	for _, counted := range report.Diagnostics.Runs.ByStatus {
		require.NotEqual(t, "running", counted.Status, "a run still running counts for no day")
		if counted.Status != "completed" {
			notCompleted += counted.Count
		}
	}
	require.Equal(t, 12, notCompleted, "four statuses that did not complete, three runs each")

	errorsCounted := 0
	for _, row := range report.Diagnostics.Errors {
		require.Contains(t, telemetry.ErrorCategories, row.Category)
		require.Contains(t, telemetry.ErrorSteps, row.Step)
		errorsCounted += row.Count
	}
	require.Equal(t, notCompleted, errorsCounted)
	require.Equal(t, []telemetry.ErrorCount{
		{Category: "canceled", Step: "other", Count: 2},
		{Category: "canceled", Step: "table_sync", Count: 1},
		{Category: "constraint_violated", Step: "table_sync", Count: 1},
		{Category: "other", Step: "other", Count: 5},
		{Category: "timeout", Step: "other", Count: 3},
	}, report.Diagnostics.Errors)
}
