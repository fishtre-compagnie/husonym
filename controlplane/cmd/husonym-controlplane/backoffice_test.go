package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	backofficeHost = "backoffice.example.com"
	operatorEmail  = "operator@example.com"
	throwawayKid   = "throwaway"
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

// readOnly is a console over store that writes nothing and issues nothing.
func readOnly(store console.Reader, logger *slog.Logger) *console.Config {
	return &console.Config{Reader: store, Now: time.Now, Logger: logger}
}

// writeSigningKey writes a fresh signing key, as `openssl genpkey -algorithm ed25519` does, in a
// throwaway directory, and returns its path and the public key that verifies what it signs.
func writeSigningKey(t *testing.T) (path string, pub ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	require.NoError(t, err)
	path = filepath.Join(t.TempDir(), "signing-key.pem")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600))
	return path, pub
}

// ringOf is a keyring that holds pub alone, under a throwaway name.
func ringOf(pub ed25519.PublicKey) func() (license.Keyring, error) {
	return func() (license.Keyring, error) { return license.Keyring{throwawayKid: pub}, nil }
}

func Test_LoadSigner_WithoutAPath_GivesNoSignerAndReadsNoKeyring(t *testing.T) {
	signer, err := loadSigner("", func() (license.Keyring, error) {
		t.Fatal("the keyring is read without a signing key")
		return nil, nil
	})

	require.NoError(t, err)
	require.Nil(t, signer)
}

func Test_LoadSigner_AKeyOfTheRing_GivesTheSignerOfThatKey(t *testing.T) {
	path, pub := writeSigningKey(t)

	signer, err := loadSigner(path, ringOf(pub))

	require.NoError(t, err)
	require.Equal(t, throwawayKid, signer.Kid())
	require.Equal(t, license.PublicKeyFingerprint(pub), signer.PublicKeyFingerprint())
}

// Review focus: started with a key that cannot sign for the product, the backoffice does not start.
// It is told before the Access gate or the database is reached.
func Test_ServeBackoffice_SigningKeyThatCannotSign_RefusesToStartNamingTheVariable(t *testing.T) {
	good, pub := writeSigningKey(t)
	_, another := writeSigningKey(t)
	notAKey := filepath.Join(t.TempDir(), "not-a-key.pem")
	require.NoError(t, os.WriteFile(notAKey, []byte("WHAT-THE-FILE-HOLDS"), 0o600))
	cases := map[string]struct {
		path    string
		keyring func() (license.Keyring, error)
		words   string
	}{
		"a key outside the ring": {good, ringOf(another), "the signing key is not one of the keys license keys are verified against"},
		"a missing file":         {filepath.Join(t.TempDir(), "absent.pem"), ringOf(pub), "the signing key file does not exist"},
		"a file that is no key":  {notAKey, ringOf(pub), "does not hold a PEM-encoded Ed25519 private key"},
		"a keyring that is away": {good, func() (license.Keyring, error) { return nil, errors.New("no keyring") }, "unable to read the keyring"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			for _, insecure := range []bool{false, true} {
				setBackofficeEnv(t)
				t.Setenv(signingKeyFileEnv, tc.path)
				args := []string{"serve", "backoffice"}
				if insecure {
					t.Setenv(listenAddrEnv, "127.0.0.1:0")
					args = append(args, "--"+insecureNoAccessFlag)
				}
				cmd := newRootCmd(tc.keyring)
				cmd.SetArgs(args)
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)

				err := cmd.ExecuteContext(t.Context())

				require.ErrorContains(t, err, signingKeyFileEnv)
				require.ErrorContains(t, err, tc.words)
				require.NotContains(t, err.Error(), tc.path, "the path is not told")
				require.NotContains(t, err.Error(), "WHAT-THE-FILE-HOLDS")
			}
		})
	}
}

// The public server signs nothing: it does not read the variable, whatever it names.
func Test_ServePublic_DoesNotReadTheSigningKey(t *testing.T) {
	t.Setenv(databaseURLEnv, "")
	t.Setenv(signingKeyFileEnv, filepath.Join(t.TempDir(), "absent.pem"))
	cmd := newRootCmd(func() (license.Keyring, error) {
		t.Fatal("the keyring is read by the public server")
		return nil, nil
	})
	cmd.SetArgs([]string{"serve", "public"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.ExecuteContext(t.Context())

	require.ErrorContains(t, err, databaseURLEnv+" is not set")
	require.NotContains(t, err.Error(), signingKeyFileEnv)
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
	handler, err := newBackofficeHandler(readOnly(nothingToSee{}, logger),
		&backofficeGates{host: backofficeHost, access: access.Gate(t, time.Now, logger)})
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

// Review focus: the handler `serve backoffice` serves refuses, behind both gates, the POST another
// origin makes the browser of the operator send, though it carries a token that passes: nothing is
// written. The same POST from a page of the console is the one that writes.
func Test_Backoffice_ACrossOriginPostWithAValidToken_IsRefusedAndWritesNothing(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	store := cpstore.New(cptest.NewDatabase(t))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	access := cptest.NewAccess(t)
	handler, err := newBackofficeHandler(
		&console.Config{Reader: store, Writer: store, Now: time.Now, Logger: logger},
		&backofficeGates{host: backofficeHost, access: access.Gate(t, time.Now, logger)})
	require.NoError(t, err)
	post := func(fetchSite, origin string) *httptest.ResponseRecorder {
		form := url.Values{"external_id": {"cust-1"}, "name": {"Acme"}}
		req := httptest.NewRequest(http.MethodPost, "/customers", strings.NewReader(form.Encode()))
		req.Host = backofficeHost
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set(cptest.AccessHeader, access.Token(t, operatorEmail, time.Now()))
		req.Header.Set("Sec-Fetch-Site", fetchSite)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	refused := post("cross-site", "https://attacker.example")

	require.Equal(t, http.StatusForbidden, refused.Code)
	require.Contains(t, refused.Body.String(), "<h1>Refused</h1>")
	customers, err := store.Customers(t.Context(), time.Now())
	require.NoError(t, err)
	require.Empty(t, customers, "no customer was recorded")
	journal, err := store.Journal(t.Context(), cpstore.JournalCap)
	require.NoError(t, err)
	require.Empty(t, journal, "nothing was journaled")

	require.Equal(t, http.StatusSeeOther, post("same-origin", "https://"+backofficeHost).Code)
	customers, err = store.Customers(t.Context(), time.Now())
	require.NoError(t, err)
	require.Len(t, customers, 1, "the same POST from a page of the console is the one that writes")
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
	handler := publicapi.NewHandler(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, path := range consolePaths {
		got := request(handler, http.MethodGet, backofficeHost, path, "")

		require.Equal(t, http.StatusNotFound, got.Code, path)
		require.Empty(t, got.Body.String(), path)
	}
}

func Test_Backoffice_WithoutGates_SaysSoOnThePage(t *testing.T) {
	handler, err := newBackofficeHandler(readOnly(nothingToSee{}, slog.New(slog.NewTextHandler(io.Discard, nil))), nil)
	require.NoError(t, err)

	page := request(handler, http.MethodGet, "127.0.0.1:8080", "/", "")

	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "The host and Access gates are off")
	require.Contains(t, page.Body.String(), `<span class="operator">local</span>`)
}

// The console sets its headers on what it answers; what is answered before it is reached carries
// them as well.
func Test_Backoffice_EveryAnswerCarriesTheSecurityHeaders(t *testing.T) {
	handler, token := gatedBackoffice(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	local, err := newBackofficeHandler(readOnly(nothingToSee{}, logger), nil)
	require.NoError(t, err)
	panicking := outermost(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("away") }), logger)

	answers := map[string]struct {
		got    *httptest.ResponseRecorder
		status int
	}{
		"the 404 of the host gate":        {request(handler, http.MethodGet, "reports.example.com", "/customers", token), http.StatusNotFound},
		"the 401 of the Access gate":      {request(handler, http.MethodGet, backofficeHost, "/customers", ""), http.StatusUnauthorized},
		"the health check":                {request(handler, http.MethodGet, "10.42.0.17:8080", "/healthz", ""), http.StatusOK},
		"the health check, wrong method":  {request(handler, http.MethodPost, "10.42.0.17:8080", "/healthz", ""), http.StatusMethodNotAllowed},
		"a page":                          {request(handler, http.MethodGet, backofficeHost, "/", token), http.StatusOK},
		"the 404 of the local mode":       {request(local, http.MethodGet, "attacker.example", "/", ""), http.StatusNotFound},
		"the 500 of a panic before pages": {request(panicking, http.MethodGet, backofficeHost, "/", ""), http.StatusInternalServerError},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, answer.status, answer.got.Code)
			header := answer.got.Header()
			require.Equal(t, []string{"no-store"}, header.Values("Cache-Control"))
			require.Equal(t,
				[]string{"default-src 'none'; style-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"},
				header.Values("Content-Security-Policy"))
			require.Equal(t, []string{"nosniff"}, header.Values("X-Content-Type-Options"))
			require.Equal(t, []string{"no-referrer"}, header.Values("Referrer-Policy"))
		})
	}
}

// The console recovers from its own panics; a gate is in front of it, and what net/http would say
// of a panic is dropped by the server.
func Test_Backoffice_APanicOutsideTheConsole_Answers500AndLogsFixedWords(t *testing.T) {
	var logs bytes.Buffer
	gate := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("SECRET") })
	handler := outermost(gate, slog.New(slog.NewTextHandler(&logs, nil)))
	req := httptest.NewRequest(http.MethodGet, "/customers/SECRET-PATH", nil)
	rec := httptest.NewRecorder()

	require.NotPanics(t, func() { handler.ServeHTTP(rec, req) })

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Empty(t, rec.Body.String())
	logged := logs.String()
	require.Equal(t, 1, strings.Count(logged, "\n"), "one line: %s", logged)
	require.Contains(t, logged, "level=ERROR")
	require.Contains(t, logged, "status=500")
	for _, secret := range []string{"SECRET", "192.0.2.1", "goroutine"} {
		require.NotContains(t, logged, secret)
	}
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
	addr, stop := runLocalBackoffice(t, pool)

	require.Equal(t, http.StatusOK, get(t, "http://"+addr+"/healthz").status)
	customers := get(t, "http://"+addr+"/customers")
	require.Equal(t, http.StatusOK, customers.status)
	require.Contains(t, customers.body, ">Acme</a>")
	require.Contains(t, customers.body, "The host and Access gates are off")
	require.NotContains(t, customers.body, entry.Encoded)
	require.Equal(t, http.StatusNotFound, get(t, "http://"+addr+"/v1/usage-reports").status, "no report intake")
	require.Equal(t, http.StatusNotFound, get(t, "http://"+addr+"/v1/license-renewals").status, "no renewal")

	require.NoError(t, stop())
}

// postForm sends a form to the local backoffice as a browser sends one of its own pages, and does
// not follow where the answer leads.
func postForm(t *testing.T, addr, path string, form url.Values) (status int, location, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+addr+path, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://"+addr)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	read, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Location"), string(read)
}

// Review focus: with a key of the ring, the command issues a license the product's verifier takes;
// without one it shows no form of issuing and refuses to issue.
func Test_ServeBackoffice_WithASigningKey_IssuesALicenseTheRingVerifies(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	keyFile, pub := writeSigningKey(t)
	addr, stop := runLocalBackofficeWith(t, pool, keyFile, ringOf(pub))

	status, customerPath, _ := postForm(t, addr, "/customers", url.Values{"external_id": {"cust-cmd"}, "name": {"Acme"}})
	require.Equal(t, http.StatusSeeOther, status)
	require.Contains(t, get(t, "http://"+addr+customerPath).body, "New 30-day license")
	draft := url.Values{
		"customer": {strings.TrimPrefix(customerPath, "/customers/")}, "customer_external_id": {"cust-cmd"}, "customer_name": {"Acme"},
		"all_features": {"1"}, "expires_at": {time.Now().UTC().AddDate(0, 0, 30).Format(time.DateOnly)},
	}
	status, _, confirmation := postForm(t, addr, "/licenses/confirm", draft)
	require.Equal(t, http.StatusOK, status)
	id := regexp.MustCompile(`name="license_id" value="([0-9a-f]{16})"`).FindStringSubmatch(confirmation)
	require.Len(t, id, 2, "the confirmation carries the id of the license")
	draft.Set("license_id", id[1])

	status, _, issued := postForm(t, addr, "/licenses", draft)

	require.Equal(t, http.StatusOK, status)
	shown := regexp.MustCompile(`(?s)<textarea[^>]*>([^<]+)</textarea>`).FindStringSubmatch(issued)
	require.Len(t, shown, 2, "the key is shown")
	ring, err := ringOf(pub)()
	require.NoError(t, err)
	key, err := license.ParseWith(html.UnescapeString(shown[1]), ring)
	require.NoError(t, err)
	require.Equal(t, id[1], key.Id)
	require.Equal(t, "cust-cmd", key.CustomerId)
	require.True(t, key.AllowsEveryFeature())
	stored, err := cpstore.New(pool).LicenseDetail(t.Context(), id[1], time.Now())
	require.NoError(t, err)
	require.Equal(t, throwawayKid, stored.Kid)
	require.Equal(t, license.PublicKeyFingerprint(pub), stored.SigningKeyFingerprint)
	require.Equal(t, "local", stored.IssuedBy)
	require.NoError(t, stop())

	// The same database, served without a key.
	addr, stop = runLocalBackoffice(t, pool)
	customer := get(t, "http://"+addr+customerPath)
	require.Contains(t, customer.body, "Issuing licenses is not configured on this server.")
	require.NotContains(t, customer.body, "New 30-day license")
	require.Equal(t, http.StatusNotFound, get(t, "http://"+addr+customerPath+"/licenses/new").status)
	draft.Del("license_id")
	status, _, _ = postForm(t, addr, "/licenses/confirm", draft)
	require.Equal(t, http.StatusNotFound, status)
	draft.Set("license_id", "0123456789abcdef")
	status, _, refused := postForm(t, addr, "/licenses", draft)
	require.Equal(t, http.StatusNotFound, status)
	require.NotContains(t, refused, "<textarea")
	require.NoError(t, stop())
}

// The public server owns the schema: the console, started on a database it finds empty, shows
// its failure page and leaves the database as it found it.
func Test_ServeBackoffice_OnAnEmptyDatabase_AppliesNoMigration(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewEmptyDatabase(t)
	schemaExists := func() bool {
		var exists bool
		require.NoError(t, pool.QueryRow(t.Context(), `SELECT to_regnamespace('controlplane') IS NOT NULL`).Scan(&exists))
		return exists
	}
	require.False(t, schemaExists(), "the database starts empty")
	addr, stop := runLocalBackoffice(t, pool)

	require.Equal(t, http.StatusOK, get(t, "http://"+addr+"/healthz").status)
	customers := get(t, "http://"+addr+"/customers")
	require.Equal(t, http.StatusInternalServerError, customers.status)
	require.Contains(t, customers.body, "<h1>Something went wrong</h1>")
	require.NotContains(t, customers.body, "controlplane.customers", "the error of the database is not on the page")

	require.NoError(t, stop())
	require.False(t, schemaExists(), "no migration was applied")
	var tables int
	require.NoError(t, pool.QueryRow(t.Context(),
		`SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog', 'information_schema')`).Scan(&tables))
	require.Zero(t, tables, "not even the table of the schema version")
}

// runLocalBackoffice runs `serve backoffice --insecure-no-access` over the database of pool, on a
// free port of 127.0.0.1, and waits until it listens. It returns the address and what stops the
// command and gives its error.
func runLocalBackoffice(t *testing.T, pool *pgxpool.Pool) (addr string, stop func() error) {
	t.Helper()
	return runLocalBackofficeWith(t, pool, "", license.EmbeddedKeyring)
}

// runLocalBackofficeWith is runLocalBackoffice with a signing key file, empty for none, and the
// keyring that key is held to.
func runLocalBackofficeWith(
	t *testing.T, pool *pgxpool.Pool, signingKeyFile string, keyring func() (license.Keyring, error),
) (addr string, stop func() error) {
	t.Helper()
	t.Setenv(signingKeyFileEnv, signingKeyFile)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr = probe.Addr().String()
	require.NoError(t, probe.Close())
	t.Setenv(databaseURLEnv, pool.Config().ConnString())
	t.Setenv(listenAddrEnv, addr)
	// None of the three is needed without the gates.
	t.Setenv(backofficeHostEnv, "")
	t.Setenv(accessTeamDomainEnv, "")
	t.Setenv(accessAudienceEnv, "")
	ctx, cancel := context.WithCancel(t.Context())
	cmd := newRootCmd(keyring)
	cmd.SetArgs([]string{"serve", "backoffice", "--" + insecureNoAccessFlag})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	stopped := make(chan error, 1)
	go func() { stopped <- cmd.ExecuteContext(ctx) }()
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

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 15*time.Second, 50*time.Millisecond, "the server listens")
	return addr, stop
}
