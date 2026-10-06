// Package licensestore holds the rule by which an instance accepts a license key into its
// database: a key is stored only when it is signed by a key of the ring and was issued after
// the one in force.
package licensestore

import (
	"context"
	"fmt"
	"strings"
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

const (
	// Accepted: the key was stored and is now the one in force.
	Accepted Outcome = iota
	// Unchanged: the key is the one already in force.
	Unchanged
	// RefusedOlder: the key was not issued after the one in force.
	RefusedOlder
	// RefusedInvalid: the key is empty, unreadable or not signed by a key of the ring.
	RefusedInvalid
)

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
}

func New(db *husonymdb.HusonymDb, ring license.Keyring) *Store {
	return &Store{db: db, ring: ring}
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
		if err == nil {
			if current.Key == value {
				result = &Result{Outcome: Unchanged, Key: key}
				return nil
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
