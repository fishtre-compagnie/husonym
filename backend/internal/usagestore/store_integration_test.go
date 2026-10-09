package usagestore

import (
	"context"
	"os"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	accountA = "00000000-0000-0000-0000-00000000000a"
	jobA     = "00000000-0000-0000-0000-0000000000a1"
)

const schemaDir = "../../sql/postgresql/schema"

// migratedDatabase starts a PostgreSQL holding the schema of the API, and a store on it.
func migratedDatabase(ctx context.Context, t *testing.T) (*tcpostgres.PostgresTestContainer, *Store) {
	t.Helper()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, schemaDir, testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return container, New(husonymdb.New(pool, db_queries.New()))
}

type runRow struct {
	status                           string
	endedAt                          *time.Time
	rowsRead, rowsDiscarded, retries int64
	errorCategory, errorStep         *string
}

// pairOf is the category and the step a row holds, "" for the one it does not hold.
func (r runRow) pairOf() [2]string {
	var pair [2]string
	if r.errorCategory != nil {
		pair[0] = *r.errorCategory
	}
	if r.errorStep != nil {
		pair[1] = *r.errorStep
	}
	return pair
}

func readRun(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, runId string) runRow {
	t.Helper()
	var row runRow
	require.NoError(t, container.DB.QueryRow(ctx,
		`SELECT status, ended_at, rows_read, rows_discarded, retries, error_category, error_step
		 FROM husonym_api.run_usage WHERE run_id = $1`, runId).
		Scan(&row.status, &row.endedAt, &row.rowsRead, &row.rowsDiscarded, &row.retries,
			&row.errorCategory, &row.errorStep))
	return row
}

func countRows(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, table string) int {
	t.Helper()
	var n int
	require.NoError(t, container.DB.QueryRow(ctx, "SELECT count(*) FROM husonym_api."+table).Scan(&n))
	return n
}

func Test_InstanceId_IsTheSameEachTime(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	first, err := store.InstanceId(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	second, err := store.InstanceId(ctx)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func Test_RunStarted_TwiceLeavesOneRow(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	start := RunStart{RunId: "run-1", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: time.Now().UTC()}
	require.NoError(t, store.RunStarted(ctx, start))
	require.NoError(t, store.RunStarted(ctx, start))
	require.Equal(t, 1, countRows(ctx, t, container, "run_usage"))
	require.Equal(t, "running", readRun(ctx, t, container, "run-1").status)
}

func Test_RunEnded_WithoutAStartMakesAFinishedRow(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	ended := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, store.RunEnded(ctx, RunEnd{
		RunId: "run-1", AccountId: accountA, JobId: jobA, Kind: JobKindGenerate,
		StartedAt: ended.Add(-time.Minute), EndedAt: ended, Status: StatusCompleted,
		RowsRead: 10, RowsDiscarded: 2, Retries: 1,
	}))
	require.Equal(t, 1, countRows(ctx, t, container, "run_usage"))
	row := readRun(ctx, t, container, "run-1")
	require.Equal(t, "completed", row.status)
	require.NotNil(t, row.endedAt)
	require.True(t, ended.Equal(*row.endedAt))
	require.Equal(t, int64(10), row.rowsRead)
	require.Equal(t, int64(2), row.rowsDiscarded)
	require.Equal(t, int64(1), row.retries)
}

func Test_RunEnded_AfterAStartFinishesTheRow(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	now := time.Now().UTC()
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "run-1", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: now,
	}))
	require.NoError(t, store.RunEnded(ctx, RunEnd{
		RunId: "run-1", AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: now, EndedAt: now.Add(time.Second), Status: StatusFailed, RowsRead: 5,
	}))
	row := readRun(ctx, t, container, "run-1")
	require.Equal(t, "failed", row.status)
	require.Equal(t, int64(5), row.rowsRead)
}

func Test_RunEnded_TwiceTheFirstWins(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	now := time.Now().UTC()
	end := RunEnd{
		RunId: "run-1", AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: now, EndedAt: now.Add(time.Second), Status: StatusCompleted, RowsRead: 5, Retries: 1,
	}
	require.NoError(t, store.RunEnded(ctx, end))
	end.Status, end.RowsRead, end.Retries = StatusFailed, 99, 7
	require.NoError(t, store.RunEnded(ctx, end))

	row := readRun(ctx, t, container, "run-1")
	require.Equal(t, "completed", row.status)
	require.Equal(t, int64(5), row.rowsRead)
	require.Equal(t, int64(1), row.retries)
}

func Test_Settle_OnlyTouchesARunStillOpen(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "open", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: now,
	}))
	require.NoError(t, store.RunEnded(ctx, RunEnd{
		RunId: "done", AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: now, EndedAt: now, Status: StatusCompleted,
	}))

	ended := now.Add(time.Minute)
	require.NoError(t, store.Settle(ctx, "open", StatusTimedOut, &ended))
	require.NoError(t, store.Settle(ctx, "done", StatusTerminated, nil))
	require.NoError(t, store.Settle(ctx, "unknown", StatusTerminated, nil))

	open := readRun(ctx, t, container, "open")
	require.Equal(t, "timed_out", open.status)
	require.True(t, ended.Equal(*open.endedAt))
	done := readRun(ctx, t, container, "done")
	require.Equal(t, "completed", done.status)
	require.NotNil(t, done.endedAt)
	require.Equal(t, 2, countRows(ctx, t, container, "run_usage"))

	// Settled without a known end: the end stays empty.
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "lost", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: now,
	}))
	require.NoError(t, store.Settle(ctx, "lost", StatusTerminated, nil))
	lost := readRun(ctx, t, container, "lost")
	require.Equal(t, "terminated", lost.status)
	require.Nil(t, lost.endedAt)
}

func Test_CloseRun_ClosesOnlyARunningRowThatExists(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "open", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: now,
	}))
	require.NoError(t, store.RunEnded(ctx, RunEnd{
		RunId: "done", AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: now, EndedAt: now, Status: StatusCompleted, RowsRead: 5,
	}))

	ended := now.Add(time.Minute)
	require.NoError(t, store.CloseRun(ctx, "open", StatusFailed, ended, 120, 3, 2, 0, "", RunError{}))
	require.NoError(t, store.CloseRun(ctx, "done", StatusFailed, ended, 999, 9, 9, 0, "", RunError{}))
	require.NoError(t, store.CloseRun(ctx, "unknown", StatusCompleted, ended, 1, 1, 1, 0, "", RunError{}))

	open := readRun(ctx, t, container, "open")
	require.Equal(t, "failed", open.status)
	require.True(t, ended.Equal(*open.endedAt))
	require.Equal(t, int64(120), open.rowsRead)
	require.Equal(t, int64(3), open.rowsDiscarded)
	require.Equal(t, int64(2), open.retries)
	done := readRun(ctx, t, container, "done")
	require.Equal(t, "completed", done.status)
	require.Equal(t, int64(5), done.rowsRead)
	require.Equal(t, 2, countRows(ctx, t, container, "run_usage"))
}

func Test_OpenRunsStartedBefore_OnlyOldRunningOnes(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	now := time.Now().UTC().Truncate(time.Second)
	for _, run := range []RunStart{
		{RunId: "old-open", StartedAt: now.Add(-2 * time.Hour)},
		{RunId: "old-done", StartedAt: now.Add(-2 * time.Hour)},
		{RunId: "recent-open", StartedAt: now},
	} {
		run.AccountId, run.JobId, run.Kind = accountA, jobA, JobKindSync
		require.NoError(t, store.RunStarted(ctx, run))
	}
	require.NoError(t, store.Settle(ctx, "old-done", StatusCompleted, &now))

	open, err := store.OpenRunsStartedBefore(ctx, now.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, open, 1)
	require.Equal(t, "old-open", open[0].RunId)
	require.Equal(t, accountA, open[0].AccountId)
	require.True(t, now.Add(-2*time.Hour).Equal(open[0].StartedAt))
}

func Test_CountRefusal_CountsPerGatePerUtcDay(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	// 23:30 at UTC-5 is already the next day at UTC.
	at := time.Date(2026, 10, 7, 23, 30, 0, 0, time.FixedZone("west", -5*3600))
	gates := []license.Gate{license.GateJobCap, license.GateSourceCap}
	require.NoError(t, store.CountRefusal(ctx, accountA, gates, at))
	require.NoError(t, store.CountRefusal(ctx, accountA, gates[:1], at))
	require.NoError(t, store.CountRefusal(ctx, accountA, gates[:1], at.Add(24*time.Hour)))

	counts := map[string]int64{}
	rows, err := container.DB.Query(ctx,
		`SELECT day::text || '/' || gate, count FROM husonym_api.gate_refusals_daily`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		require.NoError(t, rows.Scan(&key, &count))
		counts[key] = count
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]int64{
		"2026-10-08/job_cap":    2,
		"2026-10-08/source_cap": 1,
		"2026-10-09/job_cap":    1,
	}, counts)
}

func Test_CountRefusal_AnUnknownGateLeavesNoRow(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	require.NoError(t, store.CountRefusal(ctx, accountA, []license.Gate{"made_up"}, time.Now()))
	require.Zero(t, countRows(ctx, t, container, "gate_refusals_daily"))
}

func Test_MigrationDown_RemovesTheThreeTablesOnly(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, _ := migratedDatabase(ctx, t)

	tables := func(names ...string) int {
		var n int
		require.NoError(t, container.DB.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables
			 WHERE table_schema = 'husonym_api' AND table_name = ANY($1)`, names).Scan(&n))
		return n
	}
	usage := []string{"instance", "run_usage", "gate_refusals_daily"}
	require.Equal(t, 3, tables(usage...))

	// The down file of this migration alone, not the whole chain of them.
	down, err := os.ReadFile(schemaDir + "/20261007100000_adds-usage.down.sql")
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx, string(down))
	require.NoError(t, err)

	require.Zero(t, tables(usage...))
	require.Equal(t, 1, tables("license_keys"), "an earlier migration is untouched")
}

// Every run that did not complete holds one category and one step, whichever way the API
// learned of its end; a run that completed holds none, whatever is told with it.
func Test_RunEnded_KeepsOnePairForEveryRunThatDidNotComplete(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	now := time.Now().UTC().Truncate(time.Second)
	end := func(runId string, status Status, told RunError) {
		t.Helper()
		require.NoError(t, store.RunEnded(ctx, RunEnd{
			RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync,
			StartedAt: now, EndedAt: now.Add(time.Second), Status: status, Error: told,
		}))
	}
	start := func(runId string) {
		t.Helper()
		require.NoError(t, store.RunStarted(ctx, RunStart{
			RunId: runId, AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: now,
		}))
	}

	end("failed-told", StatusFailed, RunError{Category: "constraint_violated", Step: "table_sync"})
	end("failed-untold", StatusFailed, RunError{})
	end("canceled", StatusCanceled, RunError{})
	end("completed", StatusCompleted, RunError{Category: "timeout", Step: "hooks"})
	for _, runId := range []string{"timed-out", "terminated", "closed", "running"} {
		start(runId)
	}
	ended := now.Add(time.Minute)
	require.NoError(t, store.Settle(ctx, "timed-out", StatusTimedOut, &ended))
	require.NoError(t, store.Settle(ctx, "terminated", StatusTerminated, nil))
	require.NoError(t, store.CloseRun(
		ctx, "closed", StatusFailed, ended, 0, 0, 0, 0, "", RunError{Category: "object_missing", Step: "schema_init"},
	))

	for runId, want := range map[string][2]string{
		"failed-told":   {"constraint_violated", "table_sync"},
		"failed-untold": {"other", "other"},
		"canceled":      {"canceled", "other"},
		"timed-out":     {"timeout", "other"},
		"terminated":    {"other", "other"},
		"closed":        {"object_missing", "schema_init"},
	} {
		require.Equal(t, want, readRun(ctx, t, container, runId).pairOf(), runId)
	}
	for _, runId := range []string{"completed", "running"} {
		row := readRun(ctx, t, container, runId)
		require.Nil(t, row.errorCategory, runId)
		require.Nil(t, row.errorStep, runId)
	}
}

// The first end told wins for the error as for the rest: neither a second end nor a settlement
// moves the category or the step.
func Test_RunEnded_TwiceTheFirstErrorWins(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	now := time.Now().UTC()
	end := RunEnd{
		RunId: "run-1", AccountId: accountA, JobId: jobA, Kind: JobKindSync,
		StartedAt: now, EndedAt: now.Add(time.Second), Status: StatusFailed,
		Error: RunError{Category: "timeout", Step: "preflight"},
	}
	require.NoError(t, store.RunEnded(ctx, end))
	end.Error = RunError{Category: "license", Step: "hooks"}
	require.NoError(t, store.RunEnded(ctx, end))
	require.Equal(t, [2]string{"timeout", "preflight"}, readRun(ctx, t, container, "run-1").pairOf())

	require.NoError(t, store.CloseRun(
		ctx, "run-1", StatusCanceled, now, 0, 0, 0, 0, "", RunError{Category: "license", Step: "hooks"},
	))
	require.NoError(t, store.Settle(ctx, "run-1", StatusTimedOut, nil))
	row := readRun(ctx, t, container, "run-1")
	require.Equal(t, "failed", row.status)
	require.Equal(t, [2]string{"timeout", "preflight"}, row.pairOf())
}

const errorsMigration = schemaDir + "/20261011100000_adds-run-usage-errors"

// execMigrationFile runs one file of a migration alone, not the whole chain of them.
func execMigrationFile(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, path string) {
	t.Helper()
	statements, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx, string(statements))
	require.NoError(t, err)
}

// The runs that did not complete before the migration get the pair their status alone gives;
// a run that completed or still runs gets none. Run again, the migration changes nothing.
func Test_ErrorsMigration_GivesTheRunsAlreadyEndedTheirPair(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, _ := migratedDatabase(ctx, t)
	execMigrationFile(ctx, t, container, errorsMigration+".down.sql")

	for _, status := range Statuses() {
		_, err := container.DB.Exec(ctx,
			`INSERT INTO husonym_api.run_usage (run_id, account_id, job_id, job_kind, status, started_at)
			 VALUES ($1, $2, $3, 'sync', $1, CURRENT_TIMESTAMP)`,
			string(status), accountA, jobA)
		require.NoError(t, err)
	}

	for range 2 {
		execMigrationFile(ctx, t, container, errorsMigration+".up.sql")

		for status, want := range map[Status][2]string{
			StatusFailed:     {"other", "other"},
			StatusCanceled:   {"canceled", "other"},
			StatusTerminated: {"other", "other"},
			StatusTimedOut:   {"timeout", "other"},
		} {
			require.Equal(t, want, readRun(ctx, t, container, string(status)).pairOf(), status)
		}
		for _, status := range []Status{StatusRunning, StatusCompleted} {
			row := readRun(ctx, t, container, string(status))
			require.Nil(t, row.errorCategory, status)
			require.Nil(t, row.errorStep, status)
		}
	}

	// The table refuses what is outside the lists, and a category without its step.
	for name, set := range map[string]string{
		"a category outside the list": `error_category = 'deadlock', error_step = 'other'`,
		"a step outside the list":     `error_category = 'other', error_step = 'somewhere'`,
		"a category without its step": `error_category = 'other', error_step = NULL`,
		"a step without its category": `error_category = NULL, error_step = 'other'`,
	} {
		_, err := container.DB.Exec(ctx, `UPDATE husonym_api.run_usage SET `+set+` WHERE run_id = 'failed'`)
		require.Error(t, err, name)
	}
}

func Test_ErrorsMigrationDown_LeavesTheUsageTables(t *testing.T) {
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
	constraints := func(names ...string) int {
		return count(`SELECT count(*) FROM information_schema.table_constraints
			WHERE table_schema = 'husonym_api' AND table_name = 'run_usage' AND constraint_name = ANY($1)`, names...)
	}
	added := []string{"error_category", "error_step"}
	checks := []string{"run_usage_error_category_known", "run_usage_error_step_known", "run_usage_error_whole"}
	kept := []string{"run_id", "status", "retries", "recorded_at", "tables_uncounted", "source_version_major"}
	require.Equal(t, 2, columns(added...))
	require.Equal(t, 3, constraints(checks...))

	execMigrationFile(ctx, t, container, errorsMigration+".down.sql")

	require.Zero(t, columns(added...))
	require.Zero(t, constraints(checks...))
	require.Equal(t, len(kept), columns(kept...), "the columns of the earlier migrations stay")
	require.Equal(t, 2, constraints("run_usage_job_kind_known", "run_usage_status_known"))
	require.Equal(t, 5, tables("instance", "run_usage", "gate_refusals_daily", "user_activity", "usage_reports"))
}
