package usagereport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// ErrPeriod says that the months asked are not a period a report is made for.
var ErrPeriod = errors.New("the period is not one a usage report is made for")

// BuildPeriod returns the sealed usage report of the instance for the months from the one of
// from to the one of to, both included and both taken in UTC, or ErrPeriod, or
// ErrNoLicenseInForce. now is the moment the report is made: the month under way may be asked
// and is told as far as it went, and what the report says of the license is read at that moment.
//
// The counters of a month are added up from the runs and the refusals the instance recorded
// during it. Its state is not read from the instance, which only knows the present one: it is
// the state the last report of the day kept for that month tells. A kept report that can no
// longer be read is left out and logged by its day.
//
// A document its schema refuses is an error: nothing is sealed.
func (b *Builder) BuildPeriod(ctx context.Context, from, to, now time.Time) (*Sealed, error) {
	first, last, err := periodOf(from, to, now)
	if err != nil {
		return nil, err
	}
	// Asked of what the process holds, as for the report of a day: without a license nothing is
	// read at all.
	if !b.license.IsValid() {
		return nil, ErrNoLicenseInForce
	}
	keyValue, identification, err := b.identify(ctx, now)
	if err != nil {
		return nil, err
	}
	identification.InstanceID, err = b.counters.InstanceId(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read the id of the instance: %w", err)
	}
	stored, err := b.counters.ReportsBetween(ctx, first, last.AddDate(0, 1, 0))
	if err != nil {
		return nil, fmt.Errorf("unable to read the usage reports of the period: %w", err)
	}
	kept := reportsByMonth(ctx, stored)

	report := &telemetry.PeriodReport{
		SchemaVersion:  telemetry.SchemaVersion,
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		From:           first.Format(telemetry.MonthLayout),
		To:             last.Format(telemetry.MonthLayout),
		Identification: *identification,
	}
	unknownGate := false
	for month := first; !month.After(last); month = month.AddDate(0, 1, 0) {
		told := monthOf(month, kept[month.Format(telemetry.MonthLayout)], b.facts.Diagnostics)
		if b.facts.Diagnostics {
			unknown, err := b.countMonth(ctx, month, &told)
			if err != nil {
				return nil, err
			}
			unknownGate = unknownGate || unknown
		}
		report.Months = append(report.Months, told)
	}
	if unknownGate {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
			ctx, "refusals of a gate the usage report does not know are left out of it",
		)
	}

	document, err := report.Marshal()
	if err != nil {
		return nil, fmt.Errorf("unable to write the usage report of the period: %w", err)
	}
	if err := telemetry.ValidatePeriod(document); err != nil {
		return nil, err
	}
	seal, err := telemetry.Seal(keyValue, document)
	if err != nil {
		return nil, err
	}
	return &Sealed{Document: document, Seal: seal, KeyFingerprint: identification.KeyFingerprint}, nil
}

// periodOf gives the first day, in UTC, of the first and of the last month of a period, or
// ErrPeriod: the first month is not after the last, the last is not after the month of now, and
// they are telemetry.MaxPeriodMonths months at most.
func periodOf(from, to, now time.Time) (first, last time.Time, err error) {
	first, last = firstOfMonth(from), firstOfMonth(to)
	months := (last.Year()-first.Year())*12 + int(last.Month()-first.Month()) + 1
	switch {
	case first.After(last):
		return first, last, fmt.Errorf("%w: its first month is after its last", ErrPeriod)
	case last.After(firstOfMonth(now)):
		return first, last, fmt.Errorf("%w: its last month has not begun", ErrPeriod)
	case months > telemetry.MaxPeriodMonths:
		return first, last, fmt.Errorf("%w: it holds more than %d months", ErrPeriod, telemetry.MaxPeriodMonths)
	}
	return first, last, nil
}

// firstOfMonth is the first day of the UTC month of a moment.
func firstOfMonth(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// reportsByMonth reads the reports that are kept, the oldest first, and gives them by their
// month, in the same order. One that is no longer JSON, or that the schema refuses, is left out
// and logged by its day and nothing else: why it is refused may quote what it holds.
func reportsByMonth(ctx context.Context, stored []usagestore.StoredReport) map[string][]*telemetry.Report {
	kept := make(map[string][]*telemetry.Report)
	for i := range stored {
		day := stored[i].Day.UTC()
		report, ok := readReport(stored[i].Document)
		if !ok {
			logger_interceptor.GetLoggerFromContextOrDefault(ctx).WarnContext(
				ctx, "a usage report that is kept can no longer be read: the report of the period is made without it",
				"day", day.Format(time.DateOnly),
			)
			continue
		}
		month := day.Format(telemetry.MonthLayout)
		kept[month] = append(kept[month], report)
	}
	return kept
}

// readReport reads the report of a day back from its document, when it still is one.
func readReport(document []byte) (*telemetry.Report, bool) {
	if err := telemetry.Validate(document); err != nil {
		return nil, false
	}
	var report telemetry.Report
	if err := json.Unmarshal(document, &report); err != nil {
		return nil, false
	}
	return &report, true
}

// monthOf says what the reports kept for a month, the oldest first, tell of it: how many days
// they are, the most sources one of them counts, and the version and the state of the last one.
// A last report made with the diagnostic switched off tells no state, and none is told when the
// diagnostic is switched off now.
func monthOf(month time.Time, reports []*telemetry.Report, diagnostics bool) telemetry.MonthReport {
	told := telemetry.MonthReport{Month: month.Format(telemetry.MonthLayout), DaysReported: len(reports)}
	if len(reports) == 0 {
		return told
	}
	for _, report := range reports {
		told.Sources.Count = max(told.Sources.Count, report.Sources.Count)
	}
	last := reports[len(reports)-1]
	told.Version = &last.Version
	if diagnostics && last.Diagnostics != nil {
		told.State = telemetry.StateOf(last.Diagnostics)
	}
	return told
}

// countMonth adds to a month the runs and the refusals the instance recorded from its first day
// to the first of the next month. unknown tells there were refusals of a gate the report does
// not know, which are left out.
func (b *Builder) countMonth(ctx context.Context, month time.Time, told *telemetry.MonthReport) (unknown bool, err error) {
	next := month.AddDate(0, 1, 0)
	counted, err := b.counters.RunsBetween(ctx, month, next)
	if err != nil {
		return false, fmt.Errorf("unable to read the runs of a month: %w", err)
	}
	refusals, err := b.counters.RefusalsBetween(ctx, month, next)
	if err != nil {
		return false, fmt.Errorf("unable to read the refusals of a month: %w", err)
	}
	runs := runsOf(counted)
	told.Runs = &runs
	told.Refusals, unknown = refusalCounts(refusals)
	return unknown, nil
}
