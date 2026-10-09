package usagestore

import (
	"context"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

const jobA2 = "00000000-0000-0000-0000-0000000000a2"

var (
	oct5 = CalendarDay{Year: 2026, Month: time.October, Day: 5}
	oct6 = CalendarDay{Year: 2026, Month: time.October, Day: 6}
	oct7 = CalendarDay{Year: 2026, Month: time.October, Day: 7}
)

// utcDays is the period of the UTC days from the first given to the last, both included.
func utcDays(from, to CalendarDay) Period {
	return Period{From: from, To: to, Zone: time.UTC}
}

func seconds(n int64) *int64 { return &n }

// usageFixture makes the runs the pages are read from. Two accounts hold runs on the same day,
// and the second account holds a run under the job id of the first.
//
//	run          account / job        status                          lasts   rows read / set aside / tables uncounted   recorded
//	a1-ok        A / jobA             completed                       60 s    100 / 5 / 0                                Oct 6 09:00
//	a1-fail      A / jobA             failed, constraint, table sync  120 s   40 / 0 / 1                                 Oct 6 10:00
//	a1-settled   A / jobA             terminated, settled             no end  0                                          Oct 6 11:00
//	a1-canceled  A / jobA             canceled at the hooks           20 s    3 / 0 / 0                                  Oct 6 11:30
//	a2-ok        A / jobA2, generate  completed                       600 s   1000 / 0 / 0                               Oct 6 12:00
//	a1-old       A / jobA             completed                       30 s    7 / 0 / 0                                  Oct 5 23:59
//	a1-running   A / jobA             running since Oct 6 13:00
//	b-ok         B / jobA             completed                       10 s    9999 / 0 / 0                               Oct 6 09:00
func usageFixture(ctx context.Context, t *testing.T, container *tcpostgres.PostgresTestContainer, store *Store) {
	t.Helper()
	at := func(hour, minute int) time.Time {
		return october6.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	end := func(runId string, recorded time.Time, lasts time.Duration, mutate func(*RunEnd)) {
		t.Helper()
		endRun(t, container, store, runId, recorded.Add(-lasts), recorded, mutate)
	}

	end("a1-ok", at(9, 0), 60*time.Second, func(r *RunEnd) { r.RowsRead, r.RowsDiscarded = 100, 5 })
	end("a1-fail", at(10, 0), 120*time.Second, func(r *RunEnd) {
		r.Status, r.RowsRead, r.TablesUncounted = StatusFailed, 40, 1
		r.Error = RunError{Category: "constraint_violated", Step: "table_sync"}
	})
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "a1-settled", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: at(8, 0),
	}))
	require.NoError(t, store.Settle(ctx, "a1-settled", StatusTerminated, nil))
	recordOn(t, container, "a1-settled", at(11, 0))
	end("a1-canceled", at(11, 30), 20*time.Second, func(r *RunEnd) {
		r.Status, r.RowsRead, r.Error = StatusCanceled, 3, RunError{Step: "hooks"}
	})
	end("a2-ok", at(12, 0), 600*time.Second, func(r *RunEnd) { r.JobId, r.Kind, r.RowsRead = jobA2, JobKindGenerate, 1000 })
	end("a1-old", october6.Add(-time.Minute), 30*time.Second, func(r *RunEnd) { r.RowsRead = 7 })
	require.NoError(t, store.RunStarted(ctx, RunStart{
		RunId: "a1-running", AccountId: accountA, JobId: jobA, Kind: JobKindSync, StartedAt: at(13, 0),
	}))
	end("b-ok", at(9, 0), 10*time.Second, func(r *RunEnd) { r.AccountId, r.RowsRead = accountB, 9999 })
}

// The same job id read through two accounts gives the runs of each account, and a job the
// account has no run of gives nothing, with no error.
func Test_UsageTotals_CountOnlyTheAccountTheJobAndThePeriod(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)

	totals := func(scope Scope, period Period) *UsageTotals {
		t.Helper()
		got, err := store.UsageTotals(ctx, scope, period)
		require.NoError(t, err)
		return got
	}
	day := utcDays(oct6, oct6)

	// Four of the five runs have an end: 20, 60, 120 and 600 seconds. An account is told what
	// they add up to and no median, a job their median and no sum.
	require.Equal(t, &UsageTotals{
		Runs: 5, Completed: 2, Canceled: 1, RowsRead: 1143, RowsDiscarded: 5, WithUncountedRows: 1,
		DurationTotal: seconds(800),
	}, totals(Scope{AccountId: accountA}, day))
	require.Equal(t, &UsageTotals{
		Runs: 4, Completed: 1, Canceled: 1, RowsRead: 143, RowsDiscarded: 5, WithUncountedRows: 1,
		DurationMedian: seconds(60),
	}, totals(Scope{AccountId: accountA, JobId: jobA}, day))
	require.Equal(t, &UsageTotals{
		Runs: 1, Completed: 1, RowsRead: 1000, DurationMedian: seconds(600),
	}, totals(Scope{AccountId: accountA, JobId: jobA2}, day))

	require.Equal(t, &UsageTotals{
		Runs: 1, Completed: 1, RowsRead: 9999, DurationTotal: seconds(10),
	}, totals(Scope{AccountId: accountB}, day))
	require.Equal(t, &UsageTotals{
		Runs: 1, Completed: 1, RowsRead: 9999, DurationMedian: seconds(10),
	}, totals(Scope{AccountId: accountB, JobId: jobA}, day))
	// A job of another account: nothing, and no duration.
	require.Equal(t, &UsageTotals{}, totals(Scope{AccountId: accountB, JobId: jobA2}, day))

	// The day before holds one run, and both days hold six.
	require.Equal(t, &UsageTotals{
		Runs: 1, Completed: 1, RowsRead: 7, DurationTotal: seconds(30),
	}, totals(Scope{AccountId: accountA}, utcDays(oct5, oct5)))
	require.Equal(t, int64(6), totals(Scope{AccountId: accountA}, utcDays(oct5, oct6)).Runs)
	require.Equal(t, &UsageTotals{}, totals(Scope{AccountId: accountA}, utcDays(oct7, oct7)))
}

// A run settled without an end counts among the runs and in no duration; when no run of the
// period has an end there is no duration at all, not a duration of zero.
func Test_UsageTotals_ARunWithoutAnEndCountsInNoDuration(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	runOfStatus(ctx, t, store, "settled", StatusTimedOut, RunError{})
	recordAllOn(ctx, t, container, october6.Add(time.Hour))

	got, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, &UsageTotals{Runs: 1}, got)

	// An end told before its start lasts nothing.
	endRun(t, container, store, "backwards", october6.Add(3*time.Hour), october6.Add(2*time.Hour), nil)
	got, err = store.UsageTotals(ctx, Scope{AccountId: accountA}, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, &UsageTotals{Runs: 2, Completed: 1, DurationTotal: seconds(0)}, got)

	// The same for the median of a job.
	ofJob, err := store.UsageTotals(ctx, Scope{AccountId: accountA, JobId: jobA}, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, &UsageTotals{Runs: 2, Completed: 1, DurationMedian: seconds(0)}, ofJob)
}

// The success rate is completed / (runs - canceled): the store gives the three terms, and a
// run of every status that ends lands in the right one.
func Test_UsageTotals_GiveTheTermsOfTheSuccessRate(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	for _, status := range Statuses() {
		runOfStatus(ctx, t, store, "run-"+string(status), status, RunError{})
	}
	runOfStatus(ctx, t, store, "run-completed-2", StatusCompleted, RunError{})
	runOfStatus(ctx, t, store, "run-canceled-2", StatusCanceled, RunError{})
	recordAllOn(ctx, t, container, october6.Add(time.Hour))

	got, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, utcDays(oct6, oct6))
	require.NoError(t, err)
	// Seven runs ended: the running one is not among them. Two completed of the five that
	// were not canceled.
	require.Equal(t, int64(7), got.Runs)
	require.Equal(t, int64(2), got.Completed)
	require.Equal(t, int64(2), got.Canceled)
}

func Test_UsageDays_GiveEveryDayOfThePeriod(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)

	days, err := store.UsageDays(ctx, Scope{AccountId: accountA}, utcDays(oct5, oct7))
	require.NoError(t, err)
	require.Equal(t, []UsageDay{
		{Day: oct5, RowsRead: 7, Runs: 1},
		{Day: oct6, RowsRead: 1143, Runs: 5},
		{Day: oct7},
	}, days)

	ofJob, err := store.UsageDays(ctx, Scope{AccountId: accountA, JobId: jobA2}, utcDays(oct5, oct7))
	require.NoError(t, err)
	require.Equal(t, []UsageDay{{Day: oct5}, {Day: oct6, RowsRead: 1000, Runs: 1}, {Day: oct7}}, ofJob)

	ofB, err := store.UsageDays(ctx, Scope{AccountId: accountB}, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, []UsageDay{{Day: oct6, RowsRead: 9999, Runs: 1}}, ofB)

	// A job of another account: its days, all at zero.
	foreign, err := store.UsageDays(ctx, Scope{AccountId: accountB, JobId: jobA2}, utcDays(oct5, oct6))
	require.NoError(t, err)
	require.Equal(t, []UsageDay{{Day: oct5}, {Day: oct6}}, foreign)
}

func Test_UsageJobs_PutTheJobWithMostRowsFirst(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)

	jobs, err := store.UsageJobs(ctx, accountA, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, []JobUsage{
		{JobId: jobA2, Kind: JobKindGenerate, Totals: UsageTotals{
			Runs: 1, Completed: 1, RowsRead: 1000, DurationMedian: seconds(600),
		}},
		{JobId: jobA, Kind: JobKindSync, Totals: UsageTotals{
			Runs: 4, Completed: 1, Canceled: 1, RowsRead: 143, RowsDiscarded: 5, WithUncountedRows: 1,
			DurationMedian: seconds(60),
		}},
	}, jobs)

	ofB, err := store.UsageJobs(ctx, accountB, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, []JobUsage{{JobId: jobA, Kind: JobKindSync, Totals: UsageTotals{
		Runs: 1, Completed: 1, RowsRead: 9999, DurationMedian: seconds(10),
	}}}, ofB)

	none, err := store.UsageJobs(ctx, accountA, utcDays(oct7, oct7))
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// The jobs of an account add up to its totals for everything that adds up.
func Test_UsageJobs_AddUpToTheTotalsOfTheAccount(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)

	period := utcDays(oct5, oct6)
	totals, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, period)
	require.NoError(t, err)
	jobs, err := store.UsageJobs(ctx, accountA, period)
	require.NoError(t, err)

	var sum UsageTotals
	for _, job := range jobs {
		sum.Runs += job.Totals.Runs
		sum.Completed += job.Totals.Completed
		sum.Canceled += job.Totals.Canceled
		sum.RowsRead += job.Totals.RowsRead
		sum.RowsDiscarded += job.Totals.RowsDiscarded
		sum.WithUncountedRows += job.Totals.WithUncountedRows

		// Each job reads the same through the scope of the job.
		ofJob, err := store.UsageTotals(ctx, Scope{AccountId: accountA, JobId: job.JobId}, period)
		require.NoError(t, err)
		require.Equal(t, &job.Totals, ofJob, job.JobId)
	}
	// The time the runs lasted is told of the account alone: the jobs tell a median, which does
	// not add up.
	sum.DurationTotal = totals.DurationTotal
	require.Equal(t, totals, &sum)
}

// The kind told of a job is the one of its run recorded last in the period.
func Test_UsageJobs_TellTheKindOfTheLastRun(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)

	endRun(t, container, store, "first", october6, october6.Add(time.Hour), func(r *RunEnd) { r.Kind = JobKindGenerate })
	endRun(t, container, store, "last", october6, october6.Add(3*time.Hour), func(r *RunEnd) { r.Kind = JobKindPiiDetect })
	endRun(t, container, store, "between", october6, october6.Add(2*time.Hour), func(r *RunEnd) { r.Kind = JobKindSync })

	jobs, err := store.UsageJobs(ctx, accountA, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, JobKindPiiDetect, jobs[0].Kind)
	require.Equal(t, int64(3), jobs[0].Totals.Runs)
}

func Test_UsageErrors_CountByCategory(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)
	// A second constraint, of the other job of the account, and at another step.
	endRun(t, container, store, "a2-fail", october6.Add(13*time.Hour), october6.Add(14*time.Hour), func(r *RunEnd) {
		r.JobId, r.Status, r.Error = jobA2, StatusFailed, RunError{Category: "constraint_violated", Step: "hooks"}
	})
	// An error of the other account, on the same day.
	endRun(t, container, store, "b-fail", october6.Add(13*time.Hour), october6.Add(14*time.Hour), func(r *RunEnd) {
		r.AccountId, r.Status, r.Error = accountB, StatusFailed, RunError{Category: "license", Step: "preflight"}
	})

	errors, err := store.UsageErrors(ctx, accountA, utcDays(oct6, oct6))
	require.NoError(t, err)
	// Most runs first, then by category.
	require.Equal(t, []CategoryCount{
		{Category: "constraint_violated", Count: 2},
		{Category: "canceled", Count: 1},
		{Category: "other", Count: 1},
	}, errors)

	ofB, err := store.UsageErrors(ctx, accountB, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, []CategoryCount{{Category: "license", Count: 1}}, ofB)

	// A day whose runs all completed.
	none, err := store.UsageErrors(ctx, accountA, utcDays(oct5, oct5))
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}

// A row that holds a status and no pair is what an API of the version before writes while both
// versions serve: the page reads it with the rule the store writes with when nothing is told,
// for every status, and the errors still add up to the runs that did not complete.
func Test_UsageErrors_ARowWithoutItsPairReadsAsTheStoreWouldHaveWrittenIt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	period := utcDays(oct6, oct6)

	for _, status := range Statuses() {
		t.Run(string(status), func(t *testing.T) {
			_, err := container.DB.Exec(ctx, `DELETE FROM husonym_api.run_usage`)
			require.NoError(t, err)
			runOfStatus(ctx, t, store, "run", status, RunError{})
			recordAllOn(ctx, t, container, october6.Add(12*time.Hour))

			written, err := store.UsageErrors(ctx, accountA, period)
			require.NoError(t, err)
			want := []CategoryCount{}
			if pair := errorOf(status, RunError{}); pair != (RunError{}) {
				want = append(want, CategoryCount{Category: pair.Category, Count: 1})
			}
			require.Equal(t, want, written)
			runsWritten, err := store.LatestRuns(ctx, Scope{AccountId: accountA, JobId: jobA}, period, 10)
			require.NoError(t, err)

			forgetPairs(ctx, t, container)
			require.Empty(t, readRun(ctx, t, container, "run").pairOf())
			read, err := store.UsageErrors(ctx, accountA, period)
			require.NoError(t, err)
			require.Equal(t, written, read)
			runsRead, err := store.LatestRuns(ctx, Scope{AccountId: accountA, JobId: jobA}, period, 10)
			require.NoError(t, err)
			require.Equal(t, runsWritten, runsRead)
		})
	}

	t.Run("the fixture", func(t *testing.T) {
		_, err := container.DB.Exec(ctx, `DELETE FROM husonym_api.run_usage`)
		require.NoError(t, err)
		usageFixture(ctx, t, container, store)
		forgetPairs(ctx, t, container)

		errors, err := store.UsageErrors(ctx, accountA, period)
		require.NoError(t, err)
		require.Equal(t, []CategoryCount{{Category: "other", Count: 2}, {Category: "canceled", Count: 1}}, errors)

		totals, err := store.UsageTotals(ctx, Scope{AccountId: accountA}, period)
		require.NoError(t, err)
		var counted int64
		for _, row := range errors {
			counted += row.Count
		}
		require.Equal(t, totals.Runs-totals.Completed, counted)
	})
}

func Test_LatestRuns_AreTheRunsOfTheJobOfTheAccountInThePeriod(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, store := migratedDatabase(ctx, t)
	usageFixture(ctx, t, container, store)

	latest := func(scope Scope, period Period, limit int32) []RunRow {
		t.Helper()
		runs, err := store.LatestRuns(ctx, scope, period, limit)
		require.NoError(t, err)
		return runs
	}
	ids := func(runs []RunRow) []string {
		got := make([]string, 0, len(runs))
		for i := range runs {
			got = append(got, runs[i].RunId)
		}
		return got
	}
	ofJobA := Scope{AccountId: accountA, JobId: jobA}

	// The most recently recorded first, and no more than asked.
	require.Equal(t, []string{"a1-canceled", "a1-settled"}, ids(latest(ofJobA, utcDays(oct6, oct6), 2)))

	// The runs of the day: not the one of the day before, not the one still running, which is of
	// no day, and none of another job or of another account.
	day := latest(ofJobA, utcDays(oct6, oct6), 10)
	require.Equal(t, []string{"a1-canceled", "a1-settled", "a1-fail", "a1-ok"}, ids(day))

	canceled, settled, failed, ok := day[0], day[1], day[2], day[3]
	require.Equal(t, StatusCanceled, canceled.Status)
	require.Equal(t, RunError{Category: "canceled", Step: "hooks"}, canceled.Error)

	require.Equal(t, StatusTerminated, settled.Status)
	require.Nil(t, settled.EndedAt)
	require.Equal(t, RunError{Category: "other", Step: "other"}, settled.Error)

	require.Equal(t, StatusFailed, failed.Status)
	require.Equal(t, int64(40), failed.RowsRead)
	require.Equal(t, int64(1), failed.TablesUncounted)
	require.Equal(t, RunError{Category: "constraint_violated", Step: "table_sync"}, failed.Error)
	require.True(t, october6.Add(10*time.Hour-120*time.Second).Equal(failed.StartedAt))
	require.NotNil(t, failed.EndedAt)
	require.True(t, october6.Add(10*time.Hour).Equal(*failed.EndedAt))

	require.Equal(t, StatusCompleted, ok.Status)
	require.Equal(t, int64(100), ok.RowsRead)
	require.Equal(t, RunError{}, ok.Error)

	// A longer period reaches the run of the day before, the oldest last.
	require.Equal(t,
		[]string{"a1-canceled", "a1-settled", "a1-fail", "a1-ok", "a1-old"},
		ids(latest(ofJobA, utcDays(oct5, oct6), 10)))
	require.Empty(t, latest(ofJobA, utcDays(oct7, oct7), 10))

	other := latest(Scope{AccountId: accountA, JobId: jobA2}, utcDays(oct6, oct6), 10)
	require.Equal(t, []string{"a2-ok"}, ids(other))

	// The same job id through the other account gives the run of that account only.
	require.Equal(t, []string{"b-ok"}, ids(latest(Scope{AccountId: accountB, JobId: jobA}, utcDays(oct5, oct6), 10)))
	foreign := latest(Scope{AccountId: accountB, JobId: jobA2}, utcDays(oct5, oct6), 10)
	require.NotNil(t, foreign)
	require.Empty(t, foreign)

	_, err := store.LatestRuns(ctx, Scope{AccountId: accountA}, utcDays(oct6, oct6), 10)
	require.Error(t, err, "the latest runs are the ones of a job")
}

func Test_AccountRefusals_SumTheGatesOfTheAccount(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	refuse := func(account string, at time.Time, gate license.Gate) {
		t.Helper()
		require.NoError(t, store.CountRefusal(ctx, account, []license.Gate{gate}, at))
	}
	// The first and the last second of the day, the last of the day before, the first of the next.
	refuse(accountA, october6, license.GateSourceCap)
	refuse(accountA, october6.Add(24*time.Hour-time.Second), license.GateSourceCap)
	refuse(accountA, october6.Add(-time.Second), license.GateJobCap)
	refuse(accountA, october6.Add(24*time.Hour), license.GateConnectionCap)
	refuse(accountB, october6.Add(time.Hour), license.GateSourceCap)
	refuse(accountB, october6.Add(time.Hour), license.GateNotInForce)

	day, err := store.AccountRefusals(ctx, accountA, utcDays(oct6, oct6))
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateSourceCap, Count: 2}}, day)

	both, err := store.AccountRefusals(ctx, accountA, utcDays(oct5, oct6))
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateJobCap, Count: 1}, {Gate: license.GateSourceCap, Count: 2}}, both)

	ofB, err := store.AccountRefusals(ctx, accountB, utcDays(oct5, oct7))
	require.NoError(t, err)
	require.Equal(t, []GateCount{{Gate: license.GateNotInForce, Count: 1}, {Gate: license.GateSourceCap, Count: 1}}, ofB)

	none, err := store.AccountRefusals(ctx, accountA, utcDays(
		CalendarDay{Year: 2026, Month: time.November, Day: 1}, CalendarDay{Year: 2026, Month: time.November, Day: 30}))
	require.NoError(t, err)
	require.NotNil(t, none)
	require.Empty(t, none)
}
