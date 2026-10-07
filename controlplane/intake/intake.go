// Package intake receives the usage reports of the instances: it checks one, then stores it,
// or keeps it pending when no issued license has its fingerprint yet.
package intake

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

const (
	// PendingCapPerFingerprint is how many reports are kept pending under one fingerprint.
	PendingCapPerFingerprint = 100
	// PendingCapTotal is how many reports are kept pending in all.
	PendingCapTotal = 10000
	// PendingKept is how long a report is kept pending.
	PendingKept = 45 * 24 * time.Hour
	// InstanceCapPerLicense is on how many instances a license may be seen at a time: the ones
	// whose last report is of one of the last MaxDaysBehind days.
	InstanceCapPerLicense = 50
	// MaxDaysAhead is how many days after the UTC day of its reception the day of a report may
	// be: one, for the clock of an instance that runs ahead.
	MaxDaysAhead = 1
	// MaxDaysBehind is how many days before the UTC day of its reception the day of a report
	// may be.
	MaxDaysBehind = 60
)

// Outcome is what became of a report that was received.
type Outcome int

const (
	// Stored: the report was checked against its license and stored.
	Stored Outcome = iota
	// Pending: no issued license has the fingerprint yet; the report is kept as received.
	Pending
	// Repeat: the same document was already stored for that license, instance and day.
	Repeat
	// Conflict: another document was already stored for that license, instance and day. It
	// stays, and the conflict is counted.
	Conflict
	// Refused: the caller got something wrong. The seal or the fingerprint is malformed, the
	// document is outside the schema, its day is too far from the day it is received, the seal
	// does not match, the fingerprint given aside is not the one the document tells, or the
	// license id the document states is not the one of the license it is sealed under.
	Refused
	// Full: a cap on the pending reports is reached; the caller may try again later.
	Full
	// TooManyInstances: the report is sound, but its license was seen on as many instances as
	// it may within the last MaxDaysBehind days, and this instance is not one of them.
	TooManyInstances
)

// hexShape is the one spelling of a seal and of a fingerprint: 32 bytes in lowercase hex.
var hexShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// HexShaped says whether value is spelled as a seal and a fingerprint are.
func HexShaped(value string) bool {
	return hexShape.MatchString(value)
}

// Intake receives usage reports.
type Intake struct {
	store *cpstore.Store
	now   func() time.Time
}

// New returns an Intake that stores in store and reads the time from now.
func New(store *cpstore.Store, now func() time.Time) *Intake {
	return &Intake{store: store, now: now}
}

// Receive checks a report and stores it, or keeps it pending when its license is not known.
// What the caller got wrong is told by Refused or TooManyInstances, never by an error: a non-nil
// error is a failure of ours, and the outcome that comes with it is Full, as the caller may try
// again in both cases.
func (i *Intake) Receive(ctx context.Context, document []byte, seal, fingerprint string) (Outcome, error) {
	if !HexShaped(seal) || !HexShaped(fingerprint) {
		return Refused, nil
	}
	receivedAt := i.now()
	told, ok := read(document, receivedAt)
	if !ok || told.fingerprint != fingerprint {
		return Refused, nil
	}

	issued, err := i.store.LicenseByFingerprint(ctx, fingerprint)
	if errors.Is(err, cpstore.ErrNoLicense) {
		return i.keepPending(ctx, told, document, seal, receivedAt)
	}
	if err != nil {
		return Full, err
	}

	if !sealed(issued, document, seal) {
		if err := i.store.CountSealRejection(ctx, issued.Id, receivedAt); err != nil {
			return Full, err
		}
		return Refused, nil
	}
	if !told.says(issued) {
		return Refused, nil
	}
	outcome, err := i.store.StoreReport(ctx, told.report(issued, document, seal, receivedAt), instanceCap(receivedAt))
	if err != nil {
		return Full, err
	}
	switch outcome {
	case cpstore.ReportRepeat:
		return Repeat, nil
	case cpstore.ReportConflict:
		return Conflict, nil
	case cpstore.ReportTooManyInstances:
		return TooManyInstances, nil
	default:
		return Stored, nil
	}
}

// keepPending keeps a report whose license is not known. Should the license be recorded at
// this very moment, the report is still pending afterwards: PromotePending takes it from there.
func (i *Intake) keepPending(
	ctx context.Context, told *reading, document []byte, seal string, receivedAt time.Time,
) (Outcome, error) {
	kept, err := i.store.KeepPending(ctx, &cpstore.PendingReport{
		KeyFingerprint: told.fingerprint,
		InstanceID:     told.instanceID,
		Day:            told.day,
		Document:       document,
		Seal:           seal,
		ReceivedAt:     receivedAt,
	}, cpstore.PendingCaps{PerFingerprint: PendingCapPerFingerprint, Total: PendingCapTotal})
	if err != nil {
		return Full, err
	}
	if !kept {
		return Full, nil
	}
	return Pending, nil
}

// PromotePending goes through the reports pending under a fingerprint an issued license now
// has. Each one is checked on its own as a report received is, then stored or discarded, and
// removed from pending, one report per transaction: a report discarded does not hold the others
// back. Several may be pending for one instance and day, under different seals: the one the
// instance sealed is stored, the others are discarded. A report discarded for its seal is
// counted under the license, on the day of the promotion. A report that states another license
// than the one it is sealed under, or that the license has no room for, is discarded as well.
//
// stored counts the reports that verify and that the license has room for, including one that
// turns out to be a repeat of, or in conflict with, a report already stored. A report stored
// keeps the moment it was received. Nothing is done for a fingerprint no license has.
func (i *Intake) PromotePending(ctx context.Context, fingerprint string) (stored, discarded int, err error) {
	issued, err := i.store.LicenseByFingerprint(ctx, fingerprint)
	if errors.Is(err, cpstore.ErrNoLicense) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	pending, err := i.store.PendingReports(ctx, fingerprint)
	if err != nil {
		return 0, 0, err
	}
	for n := range pending {
		kept, err := i.promote(ctx, issued, &pending[n])
		if err != nil {
			return stored, discarded, err
		}
		if kept {
			stored++
		} else {
			discarded++
		}
	}
	return stored, discarded, nil
}

// promote stores one pending report of a license, or discards it; kept tells which.
func (i *Intake) promote(
	ctx context.Context, issued *cpstore.License, pending *cpstore.PendingReport,
) (kept bool, err error) {
	// The day is judged from the moment the report was received, as Receive judged it.
	told, ok := read(pending.Document, pending.ReceivedAt)
	if !ok || told.fingerprint != pending.KeyFingerprint {
		// Receive lets no such report in; one that is there all the same is of no use.
		return false, i.store.DiscardPendingReport(ctx, pending)
	}
	if !sealed(issued, pending.Document, pending.Seal) {
		return false, i.store.DiscardPendingReportForItsSeal(ctx, pending, issued.Id, i.now())
	}
	if !told.says(issued) {
		return false, i.store.DiscardPendingReport(ctx, pending)
	}
	outcome, err := i.store.PromotePendingReport(ctx, pending,
		told.report(issued, pending.Document, pending.Seal, pending.ReceivedAt), instanceCap(i.now()))
	if err != nil {
		return false, err
	}
	return outcome != cpstore.ReportTooManyInstances, nil
}

// instanceCap is the cap on the instances of a license at the moment at: the instances that
// count are those whose last report is of a day a report received at that moment may be of.
func instanceCap(at time.Time) cpstore.InstanceCap {
	return cpstore.InstanceCap{Max: InstanceCapPerLicense, SeenSince: earliestDay(at)}
}

// sealed says whether seal is the seal of document under the license. A key the seal cannot be
// computed from says no as well: the store only holds keys that were verified.
func sealed(issued *cpstore.License, document []byte, seal string) bool {
	return telemetry.Verify(issued.Encoded, document, seal) == nil
}

// reading is what the intake reads in a document.
type reading struct {
	day         time.Time
	fingerprint string
	// licenseID is the id of its license as the document states it.
	licenseID  string
	instanceID string
	version    string
	// installKind is empty when the document carries no diagnostics.
	installKind string
}

// read checks a document received at receivedAt against the schema of the report and reads
// what the intake needs in it; ok is false for a document that is not a report, or whose day is
// too far from the day it was received.
func read(document []byte, receivedAt time.Time) (told *reading, ok bool) {
	// The schema is checked on the decoded document, which takes bytes that are not UTF-8;
	// the database would not.
	if !utf8.Valid(document) || telemetry.Validate(document) != nil {
		return nil, false
	}
	var report struct {
		Day            string `json:"day"`
		Identification struct {
			KeyFingerprint string `json:"key_fingerprint"`
			LicenseID      string `json:"license_id"`
			InstanceID     string `json:"instance_id"`
		} `json:"identification"`
		Version struct {
			Husonym string `json:"husonym"`
		} `json:"version"`
		Diagnostics *struct {
			Installation struct {
				Kind string `json:"kind"`
			} `json:"installation"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(document, &report); err != nil {
		return nil, false
	}
	// The schema checks the shape of the day, not that it is a date.
	day, err := time.Parse(time.DateOnly, report.Day)
	if err != nil || !withinBounds(day, receivedAt) {
		return nil, false
	}
	told = &reading{
		day:         day,
		fingerprint: report.Identification.KeyFingerprint,
		licenseID:   report.Identification.LicenseID,
		instanceID:  report.Identification.InstanceID,
		version:     report.Version.Husonym,
	}
	if report.Diagnostics != nil {
		told.installKind = report.Diagnostics.Installation.Kind
	}
	return told, true
}

// withinBounds says whether a report received at receivedAt may be of that day. A day far ahead
// would stay the last day of its instance for ever, and no later report would tell its state;
// a day far back is of no use, as an instance stops trying long before.
func withinBounds(day, receivedAt time.Time) bool {
	return !day.After(utcDay(receivedAt).AddDate(0, 0, MaxDaysAhead)) && !day.Before(earliestDay(receivedAt))
}

// earliestDay is the earliest day a report received at receivedAt may be of.
func earliestDay(receivedAt time.Time) time.Time {
	return utcDay(receivedAt).AddDate(0, 0, -MaxDaysBehind)
}

// utcDay is the UTC day of at, at midnight.
func utcDay(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// says tells whether the document states the license it is sealed under: the id of that
// license, as the product writes it.
func (r *reading) says(issued *cpstore.License) bool {
	return r.licenseID == telemetry.LicenseId(issued.Id)
}

// report is the report to store for a document that was read and whose seal was verified.
func (r *reading) report(issued *cpstore.License, document []byte, seal string, receivedAt time.Time) *cpstore.Report {
	return &cpstore.Report{
		InstanceID:     r.instanceID,
		Day:            r.day,
		LicenseID:      issued.Id,
		Document:       document,
		Seal:           seal,
		HusonymVersion: r.version,
		InstallKind:    r.installKind,
		ReceivedAt:     receivedAt,
	}
}
