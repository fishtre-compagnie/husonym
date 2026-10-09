package usagestore

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

func paris(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)
	return zone
}

// recordRuns makes one completed run of jobA per moment given, recorded at that moment, whose
// rows read are 1, 10, 100… in the order given: a sum of rows tells which runs it holds.
func recordRuns(t *testing.T, container *tcpostgres.PostgresTestContainer, store *Store, moments ...time.Time) {
	t.Helper()
	rows := int64(1)
	for _, at := range moments {
		endRun(t, container, store, at.Format(time.RFC3339), at.Add(-time.Minute), at, func(r *RunEnd) { r.RowsRead = rows })
		rows *= 10
	}
}

// The day clocks go back lasts 25 hours in Paris, from 22:00 UTC the day before to 23:00 UTC: a
// run recorded at half past midnight that day is of that day, and so is a run recorded at half
// past eleven in the evening, which a fixed offset would give to the next day.
func Test_UsageDays_FollowTheZoneWhenClocksGoBack(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	zone := paris(t)

	recordRuns(t, container, store,
		time.Date(2026, 10, 24, 21, 59, 59, 0, time.UTC), // Saturday, 23:59:59 in Paris
		time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC),  // Sunday, 00:30
		time.Date(2026, 10, 25, 22, 30, 0, 0, time.UTC),  // Sunday, 23:30
		time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC),  // Monday, 00:30
	)
	requireDaysOfChange(ctx, t, store, zone, time.October, 24)
}

// The day clocks go forward lasts 23 hours in Paris, from 23:00 UTC the day before to 22:00 UTC.
func Test_UsageDays_FollowTheZoneWhenClocksGoForward(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	zone := paris(t)

	recordRuns(t, container, store,
		time.Date(2026, 3, 28, 22, 59, 59, 0, time.UTC), // Saturday, 23:59:59 in Paris
		time.Date(2026, 3, 28, 23, 30, 0, 0, time.UTC),  // Sunday, 00:30
		time.Date(2026, 3, 29, 21, 30, 0, 0, time.UTC),  // Sunday, 23:30
		time.Date(2026, 3, 29, 22, 30, 0, 0, time.UTC),  // Monday, 00:30
	)
	requireDaysOfChange(ctx, t, store, zone, time.March, 28)
}

// requireDaysOfChange reads the four runs of recordRuns around a change of clocks: one on the
// Saturday given, two on the Sunday, one on the Monday. Every read agrees on the day of each.
func requireDaysOfChange(ctx context.Context, t *testing.T, store *Store, zone *time.Location, month time.Month, saturdayOf int) {
	t.Helper()
	saturday := CalendarDay{Year: 2026, Month: month, Day: saturdayOf}
	sunday := CalendarDay{Year: 2026, Month: month, Day: saturdayOf + 1}
	monday := CalendarDay{Year: 2026, Month: month, Day: saturdayOf + 2}
	account, job := Scope{AccountId: accountA}, Scope{AccountId: accountA, JobId: jobA}
	in := func(from, to CalendarDay) Period { return Period{From: from, To: to, Zone: zone} }

	for _, scope := range []Scope{account, job} {
		days, err := store.UsageDays(ctx, scope, in(saturday, monday))
		require.NoError(t, err)
		require.Equal(t, []UsageDay{
			{Day: saturday, RowsRead: 1, Runs: 1},
			{Day: sunday, RowsRead: 110, Runs: 2},
			{Day: monday, RowsRead: 1000, Runs: 1},
		}, days)

		for day, rows := range map[CalendarDay]int64{saturday: 1, sunday: 110, monday: 1000} {
			totals, err := store.UsageTotals(ctx, scope, in(day, day))
			require.NoError(t, err)
			require.Equal(t, rows, totals.RowsRead, "the totals of %v", day)
		}
	}

	jobs, err := store.UsageJobs(ctx, accountA, in(sunday, sunday))
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, int64(110), jobs[0].Totals.RowsRead)

	runs, err := store.LatestRuns(ctx, job, in(sunday, sunday), 10)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	require.Equal(t, int64(100), runs[0].RowsRead, "half past eleven in the evening, the last of the day")
	require.Equal(t, int64(10), runs[1].RowsRead, "half past midnight")

	// The same dates read as UTC days hold other runs: the zone is what places a run.
	utc, err := store.UsageTotals(ctx, account, utcDays(sunday, sunday))
	require.NoError(t, err)
	require.Equal(t, int64(1100), utc.RowsRead)

	// The whole month has each of its days once, and holds the four runs.
	last := time.Date(2026, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	whole, err := store.UsageDays(ctx, account, in(
		CalendarDay{Year: 2026, Month: month, Day: 1}, CalendarDay{Year: 2026, Month: month, Day: last}))
	require.NoError(t, err)
	require.Len(t, whole, last)
	var rows int64
	for i, day := range whole {
		require.Equal(t, CalendarDay{Year: 2026, Month: month, Day: i + 1}, day.Day)
		rows += day.RowsRead
	}
	require.Equal(t, int64(1111), rows)
}

// The errors of a period are the ones of the runs counted in it, in the zone as well.
func Test_UsageErrors_FollowTheZone(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	// Half past midnight on October 7 in Paris, and still October 6 in UTC.
	at := time.Date(2026, 10, 6, 22, 30, 0, 0, time.UTC)
	endRun(t, container, store, "late", at.Add(-time.Minute), at, func(r *RunEnd) {
		r.Status, r.Error = StatusFailed, RunError{Category: "timeout", Step: "preflight"}
	})

	timeout := []CategoryCount{{Category: "timeout", Count: 1}}
	for _, c := range []struct {
		name   string
		period Period
		want   []CategoryCount
	}{
		{"October 6 in UTC", utcDays(oct6, oct6), timeout},
		{"October 7 in UTC", utcDays(oct7, oct7), []CategoryCount{}},
		{"October 6 in Paris", Period{From: oct6, To: oct6, Zone: paris(t)}, []CategoryCount{}},
		{"October 7 in Paris", Period{From: oct7, To: oct7, Zone: paris(t)}, timeout},
		{"October 6 with no zone, which reads as UTC", Period{From: oct6, To: oct6}, timeout},
	} {
		errors, err := store.UsageErrors(ctx, accountA, c.period)
		require.NoError(t, err, c.name)
		require.Equal(t, c.want, errors, c.name)
	}
}

// The refusals are counted by UTC day when they happen and keep no time: the days of a period
// are read as UTC days for them, whatever the zone.
func Test_AccountRefusals_AreOfUtcDaysWhateverTheZone(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	// Half past midnight on October 7 in Paris.
	require.NoError(t, store.CountRefusal(ctx, accountA, []license.Gate{license.GateJobCap}, october6.Add(22*time.Hour+30*time.Minute)))

	sixth, err := store.AccountRefusals(ctx, accountA, Period{From: oct6, To: oct6, Zone: paris(t)})
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateJobCap, Count: 1}}, sixth)
	seventh, err := store.AccountRefusals(ctx, accountA, Period{From: oct7, To: oct7, Zone: paris(t)})
	require.NoError(t, err)
	require.Empty(t, seventh)
}

// A zone the database does not know fails the read: it is never read as another zone.
func Test_UsagePages_AZoneTheDatabaseDoesNotKnowIsAnError(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	period := Period{From: oct6, To: oct6, Zone: time.FixedZone("Nowhere/Land", 3600)}
	_, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, period)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrPeriod)
	_, err = store.UsageDays(ctx, Scope{AccountId: accountA}, period)
	require.Error(t, err)
}

// The longest period read is a leap year, in a table that holds runs of every one of its days.
func Test_UsageDays_ReadTheLongestPeriod(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	_, err := container.DB.Exec(ctx, `
		INSERT INTO husonym_api.run_usage (run_id, account_id, job_id, job_kind, status, started_at, ended_at, rows_read, recorded_at)
		SELECT 'run-' || n, $1, $2, 'sync', 'completed', at, at + interval '1 minute', 1, at + interval '1 minute'
		FROM generate_series(0, 367) AS n, LATERAL (SELECT timestamptz '2027-12-31 12:00:00+00' + n * interval '1 day' AS at) AS moment`,
		accountA, jobA)
	require.NoError(t, err)

	year := Period{
		From: CalendarDay{Year: 2028, Month: time.January, Day: 1}, To: CalendarDay{Year: 2028, Month: time.December, Day: 31},
		Zone: paris(t),
	}
	days, err := store.UsageDays(ctx, Scope{AccountId: accountA}, year)
	require.NoError(t, err)
	require.Len(t, days, MaxPeriodDays)
	require.False(t, slices.ContainsFunc(days, func(day UsageDay) bool { return day.Runs != 1 || day.RowsRead != 1 }),
		"each day holds its run, and only its own")
	totals, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, year)
	require.NoError(t, err)
	require.Equal(t, int64(MaxPeriodDays), totals.Runs)

	year.To = CalendarDay{Year: 2029, Month: time.January, Day: 1}
	_, err = store.UsageDays(ctx, Scope{AccountId: accountA}, year)
	require.ErrorIs(t, err, ErrPeriod)
}

const pagesMigration = schemaDir + "/20261012100000_reads-run-usage-by-account-and-job"

func runUsageIndexes(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer) []string {
	t.Helper()
	rows, err := container.DB.Query(ctx,
		`SELECT indexname FROM pg_indexes WHERE schemaname = 'husonym_api' AND tablename = 'run_usage' ORDER BY indexname`)
	require.NoError(t, err)
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

var (
	// No index by account: the runs of an account are read by the moment they were recorded.
	indexesOfThePages = []string{
		"run_usage_job_id_recorded_at_idx",
		"run_usage_pkey",
		"run_usage_recorded_at_idx",
		"run_usage_status_started_at_idx",
	}
	indexesBeforeThePages = []string{
		"run_usage_account_id_ended_at_idx",
		"run_usage_pkey",
		"run_usage_recorded_at_idx",
		"run_usage_status_started_at_idx",
	}
)

func Test_RunUsage_HasTheIndexesThePagesReadBy(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, _ := migratedDatabase(ctx, t)

	require.Equal(t, indexesOfThePages, runUsageIndexes(ctx, t, container))
}

// The migration goes down to the indexes of before, up again and up once more, and touches
// neither a row nor a column on the way.
func Test_PagesMigration_GoesDownAndUpAgain(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)

	columns := func() int {
		var n int
		require.NoError(t, container.DB.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
			WHERE table_schema = 'husonym_api' AND table_name = 'run_usage'`).Scan(&n))
		return n
	}
	before := columns()
	want, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, utcDays(oct5, oct6))
	require.NoError(t, err)

	execMigrationFile(ctx, t, container, pagesMigration+".down.sql")
	require.Equal(t, indexesBeforeThePages, runUsageIndexes(ctx, t, container))
	require.Equal(t, 8, countRows(ctx, t, container, "run_usage"))
	require.Equal(t, before, columns())
	// The reads do not need the indexes to be right.
	got, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, utcDays(oct5, oct6))
	require.NoError(t, err)
	require.Equal(t, want, got)

	// Down twice changes nothing more.
	execMigrationFile(ctx, t, container, pagesMigration+".down.sql")
	require.Equal(t, indexesBeforeThePages, runUsageIndexes(ctx, t, container))

	for range 2 {
		execMigrationFile(ctx, t, container, pagesMigration+".up.sql")
		require.Equal(t, indexesOfThePages, runUsageIndexes(ctx, t, container))
		require.Equal(t, 8, countRows(ctx, t, container, "run_usage"))
		require.Equal(t, before, columns())
	}
}
