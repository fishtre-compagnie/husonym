package cpstore

import (
	"context"
	"fmt"
	"time"

	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Attention is what an operator should have a look at.
type Attention struct {
	// SilentInstances are the instances that stopped reporting while their license is in force
	// and asks them to report, the one silent for the longest first.
	SilentInstances []SilentInstance
	// ExpiringLicenses are the licenses in force that expire within ExpiringWithin, or have
	// expired, and that no other license succeeds; the one expiring first comes first.
	ExpiringLicenses []LicenseSummary
	// OldPending are the fingerprints that have a report pending for more than OldPendingAfter,
	// each with everything pending under it.
	OldPending []PendingGroup
	// SealRejections are the ones of the last SealRejectionDays days, the latest day first.
	SealRejections []SealRejection
	// SharedLicenses are the licenses seen on more than one instance in the last
	// RecentInstanceDays days.
	SharedLicenses []SharedLicense
}

// SilentInstance is an instance that stopped reporting, with the customer it is of.
type SilentInstance struct {
	InstanceSummary
	CustomerID   uuid.UUID
	CustomerName string
}

// SharedLicense is a license seen lately on several instances.
type SharedLicense struct {
	LicenseSummary
	// RecentInstances is how many instances it was seen on in the last RecentInstanceDays days.
	RecentInstances int
}

// AttentionCounts is how much there is of each thing of Attention.
type AttentionCounts struct {
	SilentInstances  int
	ExpiringLicenses int
	// OldPending counts the reports pending for more than OldPendingAfter, not their
	// fingerprints: it is the sum of OldReports over Attention.OldPending.
	OldPending int
	// SealRejections counts the reports refused for their seal on the UTC day of the instant
	// asked for, where Attention.SealRejections lists the last days.
	SealRejections int
	SharedLicenses int
}

// Attention lists what is worth a look at now.
func (s *Store) Attention(ctx context.Context, now time.Time) (*Attention, error) {
	queries := cpdb.New(s.pool)
	silent, err := silentInstances(ctx, queries, now)
	if err != nil {
		return nil, err
	}
	expiring, err := expiringLicenses(ctx, queries, now)
	if err != nil {
		return nil, err
	}
	oldPending, err := s.oldPending(ctx, now)
	if err != nil {
		return nil, err
	}
	rejections, err := sealRejections(ctx, queries, now, pgtype.Text{})
	if err != nil {
		return nil, err
	}
	shared, err := sharedLicenses(ctx, queries, now)
	if err != nil {
		return nil, err
	}
	return &Attention{
		SilentInstances:  silent,
		ExpiringLicenses: expiring,
		OldPending:       oldPending,
		SealRejections:   rejections,
		SharedLicenses:   shared,
	}, nil
}

// AttentionCounts counts what is worth a look at now. The silent instances, the expiring
// licenses, the old pending reports and the shared licenses are counted from the very lists
// Attention gives, so that a number never disagrees with the page it leads to.
func (s *Store) AttentionCounts(ctx context.Context, now time.Time) (AttentionCounts, error) {
	queries := cpdb.New(s.pool)
	silent, err := silentInstances(ctx, queries, now)
	if err != nil {
		return AttentionCounts{}, err
	}
	expiring, err := expiringLicenses(ctx, queries, now)
	if err != nil {
		return AttentionCounts{}, err
	}
	oldPending, err := s.oldPending(ctx, now)
	if err != nil {
		return AttentionCounts{}, err
	}
	rejected, err := queries.SumSealRejectionsOfDay(ctx, utcDate(now))
	if err != nil {
		return AttentionCounts{}, fmt.Errorf("unable to count the seal rejections of the day: %w", err)
	}
	shared, err := sharedLicenses(ctx, queries, now)
	if err != nil {
		return AttentionCounts{}, err
	}
	counts := AttentionCounts{
		SilentInstances:  len(silent),
		ExpiringLicenses: len(expiring),
		SealRejections:   int(rejected),
		SharedLicenses:   len(shared),
	}
	for i := range oldPending {
		counts.OldPending += oldPending[i].OldReports
	}
	return counts, nil
}

// silentInstances lists the instances whose last report is of a day more than SilentAfterDays
// and no more than RecentInstanceDays days before the UTC day of now, under a license in force
// whose telemetry is online.
//
// Whether a license is in force and what its telemetry means are what its key says, which Go
// reads: the query only bounds the day of the last report, which cannot leave a silent instance
// out, and the licenses are judged here.
func silentInstances(ctx context.Context, queries *cpdb.Queries, now time.Time) ([]SilentInstance, error) {
	rows, err := queries.ListSilentCandidates(ctx, cpdb.ListSilentCandidatesParams{
		FromDay:   recentSince(now),
		BeforeDay: utcDate(now.UTC().AddDate(0, 0, -SilentAfterDays)),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the silent instances: %w", err)
	}
	silent := make([]SilentInstance, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		key := &license.Key{ExpiresAt: row.ExpiresAt.Time, GraceDays: toGraceDays(row.GraceDays), Telemetry: row.Telemetry}
		if key.StateAt(now) == license.StateFrozen || key.TelemetryMode() != license.TelemetryOnline {
			continue
		}
		silent = append(silent, SilentInstance{
			InstanceSummary: instanceSummary(&row.ControlplaneInstance),
			CustomerID:      row.CustomerID.Bytes,
			CustomerName:    row.CustomerName,
		})
	}
	return silent, nil
}

// expiringLicenses lists the licenses in force that expire within ExpiringWithin of now, or have
// expired, and that no other license succeeds.
//
// When the grace period of a license runs out is what its key says, which Go reads: the query
// takes every license without a successor that expires before the end of the window, however long
// ago, which cannot leave one in grace out, and the frozen ones are dropped here.
func expiringLicenses(ctx context.Context, queries *cpdb.Queries, now time.Time) ([]LicenseSummary, error) {
	rows, err := queries.ListExpiringCandidates(ctx, toTimestamptz(now.Add(ExpiringWithin)))
	if err != nil {
		return nil, fmt.Errorf("unable to list the expiring licenses: %w", err)
	}
	expiring := make([]LicenseSummary, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		summary := licenseSummary(
			row.ID, row.CustomerID, row.CustomerName, row.Plan, row.Telemetry, row.ExpiresAt, row.GraceDays, false, now)
		if summary.State == license.StateFrozen {
			continue
		}
		expiring = append(expiring, summary)
	}
	return expiring, nil
}

// oldPending gives the groups of PendingByFingerprint that have an old report.
func (s *Store) oldPending(ctx context.Context, now time.Time) ([]PendingGroup, error) {
	groups, err := s.PendingByFingerprint(ctx, now)
	if err != nil {
		return nil, err
	}
	old := make([]PendingGroup, 0, len(groups))
	for i := range groups {
		if groups[i].OldReports > 0 {
			old = append(old, groups[i])
		}
	}
	return old, nil
}

func sharedLicenses(ctx context.Context, queries *cpdb.Queries, now time.Time) ([]SharedLicense, error) {
	rows, err := queries.ListSharedLicenses(ctx, recentSince(now))
	if err != nil {
		return nil, fmt.Errorf("unable to list the shared licenses: %w", err)
	}
	shared := make([]SharedLicense, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		shared = append(shared, SharedLicense{
			LicenseSummary: licenseSummary(
				row.ID, row.CustomerID, row.CustomerName, row.Plan, row.Telemetry, row.ExpiresAt, row.GraceDays,
				row.HasSuccessor, now),
			RecentInstances: int(row.RecentInstances),
		})
	}
	return shared, nil
}
