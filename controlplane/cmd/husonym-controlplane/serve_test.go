package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func Test_ServePublic_WithoutADatabaseURL_Fails(t *testing.T) {
	t.Setenv(databaseURLEnv, "")
	cmd := newRootCmd()
	cmd.SetArgs([]string{"serve", "public"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.ErrorContains(t, cmd.ExecuteContext(t.Context()), databaseURLEnv)
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
	go func() {
		stopped <- servePublic(ctx, pool, listener, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour)
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

	stop()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop when its context ended")
	}
}
