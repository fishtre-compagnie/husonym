package usagereport

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// builtByTheProduct has the builder of the product make and seal the report of reportDay for the
// instance of the fixture, and gives it as the preparer keeps it for the sender.
func builtByTheProduct(t *testing.T, f *fixture) *usagestore.StoredReport {
	t.Helper()
	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.NoError(t, err)
	return &usagestore.StoredReport{
		Day:            reportDay,
		Document:       sealed.Document,
		Seal:           sealed.Seal,
		KeyFingerprint: sealed.KeyFingerprint,
		PreparedAt:     reportNow,
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

// A report built and sealed by the builder of the product, with its diagnostics, and posted by
// its transport is stored by the real handler of the control plane, byte for byte; the same
// instance under a key nobody recorded is kept pending.
func Test_Post_AReportBuiltByTheProductIsAcceptedByTheControlPlane(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	f := newFixture(t)
	require.True(t, f.facts.Diagnostics)
	key, err := license.ParseWith(f.keys.value, f.ring)
	require.NoError(t, err)

	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	added, err := store.AddLicense(ctx, key, &license.RegistryEntry{Id: key.Id, Encoded: f.keys.value, Kid: testKid}, "registry")
	require.NoError(t, err)
	require.True(t, added)
	// The control plane receives at the moment the fixture prepares its report.
	receiver := intake.New(store, func() time.Time { return reportNow })
	server := httptest.NewServer(publicapi.NewHandler(receiver, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	transport := transportTo(t, server.URL+"/v1/usage-reports")

	report := builtByTheProduct(t, f)
	require.Contains(t, tree(t, report.Document), "diagnostics")
	require.NoError(t, transport.Post(ctx, report))

	require.Equal(t, 1, countRows(t, pool, "usage_reports"))
	require.Zero(t, countRows(t, pool, "pending_reports"))
	var document, seal, licenseID, instanceID, day string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT document, seal, license_id, instance_id, day::text FROM controlplane.usage_reports`,
	).Scan(&document, &seal, &licenseID, &instanceID, &day))
	require.Equal(t, string(report.Document), document) //nolint:testifylint // the exact bytes sent, not their meaning
	require.Equal(t, report.Seal, seal)
	require.Equal(t, testLicense, licenseID)
	require.Equal(t, testInstance, instanceID)
	require.Equal(t, "2026-10-06", day)

	var installKind, version, lastDay string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT install_kind, husonym_version, last_report_day::text FROM controlplane.instances
		 WHERE license_id = $1 AND instance_id = $2`, testLicense, testInstance,
	).Scan(&installKind, &version, &lastDay))
	require.Equal(t, "helm", installKind)
	require.Equal(t, "v0.3.0", version)
	require.Equal(t, "2026-10-06", lastDay)

	// The same report again is a repeat: the transport is told it went well, nothing is added.
	require.NoError(t, transport.Post(ctx, report))
	require.Equal(t, 1, countRows(t, pool, "usage_reports"))

	// The key of a renewal the control plane has not recorded yet.
	renewed := newFixture(t)
	renewed.keys.value, renewed.ring = mintKey(t, keyExpiring(testExpiry.AddDate(1, 0, 0)))
	pending := builtByTheProduct(t, renewed)
	require.NotEqual(t, telemetry.KeyFingerprint(f.keys.value), pending.KeyFingerprint)
	require.NoError(t, transport.Post(ctx, pending))
	require.Equal(t, 1, countRows(t, pool, "usage_reports"))
	require.Equal(t, 1, countRows(t, pool, "pending_reports"))
}
