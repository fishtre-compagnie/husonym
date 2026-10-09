package usagestore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// recordEveryHalfHour empties the runs, then records one run of account A and one of account B
// every half hour, from four days before the day given at midnight UTC to four days after it. One
// run of A in three is of jobA2, the others of jobA; one in five failed; each read one row.
func recordEveryHalfHour(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, around CalendarDay) {
	t.Helper()
	_, err := container.DB.Exec(ctx, `DELETE FROM husonym_api.run_usage`)
	require.NoError(t, err)
	first := around.date().Time.AddDate(0, 0, -4)
	_, err = container.DB.Exec(ctx, `
		INSERT INTO husonym_api.run_usage (run_id, account_id, job_id, job_kind, status, started_at, ended_at, rows_read, recorded_at)
		SELECT owner || '-' || n,
			CASE WHEN owner = 'a' THEN $1::uuid ELSE $2::uuid END,
			CASE WHEN owner = 'a' AND n % 3 = 0 THEN $4::uuid ELSE $3::uuid END,
			'sync', CASE WHEN n % 5 = 0 THEN 'failed' ELSE 'completed' END,
			at - interval '1 minute', at, 1, at
		FROM generate_series(0, 8 * 48 - 1) AS n,
			LATERAL (SELECT $5::timestamptz + n * interval '30 minutes' AS at) AS moment,
			unnest(ARRAY['a', 'b']) AS owner`,
		accountA, accountB, jobA, jobA2, first)
	require.NoError(t, err)
}

// A day is the same day in every read: the runs a period holds are the runs of its days, as the
// series of the days counts them. Clocks that go back at midnight make that midnight twice, and
// the hour between the two is of the day that follows, in the totals as in the series.
//
// The runs are one every half hour, so a day of 24 hours holds 48 of them, one of 25 hours 50
// and one of 23 hours 46: the numbers below are counted by hand from the hours of each change.
func Test_UsagePages_ADayIsTheSameDayInEveryRead(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	for _, c := range []struct {
		zone   string
		change CalendarDay
		// The runs of the two days before the change, of its day and of the two days after.
		runs [5]int64
	}{
		// Midnight twice: 01:00 goes back to 00:00.
		{"America/Havana", CalendarDay{Year: 2026, Month: time.November, Day: 1}, [5]int64{48, 48, 50, 48, 48}},
		{"Atlantic/Azores", CalendarDay{Year: 2026, Month: time.October, Day: 25}, [5]int64{48, 48, 50, 48, 48}},
		// No midnight: 00:00 goes forward to 01:00.
		{"America/Havana", CalendarDay{Year: 2026, Month: time.March, Day: 8}, [5]int64{48, 48, 46, 48, 48}},
		{"Atlantic/Azores", CalendarDay{Year: 2026, Month: time.March, Day: 29}, [5]int64{48, 48, 46, 48, 48}},
		// A change in the middle of the night.
		{"Europe/Paris", CalendarDay{Year: 2026, Month: time.October, Day: 25}, [5]int64{48, 48, 50, 48, 48}},
		{"Europe/Paris", CalendarDay{Year: 2026, Month: time.March, Day: 29}, [5]int64{48, 48, 46, 48, 48}},
		// No change: fourteen hours ahead of UTC, and UTC.
		{"Pacific/Kiritimati", CalendarDay{Year: 2026, Month: time.October, Day: 25}, [5]int64{48, 48, 48, 48, 48}},
		{"UTC", CalendarDay{Year: 2026, Month: time.October, Day: 25}, [5]int64{48, 48, 48, 48, 48}},
	} {
		zone, err := time.LoadLocation(c.zone)
		require.NoError(t, err)
		recordEveryHalfHour(ctx, t, container, c.change)
		days := [5]CalendarDay{}
		for i := range days {
			days[i] = calendarDayOf(CalendarDay{Year: c.change.Year, Month: c.change.Month, Day: c.change.Day + i - 2}.date())
		}

		// Every period of these five days: the ones that start on the day of the change, that end on
		// it, that end the day before, and each day alone.
		for first := range days {
			for last := first; last < len(days); last++ {
				period := Period{From: days[first], To: days[last], Zone: zone}
				name := fmt.Sprintf("%s, from %v to %v", c.zone, period.From, period.To)
				var want int64
				for _, runs := range c.runs[first : last+1] {
					want += runs
				}
				requireSameDays(ctx, t, store, period, c.runs[first:last+1], want, name)
			}
		}
	}
}

// requireSameDays reads a period every way the pages do, and requires each read to hold the runs
// counted by hand, day by day and in all.
func requireSameDays(ctx context.Context, t *testing.T, store *Store, period Period, ofDays []int64, want int64, name string) {
	t.Helper()
	account := Scope{AccountId: accountA}

	totals, err := store.UsageTotals(ctx, account, period)
	require.NoError(t, err, name)
	require.Equal(t, want, totals.Runs, "%s: the totals", name)
	require.Equal(t, want, totals.RowsRead, "%s: the rows of the totals", name)

	series, err := store.UsageDays(ctx, account, period)
	require.NoError(t, err, name)
	require.Len(t, series, len(ofDays), name)
	var inDays int64
	for i, day := range series {
		require.Equal(t, ofDays[i], day.Runs, "%s: the runs of %v", name, day.Day)
		inDays += day.Runs
	}
	require.Equal(t, want, inDays, "%s: the days add up to the totals", name)

	// The jobs of the table, and the one a table would leave out because it was deleted since.
	jobs, err := store.UsageJobs(ctx, accountA, period)
	require.NoError(t, err, name)
	require.Len(t, jobs, 2, name)
	var inJobs int64
	for _, job := range jobs {
		inJobs += job.Totals.Runs
	}
	require.Equal(t, want, inJobs, "%s: the jobs add up to the totals", name)

	var ofEachJob int64
	for _, jobId := range []string{jobA, jobA2} {
		job := Scope{AccountId: accountA, JobId: jobId}
		ofJob, err := store.UsageTotals(ctx, job, period)
		require.NoError(t, err, name)
		ofEachJob += ofJob.Runs

		jobSeries, err := store.UsageDays(ctx, job, period)
		require.NoError(t, err, name)
		var inJobDays int64
		for _, day := range jobSeries {
			inJobDays += day.Runs
		}
		require.Equal(t, ofJob.Runs, inJobDays, "%s: the days of the job %s add up to its totals", name, jobId)

		latest, err := store.LatestRuns(ctx, job, period, 1000)
		require.NoError(t, err, name)
		require.Len(t, latest, int(ofJob.Runs), "%s: the runs of the job %s are the ones its totals count", name, jobId)
	}
	require.Equal(t, want, ofEachJob, "%s: the job of the table and the deleted one add up to the totals", name)

	failures, err := store.UsageErrors(ctx, accountA, period)
	require.NoError(t, err, name)
	var failed int64
	for _, failure := range failures {
		failed += failure.Count
	}
	require.Equal(t, totals.Runs-totals.Completed, failed, "%s: the errors are the runs that did not complete", name)

	// The other account has a run at each of the same moments, and reads its own only.
	ofB, err := store.UsageTotals(ctx, Scope{AccountId: accountB}, period)
	require.NoError(t, err, name)
	require.Equal(t, want, ofB.Runs, "%s: the totals of the other account", name)
}
