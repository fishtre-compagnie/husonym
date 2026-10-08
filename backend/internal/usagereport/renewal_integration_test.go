package usagereport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// renewals is a destination that answers every request with the license it was given, or with
// nothing to give without one, and keeps what it got.
type renewals struct {
	*httptest.Server
	mu       sync.Mutex
	license  string
	requests []received
}

func newRenewals(t *testing.T) *renewals {
	t.Helper()
	r := &renewals{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, received{req.Method, req.URL.Path, req.Header.Clone(), body})
		given := r.license
		r.mu.Unlock()
		if given == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		answer, err := (&telemetry.RenewalAnswer{SchemaVersion: telemetry.RenewalSchemaVersion, License: given}).Marshal()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(answer)
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *renewals) give(license string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.license = license
}

func (r *renewals) last(t *testing.T) received {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.requests)
	return r.requests[len(r.requests)-1]
}

func (r *renewals) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// An instance on its real license store asks a destination that answers a successor: the
// successor is the key in force afterwards, stored as a renewal. Then each key the rule refuses
// leaves the key in force as it is.
func Test_AskIfDue_WhatIsReceivedGoesThroughTheRuleOfTheLicenseStore(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := husonymdb.New(pool, db_queries.New())

	// A pair made for this test: the instance trusts it and nothing else.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ring := license.Keyring{testKid: pub}
	licenses := licensestore.New(db, ring)
	usage := usagestore.New(db)
	instance, err := usage.InstanceId(ctx)
	require.NoError(t, err)

	// The license in force nears its expiry, and its successor runs a year more.
	issued := reportNow.AddDate(-1, 0, 0)
	key := func(id, customer string, issuedAt time.Time) license.Key {
		return license.Key{
			Version: "2", Id: id, IssuedTo: "Acme Co.", CustomerId: customer,
			IssuedAt: issuedAt, ExpiresAt: issuedAt.AddDate(1, 0, 10),
		}
	}
	first := mintKeyWith(t, priv, key("1111111111111111", "cust-001", issued))
	result, err := licenses.Offer(ctx, first, licensestore.OriginInterface, nil)
	require.NoError(t, err)
	require.Equal(t, licensestore.Accepted, result.Outcome)

	destination := newRenewals(t)
	logs := &syncBuffer{}
	clock := reportNow
	renewer := NewRenewer(
		NewInstanceKey(licenses, ring), usage, renewalTransportTo(t, destination.URL+"/v1/usage-reports"),
		func(ctx context.Context, value string) (*licensestore.Result, error) {
			return licenses.Offer(ctx, value, licensestore.OriginRenewal, nil)
		},
		"", func() time.Time { return clock }, slog.New(slog.NewTextHandler(logs, nil)),
	)
	// ask makes the next ask of the instance, a day after the one before, and checks that it
	// was the request of the given license, sealed with the given key.
	asks := 0
	ask := func(t *testing.T, inForce, licenseID string) {
		t.Helper()
		clock = clock.Add(RenewalPeriod)
		asks++
		require.NoError(t, renewer.AskIfDue(ctx))
		require.Equal(t, asks, destination.count())
		got := destination.last(t)
		require.Equal(t, telemetry.RenewalPath, got.path)
		request, err := telemetry.ParseRenewalRequest(got.body)
		require.NoError(t, err)
		require.Equal(t, licenseID, request.LicenseID)
		require.Equal(t, instance, request.InstanceID)
		require.WithinDuration(t, clock, request.At(), time.Second)
		require.NoError(t, telemetry.Verify(inForce, got.body, got.header.Get("Husonym-Seal")))
		require.Equal(t, telemetry.KeyFingerprint(inForce), got.header.Get("Husonym-Key-Fingerprint"))
	}
	inForce := func(t *testing.T) string {
		t.Helper()
		current, err := licenses.Current(ctx)
		require.NoError(t, err)
		return current
	}

	// Nothing to give: nothing changes.
	ask(t, first, "1111111111111111")
	require.Equal(t, first, inForce(t))
	require.Empty(t, logs.String())

	// A successor: it is the key in force, stored as a renewal.
	successor := mintKeyWith(t, priv, key("2222222222222222", "cust-001", reportNow))
	destination.give(successor)
	ask(t, first, "1111111111111111")
	require.Equal(t, successor, inForce(t))
	installation, err := licenses.Installation(ctx, "2222222222222222")
	require.NoError(t, err)
	require.NotNil(t, installation)
	require.Equal(t, licensestore.OriginRenewal, installation.Origin)
	require.False(t, installation.At.IsZero())
	require.Contains(t, logs.String(), "level=INFO")
	require.Contains(t, logs.String(), "licenseId=2222222222222222")
	require.NotContains(t, logs.String(), successor)
	require.NotContains(t, logs.String(), "level=WARN")

	// The same successor answered again, as it is until another is issued: nothing to say. The
	// request is now the one of the successor, sealed with it.
	ask(t, successor, "2222222222222222")
	require.Equal(t, successor, inForce(t))
	require.NotContains(t, logs.String(), "level=WARN")

	// What the rule refuses leaves the successor in force.
	later := reportNow.AddDate(0, 0, 30)
	for word, refused := range map[string]string{
		"invalid":        mintKeyWith(t, otherPriv, key("3333333333333333", "cust-001", later)),
		"other_customer": mintKeyWith(t, priv, key("4444444444444444", "cust-002", later)),
		"older":          mintKeyWith(t, priv, key("5555555555555555", "cust-001", issued.AddDate(0, 6, 0))),
	} {
		t.Run(word, func(t *testing.T) {
			before := logs.String()
			destination.give(refused)
			ask(t, successor, "2222222222222222")

			require.Equal(t, successor, inForce(t))
			said := logs.String()[len(before):]
			require.Contains(t, said, "level=WARN")
			require.Contains(t, said, "outcome="+word)
			require.NotContains(t, said, refused)
		})
	}
	for _, id := range []string{"3333333333333333", "4444444444444444", "5555555555555555"} {
		installation, err := licenses.Installation(ctx, id)
		require.NoError(t, err)
		require.Nil(t, installation, "the refused license %s was stored", id)
	}

	// The key of another customer is one the instance would take from a person: only a renewal
	// has to be for the customer of the key in force.
	other := mintKeyWith(t, priv, key("4444444444444444", "cust-002", later))
	result, err = licenses.Offer(ctx, other, licensestore.OriginInterface, nil)
	require.NoError(t, err)
	require.Equal(t, licensestore.Accepted, result.Outcome)
}
