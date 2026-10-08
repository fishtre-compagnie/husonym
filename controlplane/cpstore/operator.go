package cpstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// What the operator changes from the console. Each write goes with its line of the journal in
// one transaction: none is kept without the other.

var (
	// ErrCustomerExists is returned when a customer already has the external id asked for.
	ErrCustomerExists = errors.New("a customer already has this external id")
	// ErrCustomerIncomplete is returned for a customer without an external id or without a name.
	ErrCustomerIncomplete = errors.New("a customer needs an external id and a name")
	// ErrAlreadySucceeded is returned when the license to succeed already has a successor.
	ErrAlreadySucceeded = errors.New("the license already has a successor")
	// ErrOtherCustomer is returned when the license to succeed is the one of another customer.
	ErrOtherCustomer = errors.New("the license to succeed belongs to another customer")
)

// OperatorActionKind names what a line of the journal tells.
type OperatorActionKind string

const (
	ActionCustomerCreated OperatorActionKind = "customer_created"
	ActionCustomerUpdated OperatorActionKind = "customer_updated"
	ActionLicenseIssued   OperatorActionKind = "license_issued"
	ActionLicenseRenewed  OperatorActionKind = "license_renewed"
	ActionLicenseKeyShown OperatorActionKind = "license_key_shown"
)

// JournalCap is the most lines Journal gives at once.
const JournalCap = 1000

// originConsole marks a license issued from the console.
const originConsole = "console"

// noteChanged is what the journal says of the note of a customer that was changed.
const noteChanged = "changed"

// The index that gives a license one successor at most, and the code PostgreSQL refuses with.
const (
	oneSuccessorIndex   = "licenses_succeeds_license_id_idx"
	uniqueViolationCode = "23505"
)

// NewCustomer is a customer to create.
type NewCustomer struct {
	// ExternalID is the identifier the keys of the customer carry. It never changes afterwards.
	ExternalID string
	Name       string
	Note       string
}

// OperatorAction is a line of the journal.
type OperatorAction struct {
	ID       int64
	At       time.Time
	Operator string
	Action   OperatorActionKind
	// CustomerID is uuid.Nil, and CustomerName empty, for an act on no customer. The name is the
	// one the customer bears now.
	CustomerID   uuid.UUID
	CustomerName string
	// LicenseID is empty for an act on no license.
	LicenseID string
	// Detail is what the act was about. It never holds a key.
	Detail map[string]string
}

// CreateCustomer records a customer and journals it. The external id and the name are stored
// without the space around them. ErrCustomerIncomplete when either is missing, ErrCustomerExists
// when a customer already has the external id.
func (s *Store) CreateCustomer(
	ctx context.Context, operator string, c NewCustomer, now time.Time,
) (uuid.UUID, error) {
	externalID, name := strings.TrimSpace(c.ExternalID), strings.TrimSpace(c.Name)
	if externalID == "" || name == "" {
		return uuid.Nil, ErrCustomerIncomplete
	}
	var id pgtype.UUID
	err := s.write(ctx, func(queries *cpdb.Queries) error {
		var err error
		id, err = queries.InsertCustomer(ctx, cpdb.InsertCustomerParams{
			ExternalID: externalID,
			Name:       name,
			Note:       c.Note,
			Now:        toTimestamptz(now),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCustomerExists
		}
		if err != nil {
			return fmt.Errorf("unable to record a customer: %w", err)
		}
		return journal(ctx, queries, now, operator, ActionCustomerCreated, id, "",
			map[string]string{"external_id": externalID, "name": name})
	})
	if err != nil {
		return uuid.Nil, err
	}
	return id.Bytes, nil
}

// UpdateCustomer changes the name and the note of a customer and journals it, with the name
// before and after and, when the note changed, that it did: the note itself is never in the
// journal. When neither differs from what is stored, nothing is written and nothing is journaled.
// The external id is not changed: the keys issued carry it. ErrNotFound when no customer has the
// id, ErrCustomerIncomplete without a name.
func (s *Store) UpdateCustomer(
	ctx context.Context, operator string, id uuid.UUID, name, note string, now time.Time,
) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrCustomerIncomplete
	}
	customerID := pgtype.UUID{Bytes: id, Valid: true}
	return s.write(ctx, func(queries *cpdb.Queries) error {
		stored, err := queries.GetCustomerNameAndNoteForUpdate(ctx, customerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("unable to read a customer: %w", err)
		}
		if stored.Name == name && stored.Note == note {
			return nil
		}
		detail := map[string]string{"old_name": stored.Name, "new_name": name}
		if stored.Note != note {
			detail["note"] = noteChanged
		}
		err = queries.UpdateCustomer(ctx, cpdb.UpdateCustomerParams{
			ID:   customerID,
			Name: name,
			Note: note,
			Now:  toTimestamptz(now),
		})
		if err != nil {
			return fmt.Errorf("unable to update a customer: %w", err)
		}
		return journal(ctx, queries, now, operator, ActionCustomerUpdated, customerID, "", detail)
	})
}

// RecordIssuedLicense records a license issued from the console and journals it, as issued or,
// when succeeds names the license it succeeds, as renewed.
//
// key is the content of the key of issued, already verified by the caller: as for AddLicense,
// everything the key carries is stored from it. issued supplies the encoded value, stored without
// the space around it, and the kid; signingKeyFingerprint is the fingerprint of the public key
// that verifies it (license.PublicKeyFingerprint), which is what a registry entry gives.
//
// The customer the key names must exist, ErrNotFound otherwise: it is not created here. So must
// the license to succeed (ErrNotFound), which must be one of that customer (ErrOtherCustomer) and
// have no successor yet (ErrAlreadySucceeded, also when two renewals race). A license id already
// present is left as it is: added is false and nothing is journaled.
func (s *Store) RecordIssuedLicense(
	ctx context.Context, operator string, key *license.Key, issued *license.IssuedLicense,
	signingKeyFingerprint, succeeds, note string, now time.Time,
) (added bool, err error) {
	err = s.write(ctx, func(queries *cpdb.Queries) error {
		customerID, err := queries.GetCustomerIDByExternalID(ctx, key.CustomerId)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("unable to read the customer of a license: %w", err)
		}
		action := ActionLicenseIssued
		detail := map[string]string{
			"plan":       key.Plan,
			"expires_at": key.ExpiresAt.UTC().Format(time.RFC3339),
			"telemetry":  string(key.TelemetryMode()),
		}
		if succeeds != "" {
			owner, err := queries.GetLicenseCustomer(ctx, succeeds)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("unable to read the license to succeed: %w", err)
			}
			if owner != customerID {
				return ErrOtherCustomer
			}
			action = ActionLicenseRenewed
			detail["succeeds"] = succeeds
		}

		added, err = insertLicense(ctx, queries, customerID, key, &licenseRecord{
			Encoded:               strings.TrimSpace(issued.Encoded),
			Kid:                   issued.Kid,
			SigningKeyFingerprint: signingKeyFingerprint,
			Note:                  note,
			Origin:                originConsole,
			Succeeds:              succeeds,
		})
		var refused *pgconn.PgError
		if errors.As(err, &refused) && refused.Code == uniqueViolationCode && refused.ConstraintName == oneSuccessorIndex {
			return ErrAlreadySucceeded
		}
		if err != nil {
			return err
		}
		if !added {
			return nil
		}
		return journal(ctx, queries, now, operator, action, customerID, key.Id, detail)
	})
	if err != nil {
		return false, err
	}
	return added, nil
}

// ShowLicenseKey gives the encoded key of a license and journals that it was shown. It is the
// only read that returns a key besides LicenseByFingerprint and LatestSuccessor. ErrNotFound when
// no license has the id, and nothing is journaled then.
func (s *Store) ShowLicenseKey(
	ctx context.Context, operator, licenseID string, now time.Time,
) (encoded string, err error) {
	err = s.write(ctx, func(queries *cpdb.Queries) error {
		row, err := queries.GetLicenseKey(ctx, licenseID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("unable to read the key of a license: %w", err)
		}
		encoded = row.Encoded
		return journal(ctx, queries, now, operator, ActionLicenseKeyShown, row.CustomerID, licenseID, nil)
	})
	if err != nil {
		return "", err
	}
	return encoded, nil
}

// Journal gives the latest lines of the journal, the newest first, limit of them at most; limit
// is brought within 1 and JournalCap.
func (s *Store) Journal(ctx context.Context, limit int) ([]OperatorAction, error) {
	limit = min(max(limit, 1), JournalCap)
	rows, err := cpdb.New(s.pool).ListOperatorActions(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("unable to list the journal of the operator: %w", err)
	}
	actions := make([]OperatorAction, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		action := OperatorAction{
			ID:           row.ID,
			At:           toTime(row.At),
			Operator:     row.Operator,
			Action:       OperatorActionKind(row.Action),
			CustomerID:   row.CustomerID.Bytes,
			CustomerName: row.CustomerName.String,
			LicenseID:    row.LicenseID.String,
		}
		if err := json.Unmarshal(row.Detail, &action.Detail); err != nil {
			return nil, fmt.Errorf("unable to read the detail of a line of the journal: %w", err)
		}
		actions = append(actions, action)
	}
	return actions, nil
}

// write runs do in a transaction, committed when do returns nil and rolled back otherwise.
func (s *Store) write(ctx context.Context, do func(queries *cpdb.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("unable to begin a write of the operator: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := do(cpdb.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("unable to commit a write of the operator: %w", err)
	}
	return nil
}

// journal writes a line of the journal through queries, so in the transaction of the act it
// tells. licenseID is empty for an act on no license.
func journal(
	ctx context.Context, queries *cpdb.Queries, now time.Time, operator string, action OperatorActionKind,
	customerID pgtype.UUID, licenseID string, detail map[string]string,
) error {
	if detail == nil {
		detail = map[string]string{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("unable to encode the detail of a line of the journal: %w", err)
	}
	err = queries.InsertOperatorAction(ctx, cpdb.InsertOperatorActionParams{
		At:         toTimestamptz(now),
		Operator:   operator,
		Action:     string(action),
		CustomerID: customerID,
		LicenseID:  pgtype.Text{String: licenseID, Valid: licenseID != ""},
		Detail:     encoded,
	})
	if err != nil {
		return fmt.Errorf("unable to write the journal of the operator: %w", err)
	}
	return nil
}
