package usagestore

import (
	"context"
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
}

func readRun(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, runId string) runRow {
	t.Helper()
	var row runRow
	require.NoError(t, container.DB.QueryRow(ctx,
		`SELECT status, ended_at, rows_read, rows_discarded, retries
		 FROM husonym_api.run_usage WHERE run_id = $1`, runId).
		Scan(&row.status, &row.endedAt, &row.rowsRead, &row.rowsDiscarded, &row.retries))
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

func Test_MigrationDown_RemovesTheThreeTables(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, _ := migratedDatabase(ctx, t)

	tables := func() int {
		var n int
		require.NoError(t, container.DB.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables
			 WHERE table_schema = 'husonym_api'
			   AND table_name IN ('instance', 'run_usage', 'gate_refusals_daily')`).Scan(&n))
		return n
	}
	require.Equal(t, 3, tables())
	require.NoError(t, neomigrate.Down(ctx, container.URL, schemaDir, testutil.GetTestLogger(t)))
	require.Zero(t, tables())
}
