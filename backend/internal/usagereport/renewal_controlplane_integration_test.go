package usagereport

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	cprenewal "github.com/fishtre-compagnie/husonym/controlplane/renewal"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// An instance on its real license store asks the real public handler of the control plane, over
// its real transport. While nothing succeeds its license, nothing changes; once a successor is
// recorded, it is the key in force, stored as a renewal; and an instance two renewals behind
// receives the last license of the chain, never the one in between.
func Test_AskIfDue_TheControlPlaneAnswersTheLastLicenseOfTheChain(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()

	// The instance, on its own database.
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(context.Background()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	db := husonymdb.New(pool, db_queries.New())

	// A pair made for this test signs every license: the instance trusts it and nothing else.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	ring := license.Keyring{testKid: pub}
	licenses := licensestore.New(db, ring)
	usage := usagestore.New(db)
	instance, err := usage.InstanceId(ctx)
	require.NoError(t, err)

	// The control plane, on another database, with the same clock as the instance.
	controlPlanePool := cptest.NewDatabase(t)
	store := cpstore.New(controlPlanePool)
	clock := reportNow
	now := func() time.Time { return clock }
	controlPlaneLogs := &syncBuffer{}
	controlPlaneLogger := slog.New(slog.NewTextHandler(controlPlaneLogs, nil))
	server := httptest.NewServer(publicapi.NewHandler(
		intake.New(store, now), cprenewal.New(store, now, controlPlaneLogger, nil), nil, controlPlaneLogger))
	t.Cleanup(server.Close)

	// mint signs a license of the one customer; issue also records it in the control plane, as
	// the successor of succeeds unless that is empty.
	mint := func(id string, issuedAt time.Time) string {
		return mintKeyWith(t, priv, license.Key{
			Version: "2", Id: id, IssuedTo: "Acme Co.", CustomerId: "cust-001",
			IssuedAt: issuedAt, ExpiresAt: issuedAt.AddDate(1, 0, 10),
		})
	}
	issue := func(id string, issuedAt time.Time, succeeds string) string {
		t.Helper()
		value := mint(id, issuedAt)
		key, err := license.ParseWith(value, ring)
		require.NoError(t, err)
		added, err := store.AddLicense(ctx, key, &license.RegistryEntry{Id: id, Encoded: value, Kid: testKid}, "registry")
		require.NoError(t, err)
		require.True(t, added)
		if succeeds != "" {
			cptest.Succeed(t, controlPlanePool, id, succeeds)
		}
		return value
	}
	const firstID, secondID, thirdID, fourthID = "1111111111111111", "2222222222222222", "3333333333333333", "4444444444444444"

	// The license in force nears its expiry.
	first := issue(firstID, reportNow.AddDate(-1, 0, 0), "")
	result, err := licenses.Offer(ctx, first, licensestore.OriginInterface, nil)
	require.NoError(t, err)
	require.Equal(t, licensestore.Accepted, result.Outcome)

	logs := &syncBuffer{}
	renewer := NewRenewer(
		NewInstanceKey(licenses, ring), usage, renewalTransportTo(t, server.URL+"/v1/usage-reports"),
		func(ctx context.Context, value string) (*licensestore.Result, error) {
			return licenses.Offer(ctx, value, licensestore.OriginRenewal, nil)
		},
		"", now, slog.New(slog.NewTextHandler(logs, nil)),
	)
	// ask makes the next ask of the instance, a day after the one before.
	ask := func(t *testing.T) {
		t.Helper()
		clock = clock.Add(RenewalPeriod)
		require.NoError(t, renewer.AskIfDue(ctx))
	}
	inForce := func(t *testing.T) string {
		t.Helper()
		current, err := licenses.Current(ctx)
		require.NoError(t, err)
		return current
	}
	// asked is what the control plane recorded of the asks of the instance under a license: when
	// it last asked, and the license it was last served, empty when none was.
	asked := func(t *testing.T, licenseID string) (at time.Time, served string) {
		t.Helper()
		var servedID *string
		require.NoError(t, controlPlanePool.QueryRow(ctx, `
			SELECT last_asked_at, last_served_license_id FROM controlplane.renewal_asks
			WHERE license_id = $1 AND instance_id = $2`, licenseID, instance).Scan(&at, &servedID))
		if servedID != nil {
			served = *servedID
		}
		return at, served
	}

	// Nothing succeeds the license: nothing changes, and the control plane knows the instance asked.
	ask(t)
	require.Equal(t, first, inForce(t))
	require.Empty(t, logs.String())
	at, served := asked(t, firstID)
	require.True(t, clock.Equal(at))
	require.Empty(t, served)

	// A successor is recorded, issued later: it is the key in force, stored as a renewal.
	second := issue(secondID, reportNow, firstID)
	ask(t)
	require.Equal(t, second, inForce(t))
	installation, err := licenses.Installation(ctx, secondID)
	require.NoError(t, err)
	require.NotNil(t, installation)
	require.Equal(t, licensestore.OriginRenewal, installation.Origin)
	require.Contains(t, logs.String(), "licenseId="+secondID)
	require.NotContains(t, logs.String(), "level=WARN")
	_, served = asked(t, firstID)
	require.Equal(t, secondID, served)

	// The instance now asks under its new license, which nothing succeeds yet.
	ask(t)
	require.Equal(t, second, inForce(t))
	_, served = asked(t, secondID)
	require.Empty(t, served)

	// Two renewals are issued before the instance asks again: it receives the last one, and the
	// one in between is never installed.
	issue(thirdID, reportNow.Add(time.Hour), secondID)
	fourth := issue(fourthID, reportNow.Add(2*time.Hour), thirdID)
	ask(t)
	require.Equal(t, fourth, inForce(t))
	installation, err = licenses.Installation(ctx, fourthID)
	require.NoError(t, err)
	require.NotNil(t, installation)
	require.Equal(t, licensestore.OriginRenewal, installation.Origin)
	skipped, err := licenses.Installation(ctx, thirdID)
	require.NoError(t, err)
	require.Nil(t, skipped, "the license in between was installed")
	_, served = asked(t, secondID)
	require.Equal(t, fourthID, served)
	require.NotContains(t, logs.String(), "level=WARN")

	// Licenses whose ids are not the sixteen hexadecimal characters the license tool draws: the
	// instance does not write such an id in its request, and names it by one word, which is also
	// how the control plane reads the id of the license the request is sealed under. The first of
	// them is received as any successor is.
	const fifthID, sixthID = "acme-2027", "acme-2028"
	require.Equal(t, telemetry.LicenseId(fifthID), telemetry.LicenseId(sixthID), "one word for both")
	require.NotEqual(t, fifthID, telemetry.LicenseId(fifthID))
	fifth := issue(fifthID, reportNow.Add(3*time.Hour), fourthID)
	ask(t)
	require.Equal(t, fifth, inForce(t))
	_, served = asked(t, fourthID)
	require.Equal(t, fifthID, served)

	// Under it, the instance asks by that word: it is known as the holder of its license, and
	// recorded under the id the control plane has for it.
	ask(t)
	require.Equal(t, fifth, inForce(t))
	at, served = asked(t, fifthID)
	require.True(t, clock.Equal(at))
	require.Empty(t, served)

	// And it is given the license that succeeds it.
	sixth := issue(sixthID, reportNow.Add(4*time.Hour), fifthID)
	ask(t)
	require.Equal(t, sixth, inForce(t))
	installation, err = licenses.Installation(ctx, sixthID)
	require.NoError(t, err)
	require.NotNil(t, installation)
	require.Equal(t, licensestore.OriginRenewal, installation.Origin)
	_, served = asked(t, fifthID)
	require.Equal(t, sixthID, served)
	require.NotContains(t, logs.String(), "level=WARN")

	// The control plane logged a line per request, and no key, seal or instance in any of them.
	said := controlPlaneLogs.String()
	require.NotEmpty(t, said)
	require.NotContains(t, said, "level=ERROR")
	for _, secret := range []string{first, second, fourth, fifth, sixth, instance, fifthID, sixthID} {
		require.NotContains(t, said, secret)
	}
	require.Equal(t, 4, countRows(t, controlPlanePool, "renewal_asks"), "one row per license the instance asked under")
	require.Zero(t, countRows(t, controlPlanePool, "seal_rejections"))
}
