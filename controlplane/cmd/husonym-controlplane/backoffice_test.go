package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/console"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

const (
	backofficeHost = "backoffice.example.com"
	operatorEmail  = "operator@example.com"
)

// consolePaths is one path of every route of the console.
var consolePaths = []string{
	"/", "/customers", "/customers/9f8b1c1e-5d0a-4a3b-8d53-0c6f1a2b3c4d", "/licenses/lic-1",
	"/licenses/lic-1/instances/123e4567-e89b-12d3-a456-426614174000",
	"/licenses/lic-1/instances/123e4567-e89b-12d3-a456-426614174000/reports/2026-10-02",
	"/pending", "/static/console.css",
}

// setBackofficeEnv gives `serve backoffice` every setting it asks for. The database is never
// reached by the tests that refuse to start.
func setBackofficeEnv(t *testing.T) {
	t.Helper()
	t.Setenv(databaseURLEnv, "postgres://nobody@127.0.0.1:1/none?connect_timeout=2")
	t.Setenv(listenAddrEnv, "127.0.0.1:0")
	t.Setenv(backofficeHostEnv, backofficeHost)
	t.Setenv(accessTeamDomainEnv, "team.example.com")
	t.Setenv(accessAudienceEnv, "aud-of-the-console")
}

func runBackofficeCmd(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newRootCmd(license.EmbeddedKeyring)
	cmd.SetArgs(append([]string{"serve", "backoffice"}, args...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return cmd.ExecuteContext(t.Context())
}

func Test_ServeBackoffice_WithoutOneOfItsVariables_FailsNamingIt(t *testing.T) {
	for _, name := range []string{databaseURLEnv, backofficeHostEnv, accessTeamDomainEnv, accessAudienceEnv} {
		t.Run(name, func(t *testing.T) {
			setBackofficeEnv(t)
			t.Setenv(name, "")

			require.ErrorContains(t, runBackofficeCmd(t), name+" is not set")
		})
	}
}

// accessgate.Host cannot fail: left unchecked, such a host would start a console that answers
// 404 to everything.
func Test_ServeBackoffice_HostThatIsNotABareName_FailsNamingTheVariableOnly(t *testing.T) {
	for _, host := range []string{
		"https://backoffice.example.com", "backoffice.example.com:8443", "backoffice.example.com/", "backoffice.example.com.",
	} {
		t.Run(host, func(t *testing.T) {
			setBackofficeEnv(t)
			t.Setenv(backofficeHostEnv, host)

			err := runBackofficeCmd(t)

			require.ErrorContains(t, err, backofficeHostEnv)
			require.NotContains(t, err.Error(), host)
		})
	}
}

// The gate checks its own settings before it fetches a key: its refusal stops the start.
func Test_ServeBackoffice_AccessSettingsTheGateRefuses_FailNamingTheVariables(t *testing.T) {
	for name, value := range map[string]string{
		accessTeamDomainEnv: "https://team.example.com/path",
		accessAudienceEnv:   "   ",
	} {
		t.Run(name, func(t *testing.T) {
			setBackofficeEnv(t)
			t.Setenv(name, value)

			err := runBackofficeCmd(t)

			require.ErrorContains(t, err, "unable to set up the Access gate")
			require.ErrorContains(t, err, name)
			require.NotContains(t, err.Error(), "team.example.com/path")
		})
	}
}

func Test_ServeBackoffice_InsecureNoAccess_IsRefusedOffLoopback(t *testing.T) {
	for _, addr := range []string{"", ":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.10:8080", "backoffice.example.com:8080", "8080"} {
		t.Run(addr, func(t *testing.T) {
			setBackofficeEnv(t)
			t.Setenv(listenAddrEnv, addr)

			err := runBackofficeCmd(t, "--"+insecureNoAccessFlag)

			require.ErrorContains(t, err, "--"+insecureNoAccessFlag+" is refused")
		})
	}
}

func Test_LoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "127.8.9.10:0", "[::1]:8080", "localhost:8080"} {
		require.True(t, loopbackAddr(addr), addr)
	}
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "10.0.0.1:8080", "localhost.example.com:8080", "localhost"} {
		require.False(t, loopbackAddr(addr), addr)
	}
}

// nothingToSee is a store whose first page is empty; no other page is asked of it.
type nothingToSee struct{ console.Reader }

func (nothingToSee) Attention(context.Context, time.Time) (*cpstore.Attention, error) {
	return &cpstore.Attention{}, nil
}

// gatedBackoffice is the handler of the backoffice behind both gates, and a token that passes.
func gatedBackoffice(t *testing.T) (handler http.Handler, token string) {
	t.Helper()
	now := time.Now()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	access := cptest.NewAccess(t)
	handler, err := newBackofficeHandler(nothingToSee{},
		&backofficeGates{host: backofficeHost, access: access.Gate(t, time.Now, logger)}, time.Now, logger)
	require.NoError(t, err)
	return handler, access.Token(t, operatorEmail, now)
}

func request(handler http.Handler, method, host, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	if token != "" {
		req.Header.Set(cptest.AccessHeader, token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func Test_Backoffice_HealthCheckPassesWithoutHostOrTokenAndNothingElseDoes(t *testing.T) {
	handler, token := gatedBackoffice(t)

	health := request(handler, http.MethodGet, "10.42.0.17:8080", "/healthz", "")
	require.Equal(t, http.StatusOK, health.Code)
	require.Empty(t, health.Body.String())
	require.Equal(t, http.StatusMethodNotAllowed, request(handler, http.MethodPost, "10.42.0.17:8080", "/healthz", "").Code)

	for _, path := range append([]string{"/healthz/", "/healthz/x", "/nothing-here"}, consolePaths...) {
		require.Equal(t, http.StatusNotFound, request(handler, http.MethodGet, "10.42.0.17:8080", path, "").Code,
			"%s under another host", path)
		require.Equal(t, http.StatusUnauthorized, request(handler, http.MethodGet, backofficeHost, path, "").Code,
			"%s without a token", path)
	}

	page := request(handler, http.MethodGet, backofficeHost, "/", token)
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "<h1>Needs attention</h1>")
	require.Contains(t, page.Body.String(), operatorEmail)
	require.NotContains(t, page.Body.String(), "gates are off")
}

// Review focus: under another name the console does not exist, whatever the request carries.
func Test_Backoffice_UnderAnotherHost_Answers404EvenWithAValidToken(t *testing.T) {
	handler, token := gatedBackoffice(t)

	for _, path := range consolePaths {
		got := request(handler, http.MethodGet, "reports.example.com", path, token)

		require.Equal(t, http.StatusNotFound, got.Code, path)
		require.Empty(t, got.Body.String(), path)
	}
}

// Review focus: the public server serves nothing of the console.
func Test_PublicServer_AnswersNotFoundToEveryConsolePath(t *testing.T) {
	handler := publicapi.NewHandler(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, path := range consolePaths {
		got := request(handler, http.MethodGet, backofficeHost, path, "")

		require.Equal(t, http.StatusNotFound, got.Code, path)
		require.Empty(t, got.Body.String(), path)
	}
}

func Test_Backoffice_WithoutGates_SaysSoOnThePage(t *testing.T) {
	handler, err := newBackofficeHandler(nothingToSee{}, nil, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	page := request(handler, http.MethodGet, "127.0.0.1:8080", "/", "")

	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "The host and Access gates are off")
	require.Contains(t, page.Body.String(), `<span class="operator">local</span>`)
}

// startBackoffice serves handler as the command does and returns its address and what stops it.
func startBackoffice(t *testing.T, handler http.Handler) (base string, stop func() error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() {
		stopped <- serveBackoffice(ctx, listener, handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	stop = sync.OnceValue(func() error {
		cancel()
		select {
		case err := <-stopped:
			return err
		case <-time.After(15 * time.Second):
			return errors.New("the server did not stop when its context ended")
		}
	})
	t.Cleanup(func() { _ = stop() })
	return "http://" + listener.Addr().String(), stop
}

// The Access gate reads a token from a header and puts no cap on it: the server does.
func Test_ServeBackoffice_RefusesHeadersOverItsCapAndStopsWhenItsContextEnds(t *testing.T) {
	base, stop := startBackoffice(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	send := func(headerBytes int) int {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+"/", http.NoBody)
		require.NoError(t, err)
		req.Header.Set(cptest.AccessHeader, strings.Repeat("a", headerBytes))
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	require.Equal(t, http.StatusOK, send(16<<10), "a token of an ordinary size passes")
	// net/http allows 4096 bytes over the cap before it refuses.
	require.Equal(t, http.StatusRequestHeaderFieldsTooLarge, send(backofficeMaxHeaderBytes+(8<<10)))
	require.Equal(t, 64<<10, newBackofficeServer(nil).MaxHeaderBytes)

	require.NoError(t, stop())
}

func Test_BackofficeServer_KeepsTheTimeoutsAndTheSilenceOfThePublicOne(t *testing.T) {
	server := newBackofficeServer(nil)

	require.Equal(t, readHeaderTimeout, server.ReadHeaderTimeout)
	require.Equal(t, readTimeout, server.ReadTimeout)
	require.Equal(t, writeTimeout, server.WriteTimeout)
	require.Equal(t, idleTimeout, server.IdleTimeout)
	require.Equal(t, io.Discard, server.ErrorLog.Writer(), "what net/http would log by itself is dropped")
}

func Test_ServeBackoffice_InsecureNoAccessOnLoopback_ServesTheConsoleOverTheDatabase(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	cptest.AddLicense(t, cpstore.New(pool), issuer, &entry)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := probe.Addr().String()
	require.NoError(t, probe.Close())
	t.Setenv(databaseURLEnv, pool.Config().ConnString())
	t.Setenv(listenAddrEnv, addr)
	// None of the three is needed without the gates.
	t.Setenv(backofficeHostEnv, "")
	t.Setenv(accessTeamDomainEnv, "")
	t.Setenv(accessAudienceEnv, "")
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	cmd := newRootCmd(license.EmbeddedKeyring)
	cmd.SetArgs([]string{"serve", "backoffice", "--" + insecureNoAccessFlag})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.ExecuteContext(ctx) }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 15*time.Second, 50*time.Millisecond, "the server listens")
	require.Equal(t, http.StatusOK, get(t, "http://"+addr+"/healthz").status)
	customers := get(t, "http://"+addr+"/customers")
	require.Equal(t, http.StatusOK, customers.status)
	require.Contains(t, customers.body, ">Acme</a>")
	require.Contains(t, customers.body, "The host and Access gates are off")
	require.NotContains(t, customers.body, entry.Encoded)
	require.Equal(t, http.StatusNotFound, get(t, "http://"+addr+"/v1/usage-reports").status, "no report intake")

	stop()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop when its context ended")
	}
}
