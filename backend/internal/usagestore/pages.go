package usagestore

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// MaxPeriodDays is the longest period the pages read: the days of a leap year. The runs are
// never purged, so a period has to end somewhere for a page to stay a page.
const MaxPeriodDays = 366

// ErrPeriod is the refusal of a period that cannot be read: a day that does not exist, a last
// day before the first, or more than MaxPeriodDays days. Nothing is read of such a period.
var ErrPeriod = errors.New("the period cannot be read")

// CalendarDay is a day of a calendar: a date, with no time and no zone of its own.
type CalendarDay struct {
	Year  int
	Month time.Month
	Day   int
}

// Period is the days the pages read, from the first to the last, both included, as the zone
// counts them: a run is of the date the clocks of the zone showed, and a day lasts 23 or 25 hours
// when they change. Every read places a run in its day by that date alone, so that a period
// holds exactly the runs of its days. Zone is a location loaded by its name, and only its name
// is used; nil reads as UTC. The database places the runs in the days, with its own knowledge of
// the zones, and nothing here computes a moment in the zone: a zone the database does not know
// fails the read, and ReadInZone then reads the same days as UTC days.
//
// A run is of the day on which the API recorded its end, as in the daily report, which counts
// in UTC days: the totals of a day in another zone are not the ones of that report. A run still
// running is of no day. The license refusals are counted by UTC day when they happen: for them,
// and for them alone, the days of the period are UTC days whatever the zone.
type Period struct {
	From, To CalendarDay
	Zone     *time.Location
}

// Scope is whose runs are read: the ones of an account, and of one of its jobs when JobId is
// not empty. A job that is not of the account has no run in it.
type Scope struct {
	AccountId, JobId string
}

// UsageTotals is what the runs counted in a period add up to. The success rate of the period is
// Completed over Runs less Canceled: a canceled run neither succeeded nor failed.
type UsageTotals struct {
	// Runs is every run whose end was recorded in the period, whatever its status.
	Runs, Completed, Canceled int64
	RowsRead, RowsDiscarded   int64
	// WithUncountedRows is how many runs had a table that reported no row count.
	WithUncountedRows int64
	// Durations are in seconds, from the start of a run to its end, and never negative: the median
	// the daily report gives, and the sum. A run without an end counts in neither, and both are
	// nil when no run of the period has an end.
	DurationMedian, DurationTotal *int64
}

// UsageDay is the runs counted on a day of the period.
type UsageDay struct {
	Day            CalendarDay
	RowsRead, Runs int64
}

// JobUsage is what the runs of a job add up to. Kind is the kind of its run recorded last in the
// period: it is the kind the table of jobs tells, so that nothing of a job but its name is read
// to show it. The store holds no name and does not know whether the job still exists.
type JobUsage struct {
	JobId  string
	Kind   JobKind
	Totals UsageTotals
}

// CategoryCount is how many runs did not complete for a category of error.
type CategoryCount struct {
	Category ErrorCategory
	Count    int64
}

// RunRow is a run as its row tells it. EndedAt is nil for a run settled without a known end.
type RunRow struct {
	RunId     string
	Status    Status
	StartedAt time.Time
	EndedAt   *time.Time

	RowsRead, TablesUncounted int64

	// Error is empty for a run that completed.
	Error RunError
}

// UsageTotals adds up the runs of the scope counted in the period.
func (s *Store) UsageTotals(ctx context.Context, scope Scope, period Period) (*UsageTotals, error) {
	read, err := readOf(scope, period)
	if err != nil {
		return nil, err
	}
	var sums db_queries.SumAccountRunUsageBetweenRow
	if scope.JobId == "" {
		sums, err = s.db.Q.SumAccountRunUsageBetween(ctx, s.db.Db, db_queries.SumAccountRunUsageBetweenParams{
			AccountID: read.account, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
		})
	} else {
		var ofJob db_queries.SumJobRunUsageBetweenRow
		ofJob, err = s.db.Q.SumJobRunUsageBetween(ctx, s.db.Db, db_queries.SumJobRunUsageBetweenParams{
			AccountID: read.account, JobID: read.job, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
		})
		sums = db_queries.SumAccountRunUsageBetweenRow(ofJob)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to add up the runs of the period: %w", err)
	}
	totals := totalsOf(sums)
	return &totals, nil
}

// UsageDays gives every day of the period, the oldest first, with the runs of the scope counted
// on it: a day without a run is there, at zero.
func (s *Store) UsageDays(ctx context.Context, scope Scope, period Period) ([]UsageDay, error) {
	read, err := readOf(scope, period)
	if err != nil {
		return nil, err
	}
	var rows []db_queries.SumAccountRunUsageByDayBetweenRow
	if scope.JobId == "" {
		rows, err = s.db.Q.SumAccountRunUsageByDayBetween(ctx, s.db.Db, db_queries.SumAccountRunUsageByDayBetweenParams{
			AccountID: read.account, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
		})
	} else {
		var ofJob []db_queries.SumJobRunUsageByDayBetweenRow
		ofJob, err = s.db.Q.SumJobRunUsageByDayBetween(ctx, s.db.Db, db_queries.SumJobRunUsageByDayBetweenParams{
			AccountID: read.account, JobID: read.job, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
		})
		rows = make([]db_queries.SumAccountRunUsageByDayBetweenRow, 0, len(ofJob))
		for _, row := range ofJob {
			rows = append(rows, db_queries.SumAccountRunUsageByDayBetweenRow(row))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("unable to add up the runs of each day: %w", err)
	}

	counted := make(map[CalendarDay]UsageDay, len(rows))
	for _, row := range rows {
		day := calendarDayOf(row.Day)
		counted[day] = UsageDay{Day: day, RowsRead: row.RowsRead, Runs: row.Runs}
	}
	days := make([]UsageDay, 0, len(read.days))
	for _, day := range read.days {
		ofDay := counted[day]
		ofDay.Day = day
		days = append(days, ofDay)
	}
	return days, nil
}

// UsageJobs gives the jobs of the account that have a run counted in the period, with what
// their runs add up to: the job that read the most rows first, then the one with the most runs.
func (s *Store) UsageJobs(ctx context.Context, accountId string, period Period) ([]JobUsage, error) {
	read, err := readOf(Scope{AccountId: accountId}, period)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Q.SumAccountRunUsageByJobBetween(ctx, s.db.Db, db_queries.SumAccountRunUsageByJobBetweenParams{
		AccountID: read.account, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to add up the runs of each job: %w", err)
	}
	jobs := make([]JobUsage, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		jobs = append(jobs, JobUsage{
			JobId: husonymdb.UUIDString(row.JobID),
			Kind:  JobKind(row.JobKind),
			Totals: totalsOf(db_queries.SumAccountRunUsageBetweenRow{
				Runs: row.Runs, RunsCompleted: row.RunsCompleted, RunsCanceled: row.RunsCanceled,
				RowsRead: row.RowsRead, RowsDiscarded: row.RowsDiscarded, WithUncountedRows: row.WithUncountedRows,
				RunsWithEnd: row.RunsWithEnd, DurationMedian: row.DurationMedian, DurationTotal: row.DurationTotal,
			}),
		})
	}
	return jobs, nil
}

// UsageErrors gives, per category, the runs of the account counted in the period that did not
// complete: the category with the most runs first. They are the runs UsageTotals counts beyond
// the completed ones, each once.
func (s *Store) UsageErrors(ctx context.Context, accountId string, period Period) ([]CategoryCount, error) {
	read, err := readOf(Scope{AccountId: accountId}, period)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Q.CountAccountRunUsageErrorsBetween(ctx, s.db.Db, db_queries.CountAccountRunUsageErrorsBetweenParams{
		AccountID: read.account, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to count the errors of the period: %w", err)
	}
	counted := make(map[ErrorCategory]int64, len(rows))
	for _, row := range rows {
		// Rows of several statuses and steps, and rows that hold no pair, meet under one category.
		if pair := errorRead(Status(row.Status), row.ErrorCategory, row.ErrorStep); pair != (RunError{}) {
			counted[pair.Category] += row.Runs
		}
	}
	counts := make([]CategoryCount, 0, len(counted))
	for category, count := range counted {
		counts = append(counts, CategoryCount{Category: category, Count: count})
	}
	slices.SortFunc(counts, func(a, b CategoryCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Category, b.Category))
	})
	return counts, nil
}

// AccountRefusals adds up the refusals of each gate for the account, on the UTC days of the
// period: a refusal keeps the UTC day it happened on and no time, so the zone does not move it.
func (s *Store) AccountRefusals(ctx context.Context, accountId string, period Period) ([]GateCount, error) {
	read, err := readOf(Scope{AccountId: accountId}, period)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Q.SumAccountGateRefusalsBetween(ctx, s.db.Db, db_queries.SumAccountGateRefusalsBetweenParams{
		AccountID: read.account, FromDay: read.fromDay, BeforeDay: read.beforeDay,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to add up the refusals of the period: %w", err)
	}
	counts := make([]GateCount, 0, len(rows))
	for _, row := range rows {
		counts = append(counts, GateCount{Gate: license.Gate(row.Gate), Count: row.Refusals})
	}
	return counts, nil
}

// LatestRuns gives the runs of a job of the account counted in the period, the most recently
// recorded first, and no more than limit. They are the runs UsageTotals counts for the same
// scope: a run still running, which is of no day, is not among them. The scope names a job.
func (s *Store) LatestRuns(ctx context.Context, scope Scope, period Period, limit int32) ([]RunRow, error) {
	if scope.JobId == "" {
		return nil, errors.New("the latest runs are the ones of a job: the scope names none")
	}
	read, err := readOf(scope, period)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Q.ListJobRunUsageBetween(ctx, s.db.Db, db_queries.ListJobRunUsageBetweenParams{
		AccountID: read.account, JobID: read.job, Zone: read.zone, FromDay: read.fromDay, BeforeDay: read.beforeDay,
		RunLimit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to read the latest runs of the job: %w", err)
	}
	runs := make([]RunRow, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		run := RunRow{
			RunId:           row.RunID,
			Status:          Status(row.Status),
			StartedAt:       row.StartedAt.Time,
			RowsRead:        row.RowsRead,
			TablesUncounted: row.TablesUncounted,
			Error:           errorRead(Status(row.Status), row.ErrorCategory, row.ErrorStep),
		}
		if row.EndedAt.Valid {
			run.EndedAt = &row.EndedAt.Time
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// pageRead is a scope and a period as the queries take them.
type pageRead struct {
	account, job pgtype.UUID
	zone         string
	// The days run from the first to the day before the second, as in every read of the store.
	fromDay, beforeDay pgtype.Date
	days               []CalendarDay
}

// readOf refuses a period that cannot be read and an id that is not one, before anything is asked
// of the database.
func readOf(scope Scope, period Period) (*pageRead, error) {
	days, err := period.days()
	if err != nil {
		return nil, err
	}
	read := &pageRead{
		zone:      period.ZoneName(),
		fromDay:   period.From.date(),
		beforeDay: period.To.next().date(),
		days:      days,
	}
	if read.account, err = husonymdb.ToUuid(scope.AccountId); err != nil {
		return nil, fmt.Errorf("account id: %w", err)
	}
	if scope.JobId != "" {
		if read.job, err = husonymdb.ToUuid(scope.JobId); err != nil {
			return nil, fmt.Errorf("job id: %w", err)
		}
	}
	return read, nil
}

// totalsOf gives no duration when no run has an end, where the sums hold a zero.
func totalsOf(sums db_queries.SumAccountRunUsageBetweenRow) UsageTotals {
	totals := UsageTotals{
		Runs:              sums.Runs,
		Completed:         sums.RunsCompleted,
		Canceled:          sums.RunsCanceled,
		RowsRead:          sums.RowsRead,
		RowsDiscarded:     sums.RowsDiscarded,
		WithUncountedRows: sums.WithUncountedRows,
	}
	if sums.RunsWithEnd > 0 {
		totals.DurationMedian, totals.DurationTotal = &sums.DurationMedian, &sums.DurationTotal
	}
	return totals
}

// days are the days of the period, the first first, or ErrPeriod when the period is not one.
func (p Period) days() ([]CalendarDay, error) {
	if !p.From.exists() || !p.To.exists() {
		return nil, fmt.Errorf("%w: one of its days does not exist", ErrPeriod)
	}
	from, to := p.From.date().Time, p.To.date().Time
	if to.Before(from) {
		return nil, fmt.Errorf("%w: its last day is before its first", ErrPeriod)
	}
	// Both are midnights of UTC, which has no change of clocks: whole days apart.
	count := int(to.Sub(from)/(24*time.Hour)) + 1
	if count > MaxPeriodDays {
		return nil, fmt.Errorf("%w: it has more than %d days", ErrPeriod, MaxPeriodDays)
	}
	days := make([]CalendarDay, 0, count)
	for day := p.From; len(days) < count; day = day.next() {
		days = append(days, day)
	}
	return days, nil
}

// ReadInZone reads a period, and reads it once more as UTC days when the database does not know
// its zone: the zones a browser names and the ones the database holds come from two sets of data,
// and a page is not refused for a zone. read is every read of one answer, so that all of it is of
// the same days; it is given the period to read, and starts over when it is called again. The
// period returned is the one that was read: its zone is the one to tell with what was read.
//
// The refusal is told by the code of the database's error, never by its words. A read in UTC is
// not read again, whatever it fails on.
func ReadInZone(period Period, read func(Period) error) (Period, error) {
	err := read(period)
	var refusal *pgconn.PgError
	if err == nil || period.ZoneName() == time.UTC.String() ||
		!errors.As(err, &refusal) || refusal.Code != pgUnknownZoneCode {
		return period, err
	}
	period.Zone = time.UTC
	return period, read(period)
}

// pgUnknownZoneCode is invalid_parameter_value: what AT TIME ZONE answers for a name that is no
// zone of the database.
const pgUnknownZoneCode = "22023"

// ZoneName is the name the database knows the zone by.
func (p Period) ZoneName() string {
	if p.Zone == nil {
		return time.UTC.String()
	}
	return p.Zone.String()
}

// exists tells a day of the calendar from numbers that make none, like February 30.
func (d CalendarDay) exists() bool {
	return calendarDayOf(d.date()) == d
}

// next is the day after: calendar days have no hour, and no change of clocks.
func (d CalendarDay) next() CalendarDay {
	return calendarDayOf(pgtype.Date{Time: time.Date(d.Year, d.Month, d.Day+1, 0, 0, 0, 0, time.UTC)})
}

// date is the day as the database takes it. Numbers that make no day give the day they overflow
// to: exists tells them apart.
func (d CalendarDay) date() pgtype.Date {
	return pgtype.Date{Time: time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC), Valid: true}
}

func calendarDayOf(date pgtype.Date) CalendarDay {
	year, month, day := date.Time.Date()
	return CalendarDay{Year: year, Month: month, Day: day}
}
