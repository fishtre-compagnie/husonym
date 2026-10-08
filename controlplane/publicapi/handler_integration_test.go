package publicapi_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const instanceA = "123e4567-e89b-12d3-a456-426614174000"

func postReport(t *testing.T, url string, report cptest.SealedReport) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/v1/usage-reports",
		bytes.NewReader(report.Document))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Husonym-Seal", report.Seal)
	req.Header.Set("Husonym-Key-Fingerprint", report.Fingerprint)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Empty(t, body)
	return resp.StatusCode
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

func Test_Handler_OverTheRealIntake(t *testing.T) {
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
	server := httptest.NewServer(publicapi.NewHandler(intake.New(store, time.Now), nil,
		slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(server.Close)
	today := time.Now()

	stored := cptest.ReportFor(t, &known, instanceA, today)
	require.Equal(t, http.StatusNoContent, postReport(t, server.URL, stored))
	require.Equal(t, 1, count(t, pool, "usage_reports"))

	// The same document again is a repeat; a document under the seal of another is refused.
	require.Equal(t, http.StatusNoContent, postReport(t, server.URL, stored))
	other := cptest.ReportFor(t, &known, instanceA, today)
	other.Document = bytes.Replace(other.Document, []byte("v0.3.0"), []byte("v0.3.1"), 1)
	other.Seal = cptest.ReportFor(t, &known, instanceA, today).Seal
	require.Equal(t, http.StatusBadRequest, postReport(t, server.URL, other), "a seal that does not match")
	require.Equal(t, 1, count(t, pool, "usage_reports"))

	// A license nobody issued yet answers as a stored one does.
	require.Equal(t, http.StatusNoContent, postReport(t, server.URL, cptest.ReportFor(t, &unknown, instanceA, today)))
	require.Equal(t, 1, count(t, pool, "pending_reports"))
}
