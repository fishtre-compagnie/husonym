package usagestore

import (
	"os"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

const accountB = "00000000-0000-0000-0000-00000000000b"

var october6 = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)

// endRun ends a run and places it on the day of its end. The API records an end at the moment
// it hears of it, on the clock of the database: a test chooses the day by moving that moment.
func endRun(
	t *testing.T,
	container *tcpostgres.PostgresTestContainer,
	store *Store,
	runId string,
	startedAt, endedAt time.Time,
	mutate func(*RunEnd),
) {
	t.Helper()
	end := RunEnd{
		RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: startedAt, EndedAt: endedAt, Status: StatusCompleted,
	}
	if mutate != nil {
		mutate(&end)
	}
	require.NoError(t, store.RunEnded(t.Context(), end))
	recordOn(t, container, runId, endedAt)
}

// recordOn moves the moment the end of a run was recorded.
func recordOn(t *testing.T, container *tcpostgres.PostgresTestContainer, runId string, at time.Time) {
	t.Helper()
	_, err := container.DB.Exec(t.Context(),
		`UPDATE husonym_api.run_usage SET recorded_at = $2 WHERE run_id = $1`, runId, at)
	require.NoError(t, err)
}

// recordedAt reads the moment the end of a run was recorded, nil when there is none.
func recordedAt(t *testing.T, container *tcpostgres.PostgresTestContainer, runId string) *time.Time {
	t.Helper()
	var at *time.Time
	require.NoError(t, container.DB.QueryRow(t.Context(),
		`SELECT recorded_at FROM husonym_api.run_usage WHERE run_id = $1`, runId).Scan(&at))
	return at
}

// databaseNow reads the clock the store records with.
func databaseNow(t *testing.T, container *tcpostgres.PostgresTestContainer) time.Time {
	t.Helper()
	var at time.Time
	require.NoError(t, container.DB.QueryRow(t.Context(), `SELECT CURRENT_TIMESTAMP`).Scan(&at))
	return at
}

func Test_RunsOfDay_CountsARunForTheUtcDayItsEndWasRecorded(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	container, store := migratedDatabase(t.Context(), t)

	late := october6.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	midnight := october6.Add(24 * time.Hour)
	endRun(t, container, store, "late", late.Add(-time.Minute), late, func(r *RunEnd) { r.RowsRead = 10 })
	endRun(t, container, store, "early", midnight.Add(-time.Minute), midnight, func(r *RunEnd) {
		r.Status, r.RowsRead = StatusFailed, 7
	})

	first, err := store.RunsOfDay(t.Context(), october6)
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusCompleted, Count: 1}}, first.ByStatus)
	require.Equal(t, int64(10), first.RowsRead)

	// Any time of the day names the day.
	second, err := store.RunsOfDay(t.Context(), october6.Add(30*time.Hour))
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusFailed, Count: 1}}, second.ByStatus)
	require.Equal(t, int64(7), second.RowsRead)

	none, err := store.RunsOfDay(t.Context(), october6.Add(-24*time.Hour))
	require.NoError(t, err)
	require.Empty(t, none.ByStatus)
	require.Nil(t, none.DurationMedian)
	require.Nil(t, none.DurationP95)
}

// A run that ended on a day and whose end reached the API the day after belongs to the day
// after: the day before is closed, and its report may already be made.
func Test_RunsOfDay_AnEndLearnedTheDayAfterCountsForTheDayAfter(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	ended := october6.Add(23 * time.Hour)
	learned := october6.Add(25 * time.Hour)
	for _, runId := range []string{"told", "closed", "settled"} {
		require.NoError(t, store.RunStarted(ctx, RunStart{
			RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: ended.Add(-time.Minute),
		}))
	}
	require.NoError(t, store.RunEnded(ctx, RunEnd{
		RunId: "told", AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: ended.Add(-time.Minute), EndedAt: ended, Status: StatusCompleted, SourceVersionMajor: "16",
	}))
	require.NoError(t, store.CloseRun(ctx, "closed", StatusCompleted, ended, 0, 0, 0, 0, "16", RunError{}))
	require.NoError(t, store.Settle(ctx, "settled", StatusCompleted, &ended))
	for _, runId := range []string{"told", "closed", "settled"} {
		recordOn(t, container, runId, learned)
	}

	before, err := store.RunsOfDay(ctx, october6)
	require.NoError(t, err)
	require.Empty(t, before.ByStatus)
	versionsBefore, err := store.SourceVersionsOfDay(ctx, october6)
	require.NoError(t, err)
	require.Empty(t, versionsBefore)

	after, err := store.RunsOfDay(ctx, learned)
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusCompleted, Count: 3}}, after.ByStatus)
	require.NotNil(t, after.DurationMedian, "the duration is still the one of the run")
	require.Equal(t, int64(60), *after.DurationMedian)
	versionsAfter, err := store.SourceVersionsOfDay(ctx, learned)
	require.NoError(t, err)
	require.Equal(t, []SourceEngineRuns{{JobId: jobA, VersionMajor: "16", Runs: 2}}, versionsAfter)
}

// Each of the three ways a run ends records the moment on the clock of the database, and an
// end told again does not move it.
func Test_EndingARun_RecordsTheMomentOnceOnly(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	started := october6
	ended := october6.Add(time.Minute)
	end := func(runId string) RunEnd {
		return RunEnd{
			RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync,
			StartedAt: started, EndedAt: ended, Status: StatusCompleted,
		}
	}
	for _, runId := range []string{"upserted", "closed", "settled", "running"} {
		require.NoError(t, store.RunStarted(ctx, RunStart{
			RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: started,
		}))
		require.Nil(t, recordedAt(t, container, runId), "a run still running has no such moment")
	}

	from := databaseNow(t, container)
	require.NoError(t, store.RunEnded(ctx, end("upserted")))
	require.NoError(t, store.RunEnded(ctx, end("unstarted")))
	require.NoError(t, store.CloseRun(ctx, "closed", StatusFailed, ended, 0, 0, 0, 0, "", RunError{}))
	require.NoError(t, store.Settle(ctx, "settled", StatusTerminated, nil))
	to := databaseNow(t, container)

	ends := []string{"upserted", "unstarted", "closed", "settled"}
	for _, runId := range ends {
		at := recordedAt(t, container, runId)
		require.NotNil(t, at, runId)
		require.False(t, at.Before(from), runId)
		require.False(t, at.After(to), runId)
	}
	require.Nil(t, recordedAt(t, container, "running"))

	moved := october6.Add(-48 * time.Hour)
	for _, runId := range ends {
		recordOn(t, container, runId, moved)
		require.NoError(t, store.RunEnded(ctx, end(runId)))
		require.NoError(t, store.CloseRun(ctx, runId, StatusFailed, ended, 0, 0, 0, 0, "", RunError{}))
		require.NoError(t, store.Settle(ctx, runId, StatusTerminated, nil))
		at := recordedAt(t, container, runId)
		require.NotNil(t, at, runId)
		require.True(t, moved.Equal(*at), "a second end of %s moved the moment", runId)
	}
}

func Test_RunsOfDay_ARunSettledWithoutAnEndCountsTheDayItWasSettled(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	started := october6
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "lost", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: started,
	}))
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "running", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: started,
	}))
	require.NoError(t, store.Settle(ctx, "lost", StatusTerminated, nil))

	// The day is the one the row holds: the clock of the test and the one of the database may
	// stand on either side of midnight.
	settledOn := recordedAt(t, container, "lost")
	require.NotNil(t, settledOn)
	runs, err := store.RunsOfDay(ctx, *settledOn)
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusTerminated, Count: 1}}, runs.ByStatus)
	require.Nil(t, runs.DurationMedian, "a run with no end has no duration")
	require.Nil(t, runs.DurationP95)
}

// An end told before its start is a clock oddity: it must not make a negative duration, which
// the schema of the report refuses.
func Test_RunsOfDay_ADurationIsNeverNegative(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	container, store := migratedDatabase(t.Context(), t)

	ended := october6.Add(12 * time.Hour)
	endRun(t, container, store, "backwards", ended.Add(10*time.Minute), ended, nil)

	runs, err := store.RunsOfDay(t.Context(), october6)
	require.NoError(t, err)
	require.NotNil(t, runs.DurationMedian)
	require.NotNil(t, runs.DurationP95)
	require.Zero(t, *runs.DurationMedian)
	require.Zero(t, *runs.DurationP95)
}

func Test_RunsOfDay_DurationsOfKnownRuns(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	container, store := migratedDatabase(t.Context(), t)

	ended := october6.Add(12 * time.Hour)
	for i, seconds := range []int{60, 120, 600} {
		endRun(t, container, store, string(rune('a'+i)), ended.Add(-time.Duration(seconds)*time.Second), ended, nil)
	}
	runs, err := store.RunsOfDay(t.Context(), october6)
	require.NoError(t, err)
	require.NotNil(t, runs.DurationMedian)
	require.NotNil(t, runs.DurationP95)
	require.Equal(t, int64(120), *runs.DurationMedian)
	require.Equal(t, int64(552), *runs.DurationP95)
}

func Test_RunsOfDay_CountsUncountedRowsAndAddsUp(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	container, store := migratedDatabase(t.Context(), t)

	ended := october6.Add(12 * time.Hour)
	endRun(t, container, store, "a", ended.Add(-time.Minute), ended, func(r *RunEnd) {
		r.RowsRead, r.RowsDiscarded, r.Retries, r.TablesUncounted = 100, 4, 2, 3
	})
	endRun(t, container, store, "b", ended.Add(-time.Minute), ended, func(r *RunEnd) {
		r.RowsRead, r.RowsDiscarded, r.Retries = 50, 1, 1
	})
	runs, err := store.RunsOfDay(t.Context(), october6)
	require.NoError(t, err)
	require.Equal(t, int64(1), runs.WithUncountedRows)
	require.Equal(t, int64(150), runs.RowsRead)
	require.Equal(t, int64(5), runs.RowsDiscarded)
	require.Equal(t, int64(3), runs.Retries)
}

func Test_SourceVersionsOfDay_PerJobAndVersionWhereKnown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	ended := october6.Add(12 * time.Hour)
	for runId, version := range map[string]string{"a": "16", "b": "16", "c": "15", "d": ""} {
		endRun(t, container, store, runId, ended.Add(-time.Minute), ended, func(r *RunEnd) { r.SourceVersionMajor = version })
	}
	require.NoError(t, store.CloseRun(ctx, "unknown", StatusCompleted, ended, 0, 0, 0, 0, "9", RunError{}))

	versions, err := store.SourceVersionsOfDay(ctx, october6)
	require.NoError(t, err)
	require.Equal(t, []SourceEngineRuns{
		{JobId: jobA, VersionMajor: "15", Runs: 1},
		{JobId: jobA, VersionMajor: "16", Runs: 2},
	}, versions)
}

func Test_RefusalsOfDay_SumsAccounts(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	at := october6.Add(10 * time.Hour)
	require.NoError(t, store.CountRefusal(ctx, accountA, []license.Gate{license.GateJobCap}, at))
	require.NoError(t, store.CountRefusal(ctx, accountB, []license.Gate{license.GateJobCap, license.GateSourceCap}, at))
	require.NoError(t, store.CountRefusal(ctx, accountA, []license.Gate{license.GateJobCap}, at.Add(24*time.Hour)))

	counts, err := store.RefusalsOfDay(ctx, october6)
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateJobCap, Count: 2}, {Gate: license.GateSourceCap, Count: 1}}, counts)
}

func Test_UserSeen_NeverMovesTheDateBack(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	const user = "00000000-0000-0000-0000-0000000000c1"
	lastSeen := func() string {
		var day string
		require.NoError(t, container.DB.QueryRow(ctx,
			`SELECT last_seen_on::text FROM husonym_api.user_activity WHERE user_id = $1`, user).Scan(&day))
		return day
	}
	require.NoError(t, store.UserSeen(ctx, user, october6.Add(5*time.Hour)))
	require.NoError(t, store.UserSeen(ctx, user, october6.Add(20*time.Hour)))
	require.Equal(t, "2026-10-06", lastSeen())
	require.NoError(t, store.UserSeen(ctx, user, october6.Add(-48*time.Hour)))
	require.Equal(t, "2026-10-06", lastSeen())
	require.NoError(t, store.UserSeen(ctx, user, october6.Add(24*time.Hour)))
	require.Equal(t, "2026-10-07", lastSeen())
	require.Equal(t, 1, countRows(ctx, t, container, "user_activity"))
	require.Error(t, store.UserSeen(ctx, "not-a-uuid", october6))
}

func Test_UsersSeenSince_CountsFromTheDayOn(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	require.NoError(t, store.UserSeen(ctx, "00000000-0000-0000-0000-0000000000c1", october6.Add(-48*time.Hour)))
	require.NoError(t, store.UserSeen(ctx, "00000000-0000-0000-0000-0000000000c2", october6))
	require.NoError(t, store.UserSeen(ctx, "00000000-0000-0000-0000-0000000000c3", october6.Add(24*time.Hour)))

	since, err := store.UsersSeenSince(ctx, october6.Add(7*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(2), since)
	all, err := store.UsersSeenSince(ctx, october6.Add(-72*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(3), all)
}

func Test_SaveReport_TheFirstOfADayStays(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	missing, err := store.Report(ctx, october6)
	require.NoError(t, err)
	require.Nil(t, missing)

	prepared := october6.Add(24*time.Hour + 5*time.Minute)
	first := StoredReport{
		Day: october6, Document: []byte("{\"a\": 1}\n"), Seal: "seal-1", KeyFingerprint: "fp", PreparedAt: prepared,
	}
	saved, err := store.SaveReport(ctx, first)
	require.NoError(t, err)
	require.True(t, saved)

	second := first
	second.Document, second.Seal = []byte("{\"a\": 2}"), "seal-2"
	saved, err = store.SaveReport(ctx, second)
	require.NoError(t, err)
	require.False(t, saved)

	got, err := store.Report(ctx, october6.Add(3*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, october6.Equal(got.Day))
	require.Equal(t, []byte("{\"a\": 1}\n"), got.Document, "the document comes back byte for byte")
	require.Equal(t, "seal-1", got.Seal)
	require.Equal(t, "fp", got.KeyFingerprint)
	require.True(t, prepared.Equal(got.PreparedAt))
}

func Test_DeleteReportsBefore_KeepsTheDayItself(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	for i := range 3 {
		saved, err := store.SaveReport(ctx, StoredReport{
			Day: october6.Add(time.Duration(i) * 24 * time.Hour), Document: []byte("{}"), Seal: "s",
			KeyFingerprint: "fp", PreparedAt: time.Now(),
		})
		require.NoError(t, err)
		require.True(t, saved)
	}
	require.NoError(t, store.DeleteReportsBefore(ctx, october6.Add(24*time.Hour)))

	gone, err := store.Report(ctx, october6)
	require.NoError(t, err)
	require.Nil(t, gone)
	for i := 1; i < 3; i++ {
		kept, err := store.Report(ctx, october6.Add(time.Duration(i)*24*time.Hour))
		require.NoError(t, err)
		require.NotNil(t, kept)
	}
}

func Test_ReportsMigrationDown_LeavesTheFirstUsageMigration(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, _ := migratedDatabase(ctx, t)

	count := func(query string, names ...string) int {
		var n int
		require.NoError(t, container.DB.QueryRow(ctx, query, names).Scan(&n))
		return n
	}
	tables := func(names ...string) int {
		return count(`SELECT count(*) FROM information_schema.tables
			WHERE table_schema = 'husonym_api' AND table_name = ANY($1)`, names...)
	}
	columns := func(names ...string) int {
		return count(`SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'husonym_api' AND table_name = 'run_usage' AND column_name = ANY($1)`, names...)
	}
	added := []string{"recorded_at", "tables_uncounted", "source_version_major"}
	require.Equal(t, 2, tables("user_activity", "usage_reports"))
	require.Equal(t, 3, columns(added...))

	down, err := os.ReadFile(schemaDir + "/20261008100000_adds-usage-reports.down.sql")
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx, string(down))
	require.NoError(t, err)

	require.Zero(t, tables("user_activity", "usage_reports"))
	require.Zero(t, columns(added...))
	require.Equal(t, 3, tables("instance", "run_usage", "gate_refusals_daily"), "the first usage migration stays")
	require.Equal(t, 2, columns("run_id", "retries"), "the columns of the first migration stay")
}

// The runs that ended before the migration get the moment their end is known for, or the moment
// of the migration when they have no end; a run still running gets none.
func Test_ReportsMigration_GivesTheRunsAlreadyEndedTheirMoment(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	down, err := os.ReadFile(schemaDir + "/20261008100000_adds-usage-reports.down.sql")
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx, string(down))
	require.NoError(t, err)

	ended := october6.Add(12 * time.Hour)
	for runId, row := range map[string]struct {
		status  string
		endedAt *time.Time
	}{
		"ended":   {"completed", &ended},
		"lost":    {"terminated", nil},
		"running": {"running", nil},
	} {
		_, err = container.DB.Exec(ctx,
			`INSERT INTO husonym_api.run_usage (run_id, account_id, job_id, job_kind, status, started_at, ended_at)
			 VALUES ($1, $2, $3, 'sync', $4, $5, $6)`,
			runId, accountA, jobA, row.status, ended.Add(-time.Minute), row.endedAt)
		require.NoError(t, err)
	}

	from := databaseNow(t, container)
	up, err := os.ReadFile(schemaDir + "/20261008100000_adds-usage-reports.up.sql")
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx, string(up))
	require.NoError(t, err)

	at := recordedAt(t, container, "ended")
	require.NotNil(t, at)
	require.True(t, ended.Equal(*at))
	lost := recordedAt(t, container, "lost")
	require.NotNil(t, lost)
	require.False(t, lost.Before(from))
	require.Nil(t, recordedAt(t, container, "running"))

	runs, err := store.RunsOfDay(ctx, october6)
	require.NoError(t, err)
	require.Equal(t, []RunCount{{Kind: JobKindSync, Status: StatusCompleted, Count: 1}}, runs.ByStatus)
}
