// Package licensestore holds the rule by which an instance accepts a license key into its
// database: a key is stored only when it is signed by a key of the ring and was issued after
// the one in force. A key received as a renewal has, besides, to be for the customer of the one
// in force.
package licensestore

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Origin is where a stored key came from. The values are the ones the table allows.
type Origin string

const (
	OriginInterface   Origin = "interface"
	OriginEnvironment Origin = "environment"
	OriginFile        Origin = "file"
	OriginRenewal     Origin = "renewal"
)

// Outcome is what the rule decided about an offered key.
type Outcome int

// The outcomes start at one: the zero value is none of them, so that a Result nobody filled in
// does not read as a key that was accepted.
const (
	// Accepted: the key was stored and is now the one in force.
	Accepted Outcome = iota + 1
	// Unchanged: the key is the one already in force.
	Unchanged
	// RefusedOlder: the key was not issued after the one in force.
	RefusedOlder
	// RefusedInvalid: the key is empty, unreadable or not signed by a key of the ring.
	RefusedInvalid
	// RefusedOtherCustomer: the key was offered as a renewal and is not for the customer of the
	// key in force.
	RefusedOtherCustomer
	// RefusedNothingToRenew: the key was offered as a renewal, and no key is in force for it to
	// renew, or the one in force is a key the ring no longer reads: nothing tells whose it is.
	RefusedNothingToRenew
)

// String is the outcome in one word, for a log line.
func (o Outcome) String() string {
	switch o {
	case Accepted:
		return "accepted"
	case Unchanged:
		return "unchanged"
	case RefusedOlder:
		return "older"
	case RefusedInvalid:
		return "invalid"
	case RefusedOtherCustomer:
		return "other_customer"
	case RefusedNothingToRenew:
		return "nothing_to_renew"
	}
	return "none"
}

// Result is the verdict on an offered key. Reason is set for a refusal and never contains a
// key value. Key is the key that was read; it is nil when none could be.
type Result struct {
	Outcome Outcome
	Reason  string
	Key     *license.Key
}

// Store keeps the license keys of the instance.
type Store struct {
	db   *husonymdb.HusonymDb
	ring license.Keyring

	// doorProblems is why the key each door holds was last refused as invalid, in the memory
	// of this process alone. A door is where a key is offered with nobody to read the answer:
	// the variable and the file.
	doorMu       sync.Mutex
	doorProblems map[Origin]string
}

func New(db *husonymdb.HusonymDb, ring license.Keyring) *Store {
	return &Store{db: db, ring: ring, doorProblems: map[Origin]string{}}
}

// doors are the origins that offer a key with nobody to read the answer, in the order their
// problems are told; doorSettings names the setting each one reads.
var (
	doors        = []Origin{OriginFile, OriginEnvironment}
	doorSettings = map[Origin]string{
		OriginFile:        "EE_LICENSE_FILE",
		OriginEnvironment: "EE_LICENSE",
	}
)

// DoorProblem tells why the key a door holds was refused as invalid the last time it was
// offered, or nothing. It is for the description of an instance that holds no key: a key
// truncated in the settings of a deployment would otherwise read as no key at all. A key older
// than the one in force is no problem, and a key that was accepted since, wherever it came
// from, clears everything. The reason never contains a key value.
func (s *Store) DoorProblem() string {
	s.doorMu.Lock()
	defer s.doorMu.Unlock()
	for _, door := range doors {
		if reason, ok := s.doorProblems[door]; ok {
			return fmt.Sprintf("the license key given in %s is not valid: %s", doorSettings[door], reason)
		}
	}
	return ""
}

// rememberDoor keeps what the store decided about the key of a door.
func (s *Store) rememberDoor(origin Origin, result *Result) {
	s.doorMu.Lock()
	defer s.doorMu.Unlock()
	if result.Outcome == Accepted {
		clear(s.doorProblems)
		return
	}
	if !slices.Contains(doors, origin) {
		return
	}
	if result.Outcome == RefusedInvalid {
		s.doorProblems[origin] = result.Reason
		return
	}
	delete(s.doorProblems, origin)
}

const environmentPrefix = "EE_LICENSE="

// Clean gives the key value out of what was pasted or read from a file or a variable: it drops
// every whitespace character and one EE_LICENSE= prefix. The alphabet of a key holds no
// whitespace, so nothing of the key is lost.
func Clean(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), environmentPrefix)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

// Offer puts a key forward as the one in force. A refusal is a Result; the error is for a
// database that does not answer, which says nothing about the key.
func (s *Store) Offer(ctx context.Context, value string, origin Origin, userId *pgtype.UUID) (*Result, error) {
	result, err := s.decide(ctx, value, origin, userId)
	if err != nil {
		return nil, err
	}
	s.rememberDoor(origin, result)
	return result, nil
}

// decide applies the rule to an offered key, and stores the key it accepts.
func (s *Store) decide(ctx context.Context, value string, origin Origin, userId *pgtype.UUID) (*Result, error) {
	value = Clean(value)
	if value == "" {
		return &Result{Outcome: RefusedInvalid, Reason: "no license key was given"}, nil
	}
	// The error names the stage that failed and never echoes the key, so it can be shown.
	key, err := license.ParseWith(value, s.ring)
	if err != nil {
		return &Result{Outcome: RefusedInvalid, Reason: err.Error()}, nil
	}

	var result *Result
	// Read committed, whatever the default of the server: an offer that waited for the lock has
	// to see the key the other one stored, which a snapshot taken before the wait would not show.
	opts := &pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	err = s.db.WithTx(ctx, opts, func(dbtx husonymdb.BaseDBTX) error {
		// Two offers, wherever they are made, are looked at one after the other.
		if err := s.db.Q.LockLicenseKeys(ctx, dbtx); err != nil {
			return err
		}
		current, err := s.db.Q.GetCurrentLicenseKey(ctx, dbtx)
		if err != nil && !husonymdb.IsNoRows(err) {
			return err
		}
		if err != nil && origin == OriginRenewal {
			// A renewal succeeds the license in force: without one there is nothing it renews.
			result = &Result{
				Outcome: RefusedNothingToRenew,
				Reason:  "this license key was received as a renewal, and the instance holds no license key to renew",
				Key:     key,
			}
			return nil
		}
		if err == nil {
			if current.Key == value {
				result = &Result{Outcome: Unchanged, Key: key}
				return nil
			}
			if origin == OriginRenewal {
				if outcome, reason := s.notARenewalOf(current.Key, key); reason != "" {
					result = &Result{Outcome: outcome, Reason: reason, Key: key}
					return nil
				}
			}
			if !key.IssuedAt.After(current.IssuedAt.Time) {
				result = &Result{
					Outcome: RefusedOlder,
					Reason: fmt.Sprintf(
						"this license key was issued on %s, which is not after the key in force, issued on %s",
						key.IssuedAt.UTC().Format(time.RFC3339), current.IssuedAt.Time.UTC().Format(time.RFC3339)),
					Key: key,
				}
				return nil
			}
		}

		userUuid := pgtype.UUID{}
		if userId != nil {
			userUuid = *userId
		}
		if _, err := s.db.Q.InsertLicenseKey(ctx, dbtx, db_queries.InsertLicenseKeyParams{
			Key:             value,
			LicenseID:       key.Id,
			IssuedAt:        pgtype.Timestamptz{Time: key.IssuedAt, Valid: true},
			Origin:          string(origin),
			CreatedByUserID: userUuid,
		}); err != nil {
			return err
		}
		result = &Result{Outcome: Accepted, Key: key}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// notARenewalOf tells why a key received as a renewal does not renew the key in force, with the
// outcome that says so, or gives no reason when it does. The two customer ids are compared as
// they are written: two that are empty are the same customer, one that is empty and one that is
// not are two. A key in force that this ring no longer reads names no customer a renewal could
// be told to be for: there is then nothing to renew, which is not the key of another customer.
// The reason quotes neither key and neither customer id.
func (s *Store) notARenewalOf(inForce string, offered *license.Key) (outcome Outcome, reason string) {
	current, err := license.ParseWith(inForce, s.ring)
	if err != nil {
		return RefusedNothingToRenew,
			"this license key was received as a renewal, and the license key in force cannot be read to tell its customer"
	}
	if current.CustomerId != offered.CustomerId {
		return RefusedOtherCustomer,
			"this license key was received as a renewal, and is not for the customer of the license key in force"
	}
	return 0, ""
}

// Current gives the value of the key in force, or an empty string when the instance has none.
func (s *Store) Current(ctx context.Context) (string, error) {
	current, err := s.db.Q.GetCurrentLicenseKey(ctx, s.db.Db)
	if husonymdb.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return current.Key, nil
}

// Installation is how a key reached the instance and when it was stored.
type Installation struct {
	Origin Origin
	At     time.Time
}

// Installation tells how the key of the license with this id was stored, or gives nothing when
// the instance holds no such key. It is asked by id, not for the key in force, so that the
// answer describes the key the caller holds even when a newer one was stored since.
func (s *Store) Installation(ctx context.Context, licenseId string) (*Installation, error) {
	row, err := s.db.Q.GetLicenseKeyByLicenseId(ctx, s.db.Db, licenseId)
	if husonymdb.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Installation{Origin: Origin(row.Origin), At: row.CreatedAt.Time}, nil
}
