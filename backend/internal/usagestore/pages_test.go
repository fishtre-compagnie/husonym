package usagestore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func day(year int, month time.Month, dayOfMonth int) CalendarDay {
	return CalendarDay{Year: year, Month: month, Day: dayOfMonth}
}

// pageReads are the six reads of the pages, each on a store that has no database: a read that
// refuses its period or its scope never reaches it.
func pageReads(store *Store, scope Scope, period Period) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"UsageTotals": func(ctx context.Context) error { _, err := store.UsageTotals(ctx, scope, period); return err },
		"UsageDays":   func(ctx context.Context) error { _, err := store.UsageDays(ctx, scope, period); return err },
		"UsageJobs":   func(ctx context.Context) error { _, err := store.UsageJobs(ctx, scope.AccountId, period); return err },
		"UsageErrors": func(ctx context.Context) error { _, err := store.UsageErrors(ctx, scope.AccountId, period); return err },
		"AccountRefusals": func(ctx context.Context) error {
			_, err := store.AccountRefusals(ctx, scope.AccountId, period)
			return err
		},
		"LatestRuns": func(ctx context.Context) error { _, err := store.LatestRuns(ctx, scope, period, 20); return err },
	}
}

func Test_UsagePages_RefuseAPeriodThatIsNotOne(t *testing.T) {
	for name, period := range map[string]Period{
		"that ends before it starts":   {From: day(2026, 10, 7), To: day(2026, 10, 6)},
		"of a day that does not exist": {From: day(2026, 2, 30), To: day(2026, 3, 2)},
		"to a day that does not exist": {From: day(2026, 4, 1), To: day(2026, 4, 31)},
		"of a month that is not one":   {From: day(2026, 13, 1), To: day(2027, 1, 2)},
		"of no day at all":             {},
		"one day too long":             {From: day(2026, 1, 1), To: day(2027, 1, 2)},
		"one day too long, leap year":  {From: day(2028, 1, 1), To: day(2029, 1, 1)},
		"of years":                     {From: day(2020, 1, 1), To: day(2026, 1, 1), Zone: time.UTC},
	} {
		for read, call := range pageReads(New(nil), Scope{AccountId: accountA, JobId: jobA}, period) {
			require.ErrorIs(t, call(t.Context()), ErrPeriod, "%s of a period %s", read, name)
		}
	}
}

func Test_Period_TakesUpToTheLongestYear(t *testing.T) {
	for name, c := range map[string]struct {
		period Period
		days   int
	}{
		"one day":           {Period{From: day(2026, 10, 6), To: day(2026, 10, 6)}, 1},
		"a leap day":        {Period{From: day(2028, 2, 29), To: day(2028, 2, 29)}, 1},
		"a year":            {Period{From: day(2026, 1, 1), To: day(2026, 12, 31)}, 365},
		"a year and a day":  {Period{From: day(2026, 1, 1), To: day(2027, 1, 1)}, 366},
		"a leap year":       {Period{From: day(2028, 1, 1), To: day(2028, 12, 31)}, 366},
		"across two years":  {Period{From: day(2026, 12, 20), To: day(2027, 1, 5)}, 17},
		"long after today":  {Period{From: day(2099, 1, 1), To: day(2099, 3, 31)}, 90},
		"a month in a zone": {Period{From: day(2026, 10, 1), To: day(2026, 10, 31), Zone: time.FixedZone("Europe/Paris", 3600)}, 31},
	} {
		days, err := c.period.days()
		require.NoError(t, err, name)
		require.Len(t, days, c.days, name)
		require.Equal(t, c.period.From, days[0], name)
		require.Equal(t, c.period.To, days[len(days)-1], name)
		for i := 1; i < len(days); i++ {
			require.Equal(t, days[i-1].next(), days[i], "%s: the days follow one another", name)
		}
	}
	require.Equal(t, 366, MaxPeriodDays)
}

func Test_CalendarDay_TheNextDayCrossesMonthsAndYears(t *testing.T) {
	require.Equal(t, day(2026, 11, 1), day(2026, 10, 31).next())
	require.Equal(t, day(2027, 1, 1), day(2026, 12, 31).next())
	require.Equal(t, day(2028, 2, 29), day(2028, 2, 28).next())
	require.Equal(t, day(2026, 3, 1), day(2026, 2, 28).next())
	// The day clocks change is a day like another: a calendar day has no hour.
	require.Equal(t, day(2026, 10, 26), day(2026, 10, 25).next())
	require.Equal(t, day(2026, 3, 30), day(2026, 3, 29).next())
}

func Test_Period_NamesItsZoneForTheDatabase(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	require.NoError(t, err)
	require.Equal(t, "Europe/Paris", Period{Zone: paris}.zoneName())
	require.Equal(t, "UTC", Period{Zone: time.UTC}.zoneName())
	require.Equal(t, "UTC", Period{}.zoneName())
}

func Test_UsagePages_RefuseAnIdThatIsNotAUuid(t *testing.T) {
	period := Period{From: day(2026, 10, 6), To: day(2026, 10, 6)}
	for read, call := range pageReads(New(nil), Scope{AccountId: "nope", JobId: jobA}, period) {
		require.ErrorContains(t, call(t.Context()), "account id", read)
	}
	store := New(nil)
	for read, call := range map[string]func() error{
		"UsageTotals": func() error {
			_, err := store.UsageTotals(t.Context(), Scope{AccountId: accountA, JobId: "nope"}, period)
			return err
		},
		"UsageDays": func() error {
			_, err := store.UsageDays(t.Context(), Scope{AccountId: accountA, JobId: "nope"}, period)
			return err
		},
		"LatestRuns": func() error {
			_, err := store.LatestRuns(t.Context(), Scope{AccountId: accountA, JobId: "nope"}, period, 20)
			return err
		},
	} {
		require.ErrorContains(t, call(), "job id", read)
	}
}

func Test_LatestRuns_AreTheOnesOfAJob(t *testing.T) {
	_, err := New(nil).LatestRuns(t.Context(), Scope{AccountId: accountA}, Period{From: day(2026, 10, 6), To: day(2026, 10, 6)}, 20)
	require.ErrorContains(t, err, "job")
}
