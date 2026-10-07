package cpstore_test

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func Test_AddLicense_IsIdempotent(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	entry := cptest.NewIssuer(t).Entry("lic-1", "cust-1", "Acme")

	added, err := store.AddLicense(ctx, &entry, "registry")
	require.NoError(t, err)
	require.True(t, added)

	added, err = store.AddLicense(ctx, &entry, "registry")
	require.NoError(t, err)
	require.False(t, added)

	var licenses int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.licenses`).Scan(&licenses))
	require.Equal(t, 1, licenses)
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

	_, err := store.AddLicense(ctx, &first, "registry")
	require.NoError(t, err)
	added, err := store.AddLicense(ctx, &second, "registry")
	require.NoError(t, err)
	require.True(t, added)

	var customers int
	var name string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*), min(name) FROM controlplane.customers WHERE external_id = 'cust-1'`).Scan(&customers, &name))
	require.Equal(t, 1, customers)
	require.Equal(t, "Acme", name)
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
	ctx := t.Context()
	store := cpstore.New(cptest.NewDatabase(t))
	entry := cptest.NewIssuer(t).Entry("lic-1", "cust-1", "Acme")
	_, err := store.AddLicense(ctx, &entry, "registry")
	require.NoError(t, err)

	got, err := store.LicenseByFingerprint(ctx, telemetry.KeyFingerprint(entry.Encoded))
	require.NoError(t, err)
	require.Equal(t, "lic-1", got.Id)
	require.Equal(t, entry.Encoded, got.Encoded)
	require.Equal(t, entry.Telemetry, got.Telemetry)
}
