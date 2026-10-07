package cpstore_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func add(t *testing.T, store *cpstore.Store, issuer *cptest.Issuer, entry *license.RegistryEntry) bool {
	t.Helper()
	added, err := store.AddLicense(t.Context(), issuer.Key(entry), entry, "registry")
	require.NoError(t, err)
	return added
}

func countLicenses(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM controlplane.licenses`).Scan(&n))
	return n
}

func Test_AddLicense_IsIdempotent(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")

	require.True(t, add(t, store, issuer, &entry))
	require.False(t, add(t, store, issuer, &entry))
	require.Equal(t, 1, countLicenses(t, pool))
}

func Test_AddLicense_KeepsTheCustomerName(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	first := issuer.Entry("lic-1", "cust-1", "Acme")
	second := issuer.Entry("lic-2", "cust-1", "Acme Renamed")

	require.True(t, add(t, store, issuer, &first))
	require.True(t, add(t, store, issuer, &second))

	var customers int
	var name string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*), min(name) FROM controlplane.customers WHERE external_id = 'cust-1'`).Scan(&customers, &name))
	require.Equal(t, 1, customers)
	require.Equal(t, "Acme", name)
}

func Test_AddLicense_StoresWhatTheKeySays(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	key := issuer.Key(&entry)

	// The registry entry disagrees with the key it carries.
	entry.Telemetry = string(license.TelemetryNone)
	entry.ExpiresAt = entry.ExpiresAt.Add(1000 * time.Hour)
	entry.Plan = "forged"
	entry.Note = "kept from the entry"

	added, err := store.AddLicense(ctx, key, &entry, "registry")
	require.NoError(t, err)
	require.True(t, added)

	var telemetryMode, plan, note string
	var expiresAt time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT telemetry, plan, note, expires_at FROM controlplane.licenses WHERE id = 'lic-1'`,
	).Scan(&telemetryMode, &plan, &note, &expiresAt))
	require.Equal(t, string(license.TelemetryOnline), telemetryMode)
	require.Empty(t, plan)
	require.True(t, key.ExpiresAt.Equal(expiresAt))
	require.Equal(t, "kept from the entry", note)
}

func Test_AddLicense_StoresLimitsAndGraceDays(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	grace := 14
	maxJobs := 10
	limits := &license.Limits{MaxJobs: &maxJobs, AllowedConnectionTypes: []string{"postgres"}}
	entry := issuer.EntryFor(&license.IssueRequest{
		Id: "lic-1", IssuedTo: "Acme", CustomerId: "cust-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		GraceDays: &grace, Limits: limits,
	})
	require.True(t, add(t, store, issuer, &entry))

	var rawLimits []byte
	var graceDays *int32
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT limits, grace_days FROM controlplane.licenses WHERE id = 'lic-1'`).Scan(&rawLimits, &graceDays))
	var got license.Limits
	require.NoError(t, json.Unmarshal(rawLimits, &got))
	require.Equal(t, limits, &got)
	require.NotNil(t, graceDays)
	require.EqualValues(t, grace, *graceDays)
}

func Test_AddLicense_NoLimitsNoGrace_AreNull(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	require.True(t, add(t, store, issuer, &entry))

	var limitsNull, graceNull bool
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT limits IS NULL, grace_days IS NULL FROM controlplane.licenses WHERE id = 'lic-1'`,
	).Scan(&limitsNull, &graceNull))
	require.True(t, limitsNull)
	require.True(t, graceNull)
}

func Test_AddLicense_NilAndEmptyFeatures_AreKeptApart(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	for id, features := range map[string][]string{
		"lic-nil":   nil,
		"lic-empty": {},
		"lic-some":  {license.FeatureWildcard},
	} {
		entry := issuer.EntryFor(&license.IssueRequest{
			Id: id, IssuedTo: "Acme", CustomerId: "cust-1",
			ExpiresAt: time.Now().UTC().Add(time.Hour), Features: features,
		})
		require.True(t, add(t, store, issuer, &entry))
	}

	read := func(id string) []string {
		var features []string
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT features FROM controlplane.licenses WHERE id = $1`, id).Scan(&features))
		return features
	}
	require.Nil(t, read("lic-nil"))
	empty := read("lic-empty")
	require.NotNil(t, empty)
	require.Empty(t, empty)
	require.Equal(t, []string{license.FeatureWildcard}, read("lic-some"))
}

func Test_AddLicense_Concurrent_AddsOnce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	key := issuer.Key(&entry)

	const callers = 2
	added := make([]bool, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			added[i], errs[i] = store.AddLicense(t.Context(), key, &entry, "registry")
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	trues := 0
	for _, a := range added {
		if a {
			trues++
		}
	}
	require.Equal(t, 1, trues)
	require.Equal(t, 1, countLicenses(t, pool))
}

func Test_LicenseByFingerprint_Unknown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	store := cpstore.New(cptest.NewDatabase(t))

	_, err := store.LicenseByFingerprint(t.Context(), telemetry.KeyFingerprint("nothing"))
	require.ErrorIs(t, err, cpstore.ErrNoLicense)
}

func Test_LicenseByFingerprint_Known(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	store := cpstore.New(cptest.NewDatabase(t))
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	require.True(t, add(t, store, issuer, &entry))

	got, err := store.LicenseByFingerprint(t.Context(), telemetry.KeyFingerprint(entry.Encoded))
	require.NoError(t, err)
	require.Equal(t, "lic-1", got.Id)
	require.Equal(t, entry.Encoded, got.Encoded)
	require.Equal(t, entry.Telemetry, got.Telemetry)
}
