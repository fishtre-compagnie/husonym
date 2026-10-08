package licensestore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	pgxmock "github.com/fishtre-compagnie/husonym/internal/mocks/github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// signedKey writes a key value the way the issuing tool does, without the checks the tool makes
// (such as refusing a key that has already expired): the acceptance rule does not look at expiry.
func signedKey(t *testing.T, priv ed25519.PrivateKey, issuedAt, expiresAt time.Time) string {
	t.Helper()
	return signedKeyWithKid(t, priv, "", issuedAt, expiresAt)
}

// signedKeyWithKid is signedKey with the kid written in the envelope; none when empty.
func signedKeyWithKid(t *testing.T, priv ed25519.PrivateKey, kid string, issuedAt, expiresAt time.Time) string {
	t.Helper()
	return signedKeyOf(t, priv, kid, "cust-001", issuedAt, expiresAt)
}

// signedKeyFor is signedKey for the customer of the given id, which may be empty.
func signedKeyFor(t *testing.T, priv ed25519.PrivateKey, customerId string, issuedAt, expiresAt time.Time) string {
	t.Helper()
	return signedKeyOf(t, priv, "", customerId, issuedAt, expiresAt)
}

func signedKeyOf(t *testing.T, priv ed25519.PrivateKey, kid, customerId string, issuedAt, expiresAt time.Time) string {
	t.Helper()
	content, err := json.Marshal(license.Key{
		Version:    "v1",
		Id:         "lic-" + issuedAt.Format(time.RFC3339),
		IssuedTo:   "Acme Co.",
		CustomerId: customerId,
		IssuedAt:   issuedAt,
		ExpiresAt:  expiresAt,
	})
	require.NoError(t, err)
	fields := map[string]string{
		"license":   base64.StdEncoding.EncodeToString(content),
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, content)),
	}
	if kid != "" {
		fields["kid"] = kid
	}
	envelope, err := json.Marshal(fields)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(envelope)
}

func newPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pub, priv
}

type fixture struct {
	store   *Store
	querier *db_queries.MockQuerier
	priv    ed25519.PrivateKey
	ring    license.Keyring
}

// newFixture gives a store whose database is a double. With inTransaction, the double allows
// the one read committed transaction that Offer begins, and the lock taken in it.
func newFixture(t *testing.T, inTransaction bool) *fixture {
	t.Helper()
	pub, priv := newPair(t)
	dbtx := husonymdb.NewMockDBTX(t)
	querier := db_queries.NewMockQuerier(t)
	if inTransaction {
		tx := pgxmock.NewMockTx(t)
		dbtx.On("BeginTx", mock.Anything, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}).Return(tx, nil)
		// Not called when the database fails: the transaction is then rolled back.
		tx.On("Commit", mock.Anything).Return(nil).Maybe()
		tx.On("Rollback", mock.Anything).Return(nil)
		querier.On("LockLicenseKeys", mock.Anything, tx).Return(nil)
	}
	ring := license.Keyring{license.LegacyKid: pub}
	return &fixture{
		store:   New(husonymdb.New(dbtx, querier), ring),
		querier: querier,
		priv:    priv,
		ring:    ring,
	}
}

func stored(value string, issuedAt time.Time) db_queries.HusonymApiLicenseKey {
	return db_queries.HusonymApiLicenseKey{
		Key:      value,
		IssuedAt: pgtype.Timestamptz{Time: issuedAt, Valid: true},
	}
}

func Test_Clean(t *testing.T) {
	require.Equal(t, "abc", Clean("  EE_LICENSE=abc\n"))
	require.Equal(t, "abc", Clean("a b\nc"))
	require.Equal(t, "abc", Clean("\t a b\r\n c \n"))
	require.Equal(t, "abc", Clean("EE_LICENSE= ab\nc"))
	// Only one prefix goes: a second one is part of what was given.
	require.Equal(t, "EE_LICENSE=abc", Clean("EE_LICENSE=EE_LICENSE=abc"))
	require.Empty(t, Clean(" \n"))
}

func Test_Offer_RefusesWhatIsNotAValidKeyBeforeTouchingTheDatabase(t *testing.T) {
	_, otherPriv := newPair(t)
	now := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name  string
		value func(f *fixture) string
	}{
		{"nothing given", func(*fixture) string { return " \n" }},
		{"not a key", func(*fixture) string { return "not-a-key" }},
		{"signed by a key the ring does not hold", func(*fixture) string {
			return signedKey(t, otherPriv, now, now.Add(time.Hour))
		}},
		{"names a kid the ring does not hold", func(f *fixture) string {
			return signedKeyWithKid(t, f.priv, "k-unknown", now, now.Add(time.Hour))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, false)
			value := tt.value(f)

			res, err := f.store.Offer(context.Background(), value, OriginInterface, nil)
			require.NoError(t, err)
			require.Equal(t, RefusedInvalid, res.Outcome)
			require.NotEmpty(t, res.Reason)
			if cleaned := Clean(value); cleaned != "" {
				_, parseErr := license.ParseWith(cleaned, f.ring)
				require.Error(t, parseErr)
				require.Equal(t, parseErr.Error(), res.Reason)
			}
			require.NotContains(t, res.Reason, value)
			require.Nil(t, res.Key)
			// The doubles fail the test on any call: nothing was read or written.
		})
	}
}

func Test_Offer(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	userId := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}

	t.Run("the first key is accepted and stored with where it came from", func(t *testing.T) {
		f := newFixture(t, true)
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiLicenseKey{}, pgx.ErrNoRows)
		f.querier.On("InsertLicenseKey", mock.Anything, mock.Anything, db_queries.InsertLicenseKeyParams{
			Key:             value,
			LicenseID:       "lic-" + now.Format(time.RFC3339),
			IssuedAt:        pgtype.Timestamptz{Time: now, Valid: true},
			Origin:          "interface",
			CreatedByUserID: userId,
		}).Return(db_queries.HusonymApiLicenseKey{}, nil)

		res, err := f.store.Offer(context.Background(), "  EE_LICENSE="+value+"\n", OriginInterface, &userId)
		require.NoError(t, err)
		require.Equal(t, Accepted, res.Outcome)
		require.NotNil(t, res.Key)
		require.True(t, res.Key.IssuedAt.Equal(now))
	})

	t.Run("a key given by no one is stored without a user", func(t *testing.T) {
		f := newFixture(t, true)
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiLicenseKey{}, pgx.ErrNoRows)
		f.querier.On("InsertLicenseKey", mock.Anything, mock.Anything, mock.MatchedBy(
			func(p db_queries.InsertLicenseKeyParams) bool {
				return !p.CreatedByUserID.Valid && p.Origin == "environment"
			})).Return(db_queries.HusonymApiLicenseKey{}, nil)

		res, err := f.store.Offer(context.Background(), value, OriginEnvironment, nil)
		require.NoError(t, err)
		require.Equal(t, Accepted, res.Outcome)
	})

	t.Run("the same value again changes nothing", func(t *testing.T) {
		f := newFixture(t, true)
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).Return(stored(value, now), nil)

		res, err := f.store.Offer(context.Background(), value+"\n", OriginFile, nil)
		require.NoError(t, err)
		require.Equal(t, Unchanged, res.Outcome)
		require.NotNil(t, res.Key)
		// No InsertLicenseKey expectation: the double fails the test if it is called.
	})

	t.Run("a key issued before the one in force is refused, and the reason gives both dates", func(t *testing.T) {
		f := newFixture(t, true)
		older := now.Add(-time.Hour)
		current := signedKey(t, f.priv, now, now.Add(time.Hour))
		offered := signedKey(t, f.priv, older, now.Add(time.Hour))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).Return(stored(current, now), nil)

		res, err := f.store.Offer(context.Background(), offered, OriginInterface, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedOlder, res.Outcome)
		require.Contains(t, res.Reason, older.Format(time.RFC3339))
		require.Contains(t, res.Reason, now.Format(time.RFC3339))
		require.NotContains(t, res.Reason, offered)
		require.NotContains(t, res.Reason, current)
		require.NotNil(t, res.Key)
		require.True(t, res.Key.IssuedAt.Equal(older))
	})

	t.Run("a key issued at the same instant as the one in force is refused", func(t *testing.T) {
		f := newFixture(t, true)
		current := signedKey(t, f.priv, now, now.Add(time.Hour))
		// A different key of the same issue date: another expiry, so another value.
		offered := signedKey(t, f.priv, now, now.Add(2*time.Hour))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).Return(stored(current, now), nil)

		res, err := f.store.Offer(context.Background(), offered, OriginInterface, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedOlder, res.Outcome)
	})

	t.Run("a later key is accepted even when it has already expired", func(t *testing.T) {
		f := newFixture(t, true)
		old := now.Add(-48 * time.Hour)
		current := signedKey(t, f.priv, old, now.Add(time.Hour))
		offered := signedKey(t, f.priv, now.Add(-time.Hour), now.Add(-time.Minute))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).Return(stored(current, old), nil)
		f.querier.On("InsertLicenseKey", mock.Anything, mock.Anything, mock.MatchedBy(
			func(p db_queries.InsertLicenseKeyParams) bool { return p.Key == offered && p.Origin == "renewal" },
		)).Return(db_queries.HusonymApiLicenseKey{}, nil)

		res, err := f.store.Offer(context.Background(), offered, OriginRenewal, nil)
		require.NoError(t, err)
		require.Equal(t, Accepted, res.Outcome)
	})

	t.Run("a database failure is an error, not a verdict on the key", func(t *testing.T) {
		f := newFixture(t, true)
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiLicenseKey{}, errors.New("connection reset"))

		res, err := f.store.Offer(context.Background(), value, OriginInterface, nil)
		require.ErrorContains(t, err, "connection reset")
		require.Nil(t, res)
	})
}

// A renewal succeeds the license in force: it is for the same customer, and the rule says so
// for a key received as a renewal alone.
func Test_Offer_ARenewalIsForTheCustomerOfTheKeyInForce(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	issued := now.Add(-48 * time.Hour)
	expiry := now.Add(time.Hour)
	ctx := context.Background()

	for name, tc := range map[string]struct {
		inForce, offered string
		want             Outcome
	}{
		"the same customer":                 {inForce: "cust-001", offered: "cust-001", want: Accepted},
		"no customer on either":             {inForce: "", offered: "", want: Accepted},
		"another customer":                  {inForce: "cust-001", offered: "cust-002", want: RefusedOtherCustomer},
		"a customer the key in force lacks": {inForce: "", offered: "cust-001", want: RefusedOtherCustomer},
		"no customer where one is in force": {inForce: "cust-001", offered: "", want: RefusedOtherCustomer},
		"a customer id spelled another way": {inForce: "cust-001", offered: "CUST-001", want: RefusedOtherCustomer},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, true)
			table := newMemoryTable(f)
			current := signedKeyFor(t, f.priv, tc.inForce, issued, expiry)
			_, err := f.store.Offer(ctx, current, OriginInterface, nil)
			require.NoError(t, err)

			offered := signedKeyFor(t, f.priv, tc.offered, now, expiry)
			res, err := f.store.Offer(ctx, offered, OriginRenewal, nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, res.Outcome)
			require.NotNil(t, res.Key)

			if tc.want == Accepted {
				require.Len(t, table.stored(), 2)
				require.Equal(t, "renewal", table.stored()[1].Origin)
				return
			}
			require.Len(t, table.stored(), 1, "the key in force was replaced")
			require.NotEmpty(t, res.Reason)
			require.NotContains(t, res.Reason, offered)
			require.NotContains(t, res.Reason, current)
			require.NotContains(t, res.Reason, "cust-00")
		})
	}

	t.Run("a key of another customer installed by hand is taken as it always was", func(t *testing.T) {
		for _, origin := range []Origin{OriginInterface, OriginEnvironment, OriginFile} {
			f := newFixture(t, true)
			table := newMemoryTable(f)
			_, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-001", issued, expiry), OriginInterface, nil)
			require.NoError(t, err)

			res, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-002", now, expiry), origin, nil)
			require.NoError(t, err)
			require.Equal(t, Accepted, res.Outcome, "origin %s", origin)
			require.Len(t, table.stored(), 2)
			require.Equal(t, string(origin), table.stored()[1].Origin)
		}
	})

	t.Run("a first key installed by hand is taken as it always was", func(t *testing.T) {
		for _, origin := range []Origin{OriginInterface, OriginEnvironment, OriginFile} {
			f := newFixture(t, true)
			table := newMemoryTable(f)

			res, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-001", now, expiry), origin, nil)
			require.NoError(t, err)
			require.Equal(t, Accepted, res.Outcome, "origin %s", origin)
			require.Len(t, table.stored(), 1)
		}
	})

	t.Run("a renewal with no key in force renews nothing and is refused", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)

		res, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-001", now, expiry), OriginRenewal, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedNothingToRenew, res.Outcome, "it is not the key of another customer")
		require.NotEmpty(t, res.Reason)
		require.NotNil(t, res.Key)
		require.Empty(t, table.stored())
	})

	t.Run("the key in force received again as a renewal changes nothing", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		current := signedKeyFor(t, f.priv, "cust-001", issued, expiry)
		_, err := f.store.Offer(ctx, current, OriginInterface, nil)
		require.NoError(t, err)

		res, err := f.store.Offer(ctx, current, OriginRenewal, nil)
		require.NoError(t, err)
		require.Equal(t, Unchanged, res.Outcome)
		require.Len(t, table.stored(), 1)
	})

	t.Run("an older key of another customer is refused for its customer", func(t *testing.T) {
		f := newFixture(t, true)
		newMemoryTable(f)
		_, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-001", now, expiry), OriginInterface, nil)
		require.NoError(t, err)

		res, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-002", issued, expiry), OriginRenewal, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedOtherCustomer, res.Outcome)
	})

	t.Run("a key in force the ring no longer reads names no customer to renew for", func(t *testing.T) {
		f := newFixture(t, true)
		_, otherPriv := newPair(t)
		// Stored under a ring the instance had before: its signature is of a key this one lacks.
		inForce := signedKeyFor(t, otherPriv, "cust-001", issued, expiry)
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).Return(stored(inForce, issued), nil)

		offered := signedKeyFor(t, f.priv, "cust-001", now, expiry)
		res, err := f.store.Offer(ctx, offered, OriginRenewal, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedNothingToRenew, res.Outcome, "nothing tells it is the key of another customer")
		require.NotEmpty(t, res.Reason)
		require.NotContains(t, res.Reason, offered)
		require.NotContains(t, res.Reason, inForce)
		require.NotContains(t, res.Reason, "cust-00")
		// No InsertLicenseKey expectation: the double fails the test if it is called.
	})

	t.Run("a key of another customer is never told as nothing to renew", func(t *testing.T) {
		f := newFixture(t, true)
		newMemoryTable(f)
		_, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-001", issued, expiry), OriginInterface, nil)
		require.NoError(t, err)

		res, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-002", now, expiry), OriginRenewal, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedOtherCustomer, res.Outcome)
	})

	t.Run("only a renewal has something to renew", func(t *testing.T) {
		for _, origin := range []Origin{OriginInterface, OriginEnvironment, OriginFile} {
			f := newFixture(t, true)
			newMemoryTable(f)

			res, err := f.store.Offer(ctx, signedKeyFor(t, f.priv, "cust-001", now, expiry), origin, nil)
			require.NoError(t, err)
			require.NotEqual(t, RefusedNothingToRenew, res.Outcome, "origin %s", origin)
		}
	})
}

func Test_Outcome_IsToldInOneWord(t *testing.T) {
	words := map[Outcome]string{
		Accepted: "accepted", Unchanged: "unchanged", RefusedOlder: "older",
		RefusedInvalid: "invalid", RefusedOtherCustomer: "other_customer",
		RefusedNothingToRenew: "nothing_to_renew",
	}
	for outcome, word := range words {
		require.Equal(t, word, outcome.String())
	}
	require.Equal(t, "none", Outcome(0).String())
}

func Test_Current(t *testing.T) {
	t.Run("is the stored value of the key in force", func(t *testing.T) {
		f := newFixture(t, false)
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).Return(stored("the-value", time.Now()), nil)

		value, err := f.store.Current(context.Background())
		require.NoError(t, err)
		require.Equal(t, "the-value", value)
	})

	t.Run("is empty when the instance has no key", func(t *testing.T) {
		f := newFixture(t, false)
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiLicenseKey{}, pgx.ErrNoRows)

		value, err := f.store.Current(context.Background())
		require.NoError(t, err)
		require.Empty(t, value)
	})

	t.Run("fails when the database does", func(t *testing.T) {
		f := newFixture(t, false)
		f.querier.On("GetCurrentLicenseKey", mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiLicenseKey{}, errors.New("connection reset"))

		_, err := f.store.Current(context.Background())
		require.ErrorContains(t, err, "connection reset")
	})
}

// A Result nobody filled in must not read as a key that was accepted.
func Test_Outcome_TheZeroValueIsNoOutcome(t *testing.T) {
	var empty Result
	for _, outcome := range []Outcome{
		Accepted, Unchanged, RefusedOlder, RefusedInvalid, RefusedOtherCustomer, RefusedNothingToRenew,
	} {
		require.NotEqual(t, outcome, empty.Outcome)
	}
}

// A key that a door holds and that is not valid is remembered with its reason, so that it can
// be shown where the instance says it has no license: a key truncated in the values of a
// deployment otherwise reads as no key at all.
func Test_DoorProblem(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("nothing was offered", func(t *testing.T) {
		require.Empty(t, newFixture(t, false).store.DoorProblem())
	})

	t.Run("an invalid key of a door is told with its door and its reason, never its value", func(t *testing.T) {
		for origin, named := range map[Origin]string{
			OriginEnvironment: "EE_LICENSE",
			OriginFile:        "EE_LICENSE_FILE",
		} {
			f := newFixture(t, false)

			res, err := f.store.Offer(ctx, "not-a-key", origin, nil)
			require.NoError(t, err)

			problem := f.store.DoorProblem()
			require.Contains(t, problem, named)
			require.Contains(t, problem, res.Reason)
			require.NotContains(t, problem, "not-a-key")
		}
	})

	t.Run("a key pasted in the interface is no door: its refusal was answered to who pasted it", func(t *testing.T) {
		f := newFixture(t, false)

		_, err := f.store.Offer(ctx, "not-a-key", OriginInterface, nil)
		require.NoError(t, err)

		require.Empty(t, f.store.DoorProblem())
	})

	t.Run("a key older than the one in force is not a problem to show", func(t *testing.T) {
		f := newFixture(t, true)
		newMemoryTable(f)
		_, err := f.store.Offer(ctx, signedKey(t, f.priv, now, now.Add(time.Hour)), OriginInterface, nil)
		require.NoError(t, err)

		res, err := f.store.Offer(ctx, signedKey(t, f.priv, now.Add(-time.Hour), now.Add(time.Hour)), OriginEnvironment, nil)
		require.NoError(t, err)
		require.Equal(t, RefusedOlder, res.Outcome)

		require.Empty(t, f.store.DoorProblem())
	})

	t.Run("a door whose key is good again has no problem any more", func(t *testing.T) {
		f := newFixture(t, true)
		newMemoryTable(f)
		_, err := f.store.Offer(ctx, "not-a-key", OriginFile, nil)
		require.NoError(t, err)
		require.NotEmpty(t, f.store.DoorProblem())

		_, err = f.store.Offer(ctx, signedKey(t, f.priv, now, now.Add(time.Hour)), OriginFile, nil)
		require.NoError(t, err)

		require.Empty(t, f.store.DoorProblem())
	})

	t.Run("a key accepted, wherever it came from, clears what the doors were refused", func(t *testing.T) {
		f := newFixture(t, true)
		newMemoryTable(f)
		_, err := f.store.Offer(ctx, "not-a-key", OriginEnvironment, nil)
		require.NoError(t, err)
		_, err = f.store.Offer(ctx, "not-a-key-either", OriginFile, nil)
		require.NoError(t, err)
		require.NotEmpty(t, f.store.DoorProblem())

		res, err := f.store.Offer(ctx, signedKey(t, f.priv, now, now.Add(time.Hour)), OriginInterface, nil)
		require.NoError(t, err)
		require.Equal(t, Accepted, res.Outcome)

		require.Empty(t, f.store.DoorProblem())
	})

	t.Run("a database that does not answer decides nothing and leaves what was known", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		_, err := f.store.Offer(ctx, "not-a-key", OriginEnvironment, nil)
		require.NoError(t, err)
		known := f.store.DoorProblem()

		table.failsNext(1)
		_, err = f.store.Offer(ctx, signedKey(t, f.priv, now, now.Add(time.Hour)), OriginEnvironment, nil)
		require.Error(t, err)

		require.Equal(t, known, f.store.DoorProblem())
	})
}

func Test_Installation(t *testing.T) {
	t.Run("is where the key of that license came from and when it was stored", func(t *testing.T) {
		f := newFixture(t, false)
		storedAt := time.Now().UTC().Truncate(time.Second)
		// Asked by the id of the license: the double answers for that id alone.
		f.querier.On("GetLicenseKeyByLicenseId", mock.Anything, mock.Anything, "lic-1").Return(db_queries.HusonymApiLicenseKey{
			Key:       "the-value",
			LicenseID: "lic-1",
			Origin:    "file",
			CreatedAt: pgtype.Timestamptz{Time: storedAt, Valid: true},
		}, nil)

		installation, err := f.store.Installation(context.Background(), "lic-1")
		require.NoError(t, err)
		require.Equal(t, &Installation{Origin: OriginFile, At: storedAt}, installation)
	})

	t.Run("is nothing when the instance holds no key of that license", func(t *testing.T) {
		f := newFixture(t, false)
		f.querier.On("GetLicenseKeyByLicenseId", mock.Anything, mock.Anything, "lic-1").
			Return(db_queries.HusonymApiLicenseKey{}, pgx.ErrNoRows)

		installation, err := f.store.Installation(context.Background(), "lic-1")
		require.NoError(t, err)
		require.Nil(t, installation)
	})

	t.Run("fails when the database does", func(t *testing.T) {
		f := newFixture(t, false)
		f.querier.On("GetLicenseKeyByLicenseId", mock.Anything, mock.Anything, "lic-1").
			Return(db_queries.HusonymApiLicenseKey{}, errors.New("connection reset"))

		_, err := f.store.Installation(context.Background(), "lic-1")
		require.ErrorContains(t, err, "connection reset")
	})
}
