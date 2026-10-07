package usagestore

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

var (
	september1 = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	october1   = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	november1  = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
)

// The runs of a month are the ones recorded from its first instant to the last before the next
// month: a run recorded at the last second of the month before, or at the first of the month
// after, is not among them.
func Test_RunsBetween_AddsUpAMonthAndStopsAtItsEdges(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	for runId, run := range map[string]struct {
		recorded time.Time
		seconds  int
		rows     int64
		status   Status
	}{
		"before":     {october1.Add(-time.Second), 30, 1_000_000, StatusCompleted},
		"first":      {october1, 60, 400, StatusCompleted},
		"middle":     {october1.AddDate(0, 0, 14).Add(12 * time.Hour), 120, 500, StatusFailed},
		"last":       {november1.Add(-time.Second), 600, 99, StatusCompleted},
		"after":      {november1, 30, 2_000_000, StatusCompleted},
		"much later": {november1.AddDate(0, 0, 20), 30, 4_000_000, StatusCompleted},
	} {
		endRun(t, container, store, runId, run.recorded.Add(-time.Duration(run.seconds)*time.Second), run.recorded,
			func(r *RunEnd) {
				r.Status, r.RowsRead, r.RowsDiscarded, r.Retries, r.TablesUncounted = run.status, run.rows, 2, 1, 1
			})
	}
	// A run settled without an end is of the month, and has no duration.
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "lost", AccountId: accountB, JobId: jobA, Kind: JobKindGenerate, StartedAt: october1,
	}))
	require.NoError(t, store.Settle(ctx, "lost", StatusTerminated, nil))
	recordOn(t, container, "lost", october6)
	// A run still running is of no month.
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "running", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: october6,
	}))

	october, err := store.RunsBetween(ctx, october1, november1)
	require.NoError(t, err)
	require.Equal(t, []RunCount{
		{Kind: JobKindGenerate, Status: StatusTerminated, Count: 1},
		{Kind: JobKindSync, Status: StatusCompleted, Count: 2},
		{Kind: JobKindSync, Status: StatusFailed, Count: 1},
	}, october.ByStatus)
	require.Equal(t, int64(999), october.RowsRead)
	require.Equal(t, int64(6), october.RowsDiscarded)
	require.Equal(t, int64(3), october.Retries)
	require.Equal(t, int64(3), october.WithUncountedRows)
	// The median and the 95th percentile are the ones of the three runs that have an end.
	require.NotNil(t, october.DurationMedian)
	require.NotNil(t, october.DurationP95)
	require.Equal(t, int64(120), *october.DurationMedian)
	require.Equal(t, int64(552), *october.DurationP95)

	// Any time of a day names the day, as for the runs of a day.
	same, err := store.RunsBetween(ctx, october1.Add(17*time.Hour), november1.Add(23*time.Hour))
	require.NoError(t, err)
	require.Equal(t, october, same)

	september, err := store.RunsBetween(ctx, september1, october1)
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusCompleted, Count: 1}}, september.ByStatus)
	require.Equal(t, int64(1_000_000), september.RowsRead)

	november, err := store.RunsBetween(ctx, november1, november1.AddDate(0, 1, 0))
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusCompleted, Count: 2}}, november.ByStatus)
	require.Equal(t, int64(6_000_000), november.RowsRead)

	// The runs of a day are the ones of the days from it to the next.
	day, err := store.RunsOfDay(ctx, october1)
	require.NoError(t, err)
	between, err := store.RunsBetween(ctx, october1, october1.AddDate(0, 0, 1))
	require.NoError(t, err)
	require.Equal(t, day, between)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusCompleted, Count: 1}}, day.ByStatus)

	none, err := store.RunsBetween(ctx, september1.AddDate(0, -1, 0), september1)
	require.NoError(t, err)
	require.Empty(t, none.ByStatus)
	require.NotNil(t, none.ByStatus)
	require.Nil(t, none.DurationMedian)
	require.Nil(t, none.DurationP95)
	require.Zero(t, none.RowsRead)
}

func Test_RefusalsBetween_SumsTheDaysAndTheAccountsOfAMonth(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	refuse := func(account string, at time.Time, gates ...license.Gate) {
		t.Helper()
		require.NoError(t, store.CountRefusal(ctx, account, gates, at))
	}
	refuse(accountA, october1.Add(-time.Second), license.GateJobCap)
	refuse(accountA, october1, license.GateJobCap)
	refuse(accountB, october6, license.GateJobCap, license.GateSourceCap)
	refuse(accountA, november1.Add(-time.Second), license.GateJobCap)
	refuse(accountB, november1, license.GateSourceCap)

	october, err := store.RefusalsBetween(ctx, october1, november1)
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateJobCap, Count: 3}, {Gate: license.GateSourceCap, Count: 1}}, october)

	september, err := store.RefusalsBetween(ctx, september1, october1)
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateJobCap, Count: 1}}, september)

	day, err := store.RefusalsOfDay(ctx, october6)
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateJobCap, Count: 1}, {Gate: license.GateSourceCap, Count: 1}}, day)

	none, err := store.RefusalsBetween(ctx, november1.AddDate(0, 1, 0), november1.AddDate(0, 2, 0))
	require.NoError(t, err)
	require.Empty(t, none)
}

func Test_ReportsBetween_GivesTheReportsOfAMonthTheOldestFirst(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	prepared := november1.Add(5 * time.Minute)
	for _, day := range []time.Time{
		november1, october6, november1.AddDate(0, 0, -1), october1.AddDate(0, 0, -1), october1,
	} {
		saved, err := store.SaveReport(ctx, StoredReport{
			Day: day, Document: []byte(`{"day": "` + day.Format(time.DateOnly) + "\"}\n"), Seal: "seal-" + day.Format(time.DateOnly),
			KeyFingerprint: "fp", PreparedAt: prepared,
		})
		require.NoError(t, err)
		require.True(t, saved)
	}

	reports, err := store.ReportsBetween(ctx, october1, november1)
	require.NoError(t, err)
	require.Len(t, reports, 3)
	for i, day := range []time.Time{october1, october6, november1.AddDate(0, 0, -1)} {
		require.True(t, day.Equal(reports[i].Day), "report %d", i)
		require.Equal(t, `{"day": "`+day.Format(time.DateOnly)+"\"}\n", string(reports[i].Document), "byte for byte")
		require.Equal(t, "seal-"+day.Format(time.DateOnly), reports[i].Seal)
		require.Equal(t, "fp", reports[i].KeyFingerprint)
		require.True(t, prepared.Equal(reports[i].PreparedAt))
	}

	none, err := store.ReportsBetween(ctx, september1.AddDate(0, -1, 0), september1)
	require.NoError(t, err)
	require.Empty(t, none)
}
