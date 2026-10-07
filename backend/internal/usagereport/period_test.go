package usagereport

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/stretchr/testify/require"
)

const fullPeriodPath = "testdata/period-full.json"

// span is the days a reading was asked for: from the first to the day before the second.
type span struct{ from, before time.Time }

// fakeMonths is what the usage store holds over several days: the runs and the refusals by the
// month they are asked from, and the reports it keeps.
type fakeMonths struct {
	monthRuns     map[string]*usagestore.DayRuns
	monthRefusals map[string][]usagestore.GateCount
	reports       []usagestore.StoredReport

	runSpans, refusalSpans, reportSpans []span

	monthRunsErr, monthRefusalsErr, reportsErr error
}

func (f *fakeMonths) RunsBetween(_ context.Context, from, before time.Time) (*usagestore.DayRuns, error) {
	f.runSpans = append(f.runSpans, span{from, before})
	if runs, ok := f.monthRuns[from.Format(telemetry.MonthLayout)]; ok {
		return runs, f.monthRunsErr
	}
	return &usagestore.DayRuns{ByStatus: []usagestore.RunCount{}}, f.monthRunsErr
}

func (f *fakeMonths) RefusalsBetween(_ context.Context, from, before time.Time) ([]usagestore.GateCount, error) {
	f.refusalSpans = append(f.refusalSpans, span{from, before})
	return f.monthRefusals[from.Format(telemetry.MonthLayout)], f.monthRefusalsErr
}

// ReportsBetween gives the reports of the days asked, the oldest first, as the store does.
func (f *fakeMonths) ReportsBetween(_ context.Context, from, before time.Time) ([]usagestore.StoredReport, error) {
	f.reportSpans = append(f.reportSpans, span{from, before})
	var kept []usagestore.StoredReport
	for _, report := range f.reports {
		if !report.Day.Before(from) && report.Day.Before(before) {
			kept = append(kept, report)
		}
	}
	slices.SortFunc(kept, func(a, b usagestore.StoredReport) int { return a.Day.Compare(b.Day) })
	return kept, f.reportsErr
}

func month(year int, m time.Month) time.Time { return time.Date(year, m, 1, 0, 0, 0, 0, time.UTC) }

func monthSpan(year int, m time.Month) span {
	return span{month(year, m), month(year, m).AddDate(0, 1, 0)}
}

// keep makes the report of a day the way the instance does, after the fixture was altered, and
// keeps it.
func (f *fixture) keep(t testing.TB, day time.Time, alter func(f *fixture)) {
	t.Helper()
	made := newFixture(t)
	made.keys, made.ring = &fakeKeys{value: f.keys.value}, f.ring
	if alter != nil {
		alter(made)
	}
	sealed, err := made.builder().Build(t.Context(), day, day.Add(24*time.Hour+5*time.Minute))
	require.NoError(t, err)
	f.counters.reports = append(f.counters.reports, usagestore.StoredReport{
		Day: day, Document: sealed.Document, Seal: sealed.Seal, KeyFingerprint: sealed.KeyFingerprint,
	})
}

// newPeriodFixture is an instance that kept two reports in August and one in October, ran in
// August and in October, and did nothing in September.
func newPeriodFixture(t testing.TB) *fixture {
	t.Helper()
	f := newFixture(t)
	// A report made early in August, on an earlier version, with more sources than later.
	f.keep(t, time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC), func(early *fixture) {
		early.facts.Version = "v0.2.0"
		early.instance.sources = 5
		early.instance.workers = 1
		early.inventory.inventory.Jobs.Tables = 80
	})
	f.keep(t, time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), nil)
	f.keep(t, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), func(late *fixture) {
		late.facts.Version = "v0.3.1"
		late.instance.sources = 4
		late.inventory.inventory.Jobs.Tables = 150
	})
	f.counters.monthRuns = map[string]*usagestore.DayRuns{
		"2026-08": f.counters.runs,
		"2026-10": {
			ByStatus:       []usagestore.RunCount{{Kind: usagestore.JobKindSync, Status: usagestore.StatusCompleted, Count: 2}},
			DurationMedian: int64p(90), DurationP95: int64p(95), RowsRead: 4_200,
		},
	}
	f.counters.monthRefusals = map[string][]usagestore.GateCount{"2026-08": f.counters.refusals}
	return f
}

var (
	periodFrom = month(2026, time.August)
	periodTo   = month(2026, time.October)
)

func months(t testing.TB, document []byte) []map[string]any {
	t.Helper()
	var read []map[string]any
	for _, m := range tree(t, document)["months"].([]any) {
		read = append(read, m.(map[string]any))
	}
	return read
}

// The whole report of a period, byte for byte: the reference file is the document itself.
func Test_BuildPeriod_TheFullReportIsTheReferenceFile(t *testing.T) {
	f := newPeriodFixture(t)
	ctx, output := logged(t)

	sealed, err := f.builder().BuildPeriod(ctx, periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(fullPeriodPath, sealed.Document, 0o600))
	}
	want, err := os.ReadFile(fullPeriodPath)
	require.NoError(t, err)
	require.Equal(t, string(want), string(sealed.Document),
		"the report differs from its reference: when that is meant, run go test ./backend/internal/usagereport -update")

	// It passes its schema, and whoever holds the key checks its seal.
	require.NoError(t, telemetry.ValidatePeriod(sealed.Document))
	require.NoError(t, telemetry.Verify(f.keys.value, sealed.Document, sealed.Seal))
	require.Error(t, telemetry.Verify(f.keys.value, append(slices.Clone(sealed.Document), ' '), sealed.Seal))
	require.Equal(t, telemetry.KeyFingerprint(f.keys.value), sealed.KeyFingerprint)

	read := tree(t, sealed.Document)
	require.Equal(t, "2026-08", read["from"])
	require.Equal(t, "2026-10", read["to"])
	require.Equal(t, "2026-10-07T00:05:12Z", read["generated_at"])
	require.Equal(t, map[string]any{
		"key_fingerprint": sealed.KeyFingerprint, "license_id": testLicense, "instance_id": testInstance,
		"license_state": "valid", "days_to_expiry": float64(212),
	}, read["identification"])

	// Each month is read from its first day to the first of the next, and the reports in one go.
	wantSpans := []span{monthSpan(2026, time.August), monthSpan(2026, time.September), monthSpan(2026, time.October)}
	require.Equal(t, wantSpans, f.counters.runSpans)
	require.Equal(t, wantSpans, f.counters.refusalSpans)
	require.Equal(t, []span{{periodFrom, month(2026, time.November)}}, f.counters.reportSpans)
	// Nothing of the present state of the instance is read: a month tells what its reports kept.
	require.Zero(t, f.inventory.calls)
	require.Zero(t, f.instance.calls)

	requireNoLeak(t, string(sealed.Document))
	require.NotContains(t, string(sealed.Document), f.keys.value)
	require.Empty(t, output.String(), "a report that reads everything logs nothing")
}

// The state and the version of a month are the ones of its last report; its sources are the
// highest count its reports hold.
func Test_BuildPeriod_AMonthTellsItsLastReportAndItsMostSources(t *testing.T) {
	f := newPeriodFixture(t)

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	august := months(t, sealed.Document)[0]
	require.Equal(t, "2026-08", august["month"])
	require.Equal(t, float64(2), august["days_reported"])
	require.Equal(t, map[string]any{"husonym": "v0.3.0"}, august["version"])
	require.Equal(t, map[string]any{"count": float64(5)}, august["sources"])
	require.Equal(t, float64(120), block(t, august, "state", "jobs")["tables"])
	require.Equal(t, float64(2), block(t, august, "state", "installation")["workers"])
	// The state is the nine blocks of the diagnostic of that report, and nothing counted over a day.
	last := block(t, tree(t, f.counters.reports[1].Document), "diagnostics")
	for _, counted := range []string{"runs", "refusals", "source_engines", "errors"} {
		delete(last, counted)
	}
	require.Len(t, last, 9)
	require.Equal(t, last, august["state"])

	october := months(t, sealed.Document)[2]
	require.Equal(t, float64(1), october["days_reported"])
	require.Equal(t, map[string]any{"husonym": "v0.3.1"}, october["version"])
	require.Equal(t, map[string]any{"count": float64(4)}, october["sources"])
	require.Equal(t, float64(150), block(t, october, "state", "jobs")["tables"])
}

// The counters of a month are the ones the store adds up over it: rows leave as the band of
// their sum, and the durations are the ones of the month.
func Test_BuildPeriod_AMonthTellsItsRunsAndItsRefusals(t *testing.T) {
	f := newPeriodFixture(t)

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	read := months(t, sealed.Document)
	require.Equal(t, map[string]any{
		"by_status": []any{
			map[string]any{"kind": "generate", "status": "completed", "count": float64(1)},
			map[string]any{"kind": "sync", "status": "completed", "count": float64(12)},
			map[string]any{"kind": "sync", "status": "failed", "count": float64(2)},
		},
		"duration_seconds": map[string]any{"median": float64(180), "p95": float64(640)},
		"rows_read":        "lt_10m", "rows_discarded": "lt_1k", "retries": float64(3), "with_uncounted_rows": float64(1),
	}, read[0]["runs"])
	require.Equal(t, []any{
		map[string]any{"gate": "source_cap", "count": float64(1)},
		map[string]any{"gate": "subsetting", "count": float64(2)},
	}, read[0]["refusals"])
	require.Equal(t, "lt_10k", block(t, read[2], "runs")["rows_read"])
	require.Equal(t, []any{}, read[2]["refusals"])
}

// A month without a run nor a report: its counters are there and empty, it reports no day, has
// no version and no state, and counts no source.
func Test_BuildPeriod_AMonthWithoutRunNorReport(t *testing.T) {
	f := newPeriodFixture(t)

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	require.Equal(t, map[string]any{
		"month": "2026-09", "days_reported": float64(0), "sources": map[string]any{"count": float64(0)},
		"runs": map[string]any{
			"by_status": []any{}, "rows_read": "lt_1k", "rows_discarded": "lt_1k",
			"retries": float64(0), "with_uncounted_rows": float64(0),
		},
		"refusals": []any{},
	}, months(t, sealed.Document)[1])
}

// The diagnostic switched off: the month, its days, its version and its sources, and neither the
// runs nor the refusals are even read.
func Test_BuildPeriod_WithoutDiagnostics(t *testing.T) {
	f := newPeriodFixture(t)
	f.facts.Diagnostics = false

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.ValidatePeriod(sealed.Document))
	require.NoError(t, telemetry.Verify(f.keys.value, sealed.Document, sealed.Seal))

	require.Equal(t, []map[string]any{
		{
			"month": "2026-08", "days_reported": float64(2), "version": map[string]any{"husonym": "v0.3.0"},
			"sources": map[string]any{"count": float64(5)},
		},
		{"month": "2026-09", "days_reported": float64(0), "sources": map[string]any{"count": float64(0)}},
		{
			"month": "2026-10", "days_reported": float64(1), "version": map[string]any{"husonym": "v0.3.1"},
			"sources": map[string]any{"count": float64(4)},
		},
	}, months(t, sealed.Document))
	require.Empty(t, f.counters.runSpans)
	require.Empty(t, f.counters.refusalSpans)
}

// A month whose last report was made with the diagnostic switched off has no state to tell: the
// one of an earlier report is not the state at the end of the month.
func Test_BuildPeriod_ALastReportWithoutDiagnosticsLeavesNoState(t *testing.T) {
	f := newPeriodFixture(t)
	f.keep(t, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC), func(off *fixture) {
		off.facts.Diagnostics = false
		off.instance.sources = 1
	})

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	august := months(t, sealed.Document)[0]
	require.Equal(t, float64(3), august["days_reported"])
	require.NotContains(t, august, "state")
	require.Equal(t, map[string]any{"count": float64(5)}, august["sources"])
	require.Contains(t, august, "runs")
	require.Contains(t, august, "refusals")
}

// A period is whole months, in UTC: any moment of a month names it.
func Test_BuildPeriod_TakesAnyMomentOfAMonth(t *testing.T) {
	f := newPeriodFixture(t)
	want, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	// The last second of August in UTC is already September further east, and the first of
	// October still September further west.
	east := time.FixedZone("east", 5*3600)
	west := time.FixedZone("west", -8*3600)
	got, err := f.builder().BuildPeriod(t.Context(),
		month(2026, time.September).Add(-time.Second).In(east), periodTo.In(west), reportNow)
	require.NoError(t, err)
	require.Equal(t, string(want.Document), string(got.Document))
}

func Test_BuildPeriod_RefusesAPeriodThatIsNotOne(t *testing.T) {
	for name, tc := range map[string]struct {
		from, to time.Time
		refused  bool
	}{
		"one month":                    {periodTo, periodTo, false},
		"the month under way":          {periodFrom, month(2026, time.October), false},
		"thirty-six months":            {month(2023, time.November), month(2026, time.October), false},
		"thirty-seven months":          {month(2023, time.October), month(2026, time.October), true},
		"the first month after":        {month(2026, time.September), month(2026, time.August), true},
		"next month":                   {periodFrom, month(2026, time.November), true},
		"a month of next year":         {periodFrom, month(2027, time.January), true},
		"only months to come":          {month(2026, time.November), month(2026, time.December), true},
		"a far past the calendar has":  {month(1, time.January), month(2, time.January), false},
		"more months than a period is": {month(1, time.January), month(2026, time.October), true},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPeriodFixture(t)

			sealed, err := f.builder().BuildPeriod(t.Context(), tc.from, tc.to, reportNow)
			if !tc.refused {
				require.NoError(t, err)
				require.NoError(t, telemetry.ValidatePeriod(sealed.Document))
				return
			}
			require.ErrorIs(t, err, ErrPeriod)
			require.Nil(t, sealed)
			require.Zero(t, f.keys.calls)
			require.Zero(t, f.counters.calls)
			require.Empty(t, f.counters.reportSpans)
		})
	}
}

// The month under way is the one of now in UTC, wherever the clock of the caller stands.
func Test_BuildPeriod_TheMonthUnderWayIsTheOneOfNowInUtc(t *testing.T) {
	f := newPeriodFixture(t)
	// The last hour of October in UTC, read on a clock already in November.
	lastHour := month(2026, time.November).Add(-time.Hour).In(time.FixedZone("east", 5*3600))

	_, err := f.builder().BuildPeriod(t.Context(), periodFrom, month(2026, time.November), lastHour)
	require.ErrorIs(t, err, ErrPeriod)

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, month(2026, time.October), lastHour)
	require.NoError(t, err)
	require.Equal(t, "2026-10-31T23:00:00Z", tree(t, sealed.Document)["generated_at"])
}

// Without a license in force there is no report, and nothing of the instance is read.
func Test_BuildPeriod_NoLicenseInForce(t *testing.T) {
	for name, without := range map[string]func(f *fixture){
		"the process holds no license in force": func(f *fixture) { f.license.inForce = false },
		"the instance holds no key":             func(f *fixture) { f.keys.value = "" },
		"the key of the instance is past its grace period": func(f *fixture) {
			f.keys.value, f.ring = mintKey(t, keyExpiring(reportNow.AddDate(0, 0, -license.DefaultGraceDays-1)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newPeriodFixture(t)
			without(f)

			sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
			require.ErrorIs(t, err, ErrNoLicenseInForce)
			require.Nil(t, sealed)
			require.Zero(t, f.counters.calls)
			require.Empty(t, f.counters.reportSpans)
			require.Empty(t, f.counters.runSpans)
		})
	}
}

// The identification is the one of the key in force when the file is made, whichever key sealed
// the reports of the months.
func Test_BuildPeriod_IdentifiesTheKeyInForceNow(t *testing.T) {
	f := newPeriodFixture(t)
	earlier := f.keys.value
	renewal := keyExpiring(reportNow.Add(36 * time.Hour))
	renewal.Id = "0011223344556677"
	f.keys.value, f.ring = mintKey(t, renewal)

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.NoError(t, err)

	require.NoError(t, telemetry.Verify(f.keys.value, sealed.Document, sealed.Seal))
	require.Error(t, telemetry.Verify(earlier, sealed.Document, sealed.Seal))
	require.Equal(t, map[string]any{
		"key_fingerprint": telemetry.KeyFingerprint(f.keys.value), "license_id": "0011223344556677",
		"instance_id": testInstance, "license_state": "expiring", "days_to_expiry": float64(1),
	}, tree(t, sealed.Document)["identification"])
	require.NotContains(t, string(sealed.Document), telemetry.KeyFingerprint(earlier))
}

// A report that is kept and can no longer be read is left out and said by its day alone: nothing
// of what it holds, nor of why it is refused, reaches the document or the log.
func Test_BuildPeriod_AStoredReportThatCanNoLongerBeRead(t *testing.T) {
	f := newPeriodFixture(t)
	valid := string(f.counters.reports[1].Document)
	f.counters.reports = append(f.counters.reports,
		usagestore.StoredReport{Day: time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC), Document: []byte(`{"job": "` + leak + `-nightly"`)},
		usagestore.StoredReport{
			Day:      time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
			Document: []byte(strings.Replace(valid, `"sources":{"count":3}`, `"sources":{"count":9,"host":"`+leak+`.example.com"}`, 1)),
		},
		usagestore.StoredReport{
			Day:      time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC),
			Document: []byte(strings.Replace(valid, `"kind":"helm"`, `"kind":"`+leak+`-cluster"`, 1)),
		},
	)
	for _, report := range f.counters.reports[3:] {
		require.Contains(t, string(report.Document), leak)
	}
	ctx, output := logged(t)

	sealed, err := f.builder().BuildPeriod(ctx, periodFrom, periodTo, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.ValidatePeriod(sealed.Document))

	// August tells the two reports that read, and its state is the one of the last of them.
	august := months(t, sealed.Document)[0]
	require.Equal(t, float64(2), august["days_reported"])
	require.Equal(t, map[string]any{"count": float64(5)}, august["sources"])
	require.Equal(t, "helm", block(t, august, "state", "installation")["kind"])

	require.Equal(t, 3, strings.Count(output.String(), `"level":"WARN"`))
	for _, day := range []string{"2026-08-25", "2026-08-28", "2026-08-30"} {
		require.Contains(t, output.String(), `"day":"`+day+`"`)
	}
	requireNoLeak(t, string(sealed.Document))
	requireNoLeak(t, output.String())
	require.NotContains(t, output.String(), "schema", "why a report is refused is not said: it may quote it")
}

// Whatever is not of a closed list becomes other, or is left out where the list has no other.
func Test_BuildPeriod_ValuesOutsideTheirLists(t *testing.T) {
	f := newPeriodFixture(t)
	f.counters.monthRuns["2026-09"] = &usagestore.DayRuns{ByStatus: []usagestore.RunCount{
		{Kind: usagestore.JobKind(leak + "-kind"), Status: usagestore.Status(leak + "-status"), Count: 2},
		{Kind: usagestore.JobKind(leak + "-another-kind"), Status: usagestore.Status(leak + "-another"), Count: 3},
	}}
	for _, m := range []string{"2026-09", "2026-10"} {
		f.counters.monthRefusals[m] = []usagestore.GateCount{
			{Gate: license.Gate("subsetting"), Count: 2},
			{Gate: license.Gate(leak + "-gate"), Count: 7},
		}
	}
	ctx, output := logged(t)

	sealed, err := f.builder().BuildPeriod(ctx, periodFrom, periodTo, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.ValidatePeriod(sealed.Document))

	september := months(t, sealed.Document)[1]
	require.Equal(t,
		[]any{map[string]any{"kind": "other", "status": "other", "count": float64(5)}},
		block(t, september, "runs")["by_status"])
	require.Equal(t, []any{map[string]any{"gate": "subsetting", "count": float64(2)}}, september["refusals"])
	// What is dropped is said once for the period, and nothing of it is quoted.
	require.Equal(t, 1, strings.Count(output.String(), "refusals of a gate the usage report does not know are left out of it"))
	require.Equal(t, 1, strings.Count(output.String(), `"level":"WARN"`))
	requireNoLeak(t, string(sealed.Document))
	requireNoLeak(t, output.String())
}

// A reading the report cannot do without fails it: nothing is sealed.
func Test_BuildPeriod_AReadingThatFails(t *testing.T) {
	failure := errors.New("unavailable")
	for name, fail := range map[string]func(*fixture){
		"the id of the instance":    func(f *fixture) { f.counters.instanceErr = failure },
		"the reports that are kept": func(f *fixture) { f.counters.reportsErr = failure },
		"the runs of a month":       func(f *fixture) { f.counters.monthRunsErr = failure },
		"the refusals of a month":   func(f *fixture) { f.counters.monthRefusalsErr = failure },
		"the key":                   func(f *fixture) { f.keys.err = failure },
	} {
		t.Run(name, func(t *testing.T) {
			f := newPeriodFixture(t)
			fail(f)

			sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
			require.ErrorIs(t, err, failure)
			require.NotErrorIs(t, err, ErrNoLicenseInForce)
			require.NotErrorIs(t, err, ErrPeriod)
			require.Nil(t, sealed)
		})
	}
}

// A document the schema refuses is an error: it is never sealed.
func Test_BuildPeriod_ADocumentTheSchemaRefuses(t *testing.T) {
	f := newPeriodFixture(t)
	f.counters.instanceId = "not-an-instance-id"

	sealed, err := f.builder().BuildPeriod(t.Context(), periodFrom, periodTo, reportNow)
	require.ErrorContains(t, err, "does not match its schema")
	require.Nil(t, sealed)
}
