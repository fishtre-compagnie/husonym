package cpstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrNoLicense is returned when no issued license has the fingerprint asked for.
var ErrNoLicense = errors.New("no license with this fingerprint")

// License is an issued license as the store knows it.
type License struct {
	Id        string
	Encoded   string
	Telemetry string
}

// AddLicense records an issued license under its customer, creating the customer on first
// sight. An existing customer keeps its name. A license id already present is left as it is
// and added is false.
//
// key is the content of the license key, already verified by the caller: everything it carries
// is stored from it. entry only supplies what the key does not carry: the encoded value, the kid,
// the fingerprint of the signing key and the note.
func (s *Store) AddLicense(
	ctx context.Context, key *license.Key, entry *license.RegistryEntry, origin string,
) (added bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("unable to begin the recording of a license: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := cpdb.New(tx)

	customerID, err := queries.UpsertCustomer(ctx, cpdb.UpsertCustomerParams{
		ExternalID: key.CustomerId,
		Name:       key.IssuedTo,
	})
	if err != nil {
		return false, fmt.Errorf("unable to record the customer of a license: %w", err)
	}
	added, err = insertLicense(ctx, queries, customerID, key, &licenseRecord{
		Encoded:               entry.Encoded,
		Kid:                   entry.Kid,
		SigningKeyFingerprint: entry.KeyFingerprint,
		Note:                  entry.Note,
		Origin:                origin,
	})
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("unable to commit the recording of a license: %w", err)
	}
	return added, nil
}

// licenseRecord is what the row of a license holds that its key does not carry.
type licenseRecord struct {
	Encoded               string
	Kid                   string
	SigningKeyFingerprint string
	Note                  string
	Origin                string
	// Succeeds is the id of the license this one succeeds, empty when it succeeds none.
	Succeeds string
}

// insertLicense writes the row of a license under a customer: every column the key carries is
// filled from key, the others from record. A license id already present is left as it is and
// added is false.
func insertLicense(
	ctx context.Context, queries *cpdb.Queries, customerID pgtype.UUID, key *license.Key, record *licenseRecord,
) (added bool, err error) {
	var limits []byte
	if key.Limits != nil {
		if limits, err = json.Marshal(key.Limits); err != nil {
			return false, fmt.Errorf("unable to encode the limits of a license: %w", err)
		}
	}
	var graceDays pgtype.Int4
	if key.GraceDays != nil {
		graceDays = pgtype.Int4{Int32: int32(*key.GraceDays), Valid: true} //nolint:gosec // a number of days
	}
	// A license already recorded is left alone on its id only: the same license written twice at
	// once would otherwise be refused on its key, which is unique too.
	if err := queries.LockLicenseID(ctx, key.Id); err != nil {
		return false, fmt.Errorf("unable to hold the id of a license: %w", err)
	}
	inserted, err := queries.InsertLicense(ctx, cpdb.InsertLicenseParams{
		ID:                    key.Id,
		CustomerID:            customerID,
		Encoded:               record.Encoded,
		KeyFingerprint:        telemetry.KeyFingerprint(record.Encoded),
		Kid:                   record.Kid,
		Plan:                  key.Plan,
		Features:              key.Features,
		Limits:                limits,
		Telemetry:             key.Telemetry,
		IssuedAt:              pgtype.Timestamptz{Time: key.IssuedAt, Valid: true},
		ExpiresAt:             pgtype.Timestamptz{Time: key.ExpiresAt, Valid: true},
		GraceDays:             graceDays,
		SigningKeyFingerprint: record.SigningKeyFingerprint,
		Note:                  record.Note,
		Origin:                record.Origin,
		SucceedsLicenseID:     pgtype.Text{String: record.Succeeds, Valid: record.Succeeds != ""},
	})
	if err != nil {
		return false, fmt.Errorf("unable to record a license: %w", err)
	}
	return inserted > 0, nil
}

// LicenseByFingerprint returns the license whose key has the fingerprint, or ErrNoLicense.
func (s *Store) LicenseByFingerprint(ctx context.Context, fingerprint string) (*License, error) {
	row, err := cpdb.New(s.pool).GetLicenseByFingerprint(ctx, fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoLicense
	}
	if err != nil {
		return nil, fmt.Errorf("unable to look up a license by fingerprint: %w", err)
	}
	return &License{Id: row.ID, Encoded: row.Encoded, Telemetry: row.Telemetry}, nil
}
