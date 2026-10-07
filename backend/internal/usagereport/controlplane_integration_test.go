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
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// sealedByTheProduct builds the report of an instance for a day, marshals and seals it with the
// functions of the product, as the stored report the sender posts.
func sealedByTheProduct(t *testing.T, key string, day time.Time) *usagestore.StoredReport {
	t.Helper()
	fingerprint := telemetry.KeyFingerprint(key)
	report := &telemetry.Report{
		SchemaVersion: telemetry.SchemaVersion,
		Day:           day.UTC().Format(time.DateOnly),
		GeneratedAt:   day.UTC().Format(time.RFC3339),
		Identification: telemetry.Identification{
			KeyFingerprint: fingerprint,
			LicenseID:      "0123456789abcdef",
			InstanceID:     "123e4567-e89b-12d3-a456-426614174000",
			LicenseState:   "valid",
			DaysToExpiry:   212,
		},
		Version: telemetry.Version{Husonym: "v0.3.0"},
		Sources: telemetry.Sources{Count: 3},
	}
	document, err := report.Marshal()
	require.NoError(t, err)
	seal, err := telemetry.Seal(key, document)
	require.NoError(t, err)
	return &usagestore.StoredReport{
		Day:            day.UTC().Truncate(24 * time.Hour),
		Document:       document,
		Seal:           seal,
		KeyFingerprint: fingerprint,
		PreparedAt:     day,
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

// A report built, sealed and posted by the code of the product is stored by the real handler of
// the control plane, byte for byte; the same report under a key nobody issued is kept pending.
func Test_Post_AReportOfTheProductIsAcceptedByTheControlPlane(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	known := issuer.Entry("lic-1", "cust-1", "Acme")
	unknown := issuer.Entry("lic-2", "cust-2", "Globex")
	added, err := store.AddLicense(t.Context(), issuer.Key(&known), &known, "registry")
	require.NoError(t, err)
	require.True(t, added)
	server := httptest.NewServer(publicapi.NewHandler(intake.New(store, time.Now),
		slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	transport := transportTo(t, server.URL+"/v1/usage-reports")
	today := time.Now().UTC()

	report := sealedByTheProduct(t, known.Encoded, today)
	require.NoError(t, transport.Post(t.Context(), report))

	require.Equal(t, 1, countRows(t, pool, "usage_reports"))
	require.Equal(t, 1, countRows(t, pool, "instances"))
	var stored string
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT document FROM controlplane.usage_reports`).Scan(&stored))
	require.Equal(t, string(report.Document), stored)

	require.NoError(t, transport.Post(t.Context(), sealedByTheProduct(t, unknown.Encoded, today)))
	require.Equal(t, 1, countRows(t, pool, "usage_reports"))
	require.Equal(t, 1, countRows(t, pool, "pending_reports"))
}
