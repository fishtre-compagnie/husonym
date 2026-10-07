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
)

// Outcome is what became of a report that was received.
type Outcome int

const (
	// Stored: the report was checked against its license and stored.
	Stored Outcome = iota
	// Pending: no issued license has the fingerprint yet; the report is kept as received.
	Pending
	// Repeat: the same document was already stored for that instance and day.
	Repeat
	// Conflict: another document was already stored for that instance and day. It stays, and
	// the conflict is counted.
	Conflict
	// Refused: the caller got something wrong. The seal or the fingerprint is malformed, the
	// document is outside the schema, the seal does not match, or the fingerprint given aside
	// is not the one the document tells.
	Refused
	// Full: a cap on the pending reports is reached; the caller may try again later.
	Full
)

// hexShape is the one spelling of a seal and of a fingerprint: 32 bytes in lowercase hex.
var hexShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

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
// What the caller got wrong is told by Refused, never by an error: a non-nil error is a failure
// of ours, and the outcome that comes with it is Full, as the caller may try again in both cases.
func (i *Intake) Receive(ctx context.Context, document []byte, seal, fingerprint string) (Outcome, error) {
	if !hexShape.MatchString(seal) || !hexShape.MatchString(fingerprint) {
		return Refused, nil
	}
	told, ok := read(document)
	if !ok || told.fingerprint != fingerprint {
		return Refused, nil
	}
	receivedAt := i.now()

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
	outcome, err := i.store.StoreReport(ctx, told.report(issued, document, seal, receivedAt))
	if err != nil {
		return Full, err
	}
	return outcomeOf(outcome), nil
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
// has. Each one is checked as a report received is, then stored or discarded, and removed from
// pending, one report per transaction: a report discarded does not hold the others back. A
// report discarded for its seal is counted under the license, on the day of the promotion.
//
// stored counts the reports that verify, including one that turns out to be a repeat of, or in
// conflict with, a report stored in the meantime. A report stored keeps the moment it was
// received. Nothing is done for a fingerprint no license has.
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
		verified, err := i.promote(ctx, issued, &pending[n])
		if err != nil {
			return stored, discarded, err
		}
		if verified {
			stored++
		} else {
			discarded++
		}
	}
	return stored, discarded, nil
}

// promote stores one pending report of a license, or discards it; verified tells which.
func (i *Intake) promote(ctx context.Context, issued *cpstore.License, pending *cpstore.PendingReport) (verified bool, err error) {
	told, ok := read(pending.Document)
	if !ok || told.fingerprint != pending.KeyFingerprint {
		// Receive lets no such report in; one that is there all the same is of no use.
		return false, i.store.DiscardPendingReport(ctx, pending, issued.Id, nil)
	}
	if !sealed(issued, pending.Document, pending.Seal) {
		at := i.now()
		return false, i.store.DiscardPendingReport(ctx, pending, issued.Id, &at)
	}
	_, err = i.store.PromotePendingReport(ctx, pending,
		told.report(issued, pending.Document, pending.Seal, pending.ReceivedAt))
	return err == nil, err
}

// sealed says whether seal is the seal of document under the license. A key the seal cannot be
// computed from says no as well: the store only holds keys that were verified.
func sealed(issued *cpstore.License, document []byte, seal string) bool {
	return telemetry.Verify(issued.Encoded, document, seal) == nil
}

func outcomeOf(outcome cpstore.ReportOutcome) Outcome {
	switch outcome {
	case cpstore.ReportRepeat:
		return Repeat
	case cpstore.ReportConflict:
		return Conflict
	default:
		return Stored
	}
}

// reading is what the intake reads in a document.
type reading struct {
	day         time.Time
	fingerprint string
	instanceID  string
	version     string
	// installKind is empty when the document carries no diagnostics.
	installKind string
}

// read checks a document against the schema of the report and reads what the intake needs in
// it; ok is false for a document that is not a report.
func read(document []byte) (told *reading, ok bool) {
	// The schema is checked on the decoded document, which takes bytes that are not UTF-8;
	// the database would not.
	if !utf8.Valid(document) || telemetry.Validate(document) != nil {
		return nil, false
	}
	var report struct {
		Day            string `json:"day"`
		Identification struct {
			KeyFingerprint string `json:"key_fingerprint"`
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
	if err != nil {
		return nil, false
	}
	told = &reading{
		day:         day,
		fingerprint: report.Identification.KeyFingerprint,
		instanceID:  report.Identification.InstanceID,
		version:     report.Version.Husonym,
	}
	if report.Diagnostics != nil {
		told.installKind = report.Diagnostics.Installation.Kind
	}
	return told, true
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
