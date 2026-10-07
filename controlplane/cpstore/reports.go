package cpstore

import (
	"context"
	"fmt"
	"time"

	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Report is a usage report whose seal was verified against its license.
type Report struct {
	InstanceID string
	Day        time.Time
	LicenseID  string
	// Document is the exact bytes received, which the seal is over.
	Document       []byte
	Seal           string
	HusonymVersion string
	// InstallKind is empty when the report does not tell it.
	InstallKind string
	ReceivedAt  time.Time
}

// ReportOutcome is what became of a report handed to StoreReport.
type ReportOutcome int

const (
	// ReportStored: the first report received for its instance and day.
	ReportStored ReportOutcome = iota
	// ReportRepeat: the same document was already stored for that instance and day.
	ReportRepeat
	// ReportConflict: another document was already stored for that instance and day; it stays,
	// and the row counts the conflict.
	ReportConflict
)

// PendingReport is a report received under a fingerprint no issued license has, kept as received.
type PendingReport struct {
	KeyFingerprint string
	InstanceID     string
	Day            time.Time
	Document       []byte
	Seal           string
	ReceivedAt     time.Time
}

// PendingCaps bound how many reports are kept pending.
type PendingCaps struct {
	PerFingerprint int64
	Total          int64
}

// StoreReport stores a report and records what it tells of its instance, in one transaction.
// The first report of an instance for a day stays: the same document again is a repeat, another
// one is a conflict, counted on the row. Two calls at once for the same instance and day end as
// one row, one of them stored and the other a repeat or a conflict.
func (s *Store) StoreReport(ctx context.Context, report *Report) (ReportOutcome, error) {
	var outcome ReportOutcome
	err := s.inTx(ctx, func(queries *cpdb.Queries) error {
		var err error
		outcome, err = storeReport(ctx, queries, report)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("unable to store a usage report: %w", err)
	}
	return outcome, nil
}

// storeReport does what StoreReport says, inside the transaction of its caller. The instance is
// written first, as the report refers to it; every caller takes the two rows in that order.
func storeReport(ctx context.Context, queries *cpdb.Queries, report *Report) (ReportOutcome, error) {
	day := utcDate(report.Day)
	err := queries.UpsertInstance(ctx, cpdb.UpsertInstanceParams{
		ID:             report.InstanceID,
		SeenAt:         toTimestamptz(report.ReceivedAt),
		ReportDay:      day,
		LicenseID:      report.LicenseID,
		HusonymVersion: report.HusonymVersion,
		InstallKind:    pgtype.Text{String: report.InstallKind, Valid: report.InstallKind != ""},
	})
	if err != nil {
		return 0, err
	}
	inserted, err := queries.InsertUsageReport(ctx, cpdb.InsertUsageReportParams{
		InstanceID: report.InstanceID,
		Day:        day,
		LicenseID:  report.LicenseID,
		Document:   string(report.Document),
		Seal:       report.Seal,
		ReceivedAt: toTimestamptz(report.ReceivedAt),
	})
	if err != nil {
		return 0, err
	}
	if inserted > 0 {
		return ReportStored, nil
	}
	differing, err := queries.CountUsageReportConflict(ctx, cpdb.CountUsageReportConflictParams{
		At:         toTimestamptz(report.ReceivedAt),
		InstanceID: report.InstanceID,
		Day:        day,
		Document:   string(report.Document),
	})
	if err != nil {
		return 0, err
	}
	if differing > 0 {
		return ReportConflict, nil
	}
	return ReportRepeat, nil
}

// CountSealRejection counts one report refused for its seal under a license, on the UTC day of at.
func (s *Store) CountSealRejection(ctx context.Context, licenseID string, at time.Time) error {
	if err := countSealRejection(ctx, cpdb.New(s.pool), licenseID, at); err != nil {
		return fmt.Errorf("unable to count a refused seal: %w", err)
	}
	return nil
}

func countSealRejection(ctx context.Context, queries *cpdb.Queries, licenseID string, at time.Time) error {
	return queries.CountSealRejection(ctx, cpdb.CountSealRejectionParams{
		LicenseID: licenseID,
		Day:       utcDate(at),
		LastAt:    toTimestamptz(at),
	})
}

// KeepPending keeps a report pending, unless a cap is reached: kept is then false. A report
// already pending for the same fingerprint, instance and day is left as it is and kept is true,
// whatever the caps.
//
// The caps are counted in the transaction that inserts, without a lock: calls at once may each
// see room and go over a cap by a few rows. The caps bound what a stranger can make the service
// keep; they do not need to be exact.
func (s *Store) KeepPending(ctx context.Context, pending *PendingReport, caps PendingCaps) (kept bool, err error) {
	day := utcDate(pending.Day)
	err = s.inTx(ctx, func(queries *cpdb.Queries) error {
		exists, err := queries.PendingReportExists(ctx, cpdb.PendingReportExistsParams{
			KeyFingerprint: pending.KeyFingerprint,
			InstanceID:     pending.InstanceID,
			Day:            day,
		})
		if err != nil {
			return err
		}
		if exists {
			kept = true
			return nil
		}
		counts, err := queries.CountPendingReports(ctx, pending.KeyFingerprint)
		if err != nil {
			return err
		}
		if counts.ForFingerprint >= caps.PerFingerprint || counts.Total >= caps.Total {
			return nil
		}
		kept = true
		return queries.InsertPendingReport(ctx, cpdb.InsertPendingReportParams{
			KeyFingerprint: pending.KeyFingerprint,
			InstanceID:     pending.InstanceID,
			Day:            day,
			Document:       string(pending.Document),
			Seal:           pending.Seal,
			ReceivedAt:     toTimestamptz(pending.ReceivedAt),
		})
	})
	if err != nil {
		return false, fmt.Errorf("unable to keep a usage report pending: %w", err)
	}
	return kept, nil
}

// PendingReports gives the reports pending under a fingerprint, the oldest day first.
func (s *Store) PendingReports(ctx context.Context, fingerprint string) ([]PendingReport, error) {
	rows, err := cpdb.New(s.pool).ListPendingReports(ctx, fingerprint)
	if err != nil {
		return nil, fmt.Errorf("unable to list the pending usage reports: %w", err)
	}
	pending := make([]PendingReport, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		pending = append(pending, PendingReport{
			KeyFingerprint: row.KeyFingerprint,
			InstanceID:     row.InstanceID,
			Day:            row.Day.Time,
			Document:       []byte(row.Document),
			Seal:           row.Seal,
			ReceivedAt:     row.ReceivedAt.Time,
		})
	}
	return pending, nil
}

// PromotePendingReport stores report, which a pending report turned out to be, and removes
// that one from pending, in one transaction.
func (s *Store) PromotePendingReport(ctx context.Context, pending *PendingReport, report *Report) (ReportOutcome, error) {
	var outcome ReportOutcome
	err := s.inTx(ctx, func(queries *cpdb.Queries) error {
		var err error
		if outcome, err = storeReport(ctx, queries, report); err != nil {
			return err
		}
		_, err = deletePending(ctx, queries, pending)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("unable to store a pending usage report: %w", err)
	}
	return outcome, nil
}

// DiscardPendingReport removes a report from pending without storing it. When sealRefusedAt is
// given, the report is discarded for its seal, and that is counted under the license on that
// day; it is not counted when the report was no longer pending.
func (s *Store) DiscardPendingReport(
	ctx context.Context, pending *PendingReport, licenseID string, sealRefusedAt *time.Time,
) error {
	err := s.inTx(ctx, func(queries *cpdb.Queries) error {
		deleted, err := deletePending(ctx, queries, pending)
		if err != nil || deleted == 0 || sealRefusedAt == nil {
			return err
		}
		return countSealRejection(ctx, queries, licenseID, *sealRefusedAt)
	})
	if err != nil {
		return fmt.Errorf("unable to discard a pending usage report: %w", err)
	}
	return nil
}

func deletePending(ctx context.Context, queries *cpdb.Queries, pending *PendingReport) (int64, error) {
	return queries.DeletePendingReport(ctx, cpdb.DeletePendingReportParams{
		KeyFingerprint: pending.KeyFingerprint,
		InstanceID:     pending.InstanceID,
		Day:            utcDate(pending.Day),
	})
}

// PendingFingerprintsNowKnown gives the fingerprints that have reports pending and that an
// issued license now has: the license was recorded after its reports came.
func (s *Store) PendingFingerprintsNowKnown(ctx context.Context) ([]string, error) {
	fingerprints, err := cpdb.New(s.pool).ListPendingFingerprintsNowKnown(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to list the pending fingerprints now known: %w", err)
	}
	return fingerprints, nil
}

// PurgePending removes the reports pending since before olderThan, and says how many.
func (s *Store) PurgePending(ctx context.Context, olderThan time.Time) (int64, error) {
	purged, err := cpdb.New(s.pool).PurgePendingReports(ctx, toTimestamptz(olderThan))
	if err != nil {
		return 0, fmt.Errorf("unable to purge the pending usage reports: %w", err)
	}
	return purged, nil
}

// inTx runs do in a transaction, committed when do returns nil.
func (s *Store) inTx(ctx context.Context, do func(queries *cpdb.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return do(cpdb.New(tx))
	})
}

func toTimestamptz(at time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: at, Valid: true}
}

// utcDate is the UTC day of at.
func utcDate(at time.Time) pgtype.Date {
	utc := at.UTC()
	return pgtype.Date{Time: time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
