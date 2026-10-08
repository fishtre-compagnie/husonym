package cpstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// What the renewal of a license reads and records.

const (
	// SuccessorChainBound is how many successors away from a license its chain is followed at most.
	SuccessorChainBound = 64
	// RenewalAsksPerLicense is on how many instances the asks for the renewal of a license are
	// recorded at most: the ones that asked last.
	RenewalAsksPerLicense = 50
)

// RenewalAsk is what an instance last asked of the renewal of a license.
type RenewalAsk struct {
	InstanceID  string
	LastAskedAt time.Time
	// ServedLicenseID is the license last served to the instance, at ServedAt; empty and zero
	// when none ever was.
	ServedLicenseID string
	ServedAt        time.Time
}

// LatestSuccessor gives the last license of the chain of successors of a license: the one that
// succeeds it, or the one that succeeds that one, and so on to the end. It is nil when nothing
// succeeds the license, and for a license that is not recorded.
//
// The chain is followed SuccessorChainBound successors away at most. cutShort tells the license
// returned is that far and has a successor of its own: the chain goes on, or loops.
//
// With LicenseByFingerprint and ShowLicenseKey, it is one of the three reads that return a key.
func (s *Store) LatestSuccessor(ctx context.Context, licenseID string) (successor *License, cutShort bool, err error) {
	row, err := cpdb.New(s.pool).GetLatestSuccessor(ctx, cpdb.GetLatestSuccessorParams{
		LicenseID: licenseID,
		MaxDepth:  SuccessorChainBound,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("unable to follow the successors of a license: %w", err)
	}
	return &License{Id: row.ID, Encoded: row.Encoded, Telemetry: row.Telemetry}, row.CutShort, nil
}

// RecordRenewalAsk records that an instance asked for the renewal of a license at the instant at,
// and the license that was served to it; served is empty when there was nothing to give, and what
// was served before then stays. There is one row per license and instance, and
// RenewalAsksPerLicense rows per license at most: an instance that is not recorded yet takes the
// place of the one that asked the longest ago, so that the rows are those of the instances that
// ask. Asks made at the same moment may leave a few rows more, until the next new instance.
func (s *Store) RecordRenewalAsk(ctx context.Context, licenseID, instanceID string, at time.Time, served string) error {
	err := cpdb.New(s.pool).UpsertRenewalAsk(ctx, cpdb.UpsertRenewalAskParams{
		LicenseID:       licenseID,
		InstanceID:      instanceID,
		At:              toTimestamptz(at),
		ServedLicenseID: pgtype.Text{String: served, Valid: served != ""},
		MaxInstances:    RenewalAsksPerLicense,
	})
	if err != nil {
		return fmt.Errorf("unable to record an ask for the renewal of a license: %w", err)
	}
	return nil
}

// renewalAsks lists what the instances asked of the renewal of a license, the last ask first.
func renewalAsks(ctx context.Context, queries *cpdb.Queries, licenseID string) ([]RenewalAsk, error) {
	rows, err := queries.ListRenewalAsksOfLicense(ctx, licenseID)
	if err != nil {
		return nil, fmt.Errorf("unable to list the asks for the renewal of a license: %w", err)
	}
	asks := make([]RenewalAsk, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		asks = append(asks, RenewalAsk{
			InstanceID:      row.InstanceID,
			LastAskedAt:     toTime(row.LastAskedAt),
			ServedLicenseID: row.LastServedLicenseID.String,
			ServedAt:        toTime(row.LastServedAt),
		})
	}
	return asks, nil
}
