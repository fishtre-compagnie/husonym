package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func Test_ServePublic_WithoutADatabaseURL_Fails(t *testing.T) {
	t.Setenv(databaseURLEnv, "")
	cmd := newRootCmd(license.EmbeddedKeyring)
	cmd.SetArgs([]string{"serve", "public"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), databaseURLEnv)
}

func Test_ServePublic_UnreachableDatabase_FailsAndNeverListens(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := probe.Addr().String()
	require.NoError(t, probe.Close())
	t.Setenv(databaseURLEnv, "postgres://nobody@127.0.0.1:1/none?connect_timeout=2")
	t.Setenv(listenAddrEnv, addr)
	cmd := newRootCmd(license.EmbeddedKeyring)
	cmd.SetArgs([]string{"serve", "public"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.Error(t, cmd.ExecuteContext(t.Context()))

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err == nil {
		_ = conn.Close()
	}
	require.Error(t, err, "nothing listens on the address")
}

// Without a logger of its own, net/http writes the panic of a handler, with the address of the
// caller, to the standard logger.
func Test_PublicServer_NetHTTPLogsNothing(t *testing.T) {
	var standard bytes.Buffer
	log.SetOutput(&standard)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	server := newPublicServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("SECRET") }))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = server.Serve(listener)
	}()

	resp, err := http.Get("http://" + listener.Addr().String() + "/") //nolint:noctx // a local test server
	if err == nil {
		_ = resp.Body.Close()
	}
	require.Error(t, err, "the connection is cut")
	require.NoError(t, server.Close())
	<-served

	require.Empty(t, standard.String())
}

func Test_ServePublic_ReceivesAReportAndStopsWhenItsContextEnds(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	added, err := cpstore.New(pool).AddLicense(t.Context(), issuer.Key(&entry), &entry, "registry")
	require.NoError(t, err)
	require.True(t, added)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	stopped := make(chan error, 1)
	metricsListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		stopped <- servePublic(ctx, pool, listener, metricsListener, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour)
	}()
	base := "http://" + listener.Addr().String()

	health, err := http.Get(base + "/healthz") //nolint:noctx // a local test server
	require.NoError(t, err)
	_ = health.Body.Close()
	require.Equal(t, http.StatusOK, health.StatusCode)

	sealed := cptest.ReportFor(t, &entry, "123e4567-e89b-12d3-a456-426614174000", time.Now())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/usage-reports", bytes.NewReader(sealed.Document))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Husonym-Seal", sealed.Seal)
	req.Header.Set("Husonym-Key-Fingerprint", sealed.Fingerprint)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	metrics := get(t, "http://"+metricsListener.Addr().String()+"/metrics")
	require.Equal(t, http.StatusOK, metrics.status)
	require.Contains(t, metrics.body, `husonym_controlplane_usage_reports_total{outcome="stored"} 1`)
	require.Contains(t, metrics.body, "husonym_controlplane_silent_instances 0")
	require.Equal(t, http.StatusNotFound, get(t, "http://"+listener.Addr().String()+"/metrics").status)
	require.Equal(t, http.StatusNotFound, get(t, "http://"+metricsListener.Addr().String()+"/healthz").status)

	stop()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop when its context ended")
	}
}

type answer struct {
	status int
	body   string
}

func get(t *testing.T, url string) answer {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // a local test server
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return answer{status: resp.StatusCode, body: string(body)}
}

func Test_MetricsHandler_ServesOnlyGETMetrics(t *testing.T) {
	handler := metricsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for method, path := range map[string]string{
		http.MethodGet:    "/metrics",
		http.MethodPost:   "/metrics",
		http.MethodDelete: "/metrics/",
		http.MethodHead:   "/v1/usage-reports",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		if method == http.MethodGet {
			require.Equal(t, http.StatusOK, rec.Code)
		} else {
			require.Equal(t, http.StatusNotFound, rec.Code, "%s %s", method, path)
		}
	}
}

func Test_ServePublic_MetricsAddressTaken_FailsAndReleasesTheReportAddress(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = taken.Close() }()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	reportAddr := probe.Addr().String()
	require.NoError(t, probe.Close())
	t.Setenv(databaseURLEnv, pool.Config().ConnString())
	t.Setenv(listenAddrEnv, reportAddr)
	t.Setenv(metricsAddrEnv, taken.Addr().String())
	cmd := newRootCmd(license.EmbeddedKeyring)
	cmd.SetArgs([]string{"serve", "public"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), taken.Addr().String())

	again, err := net.Listen("tcp", reportAddr)
	require.NoError(t, err, "the report address was released")
	require.NoError(t, again.Close())
}
