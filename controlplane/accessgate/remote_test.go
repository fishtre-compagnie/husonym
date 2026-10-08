package accessgate_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
)

// team is a local stand-in for the key endpoint of an Access team.
type team struct {
	server *httptest.Server

	mu     sync.Mutex
	keys   jwk.Set
	status int
	hits   int
	// raw, when set, is served as the body in place of the keys.
	raw []byte
	// elsewhere, when set, is where every request is redirected.
	elsewhere string
}

func newTeam(t *testing.T, keys jwk.Set) *team {
	t.Helper()
	tm := &team{keys: keys, status: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cdn-cgi/access/certs", func(w http.ResponseWriter, _ *http.Request) {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		tm.hits++
		switch {
		case tm.elsewhere != "":
			w.Header().Set("Location", tm.elsewhere)
			w.WriteHeader(http.StatusFound)
		case tm.status != http.StatusOK:
			w.WriteHeader(tm.status)
		case tm.raw != nil:
			_, _ = w.Write(tm.raw)
		default:
			_ = json.NewEncoder(w).Encode(tm.keys)
		}
	})
	tm.server = httptest.NewTLSServer(mux)
	t.Cleanup(tm.server.Close)
	return tm
}

func (tm *team) domain() string { return strings.TrimPrefix(tm.server.URL, "https://") }

func (tm *team) serve(keys jwk.Set, status int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.keys, tm.status, tm.raw, tm.elsewhere = keys, status, nil, ""
}

func (tm *team) serveRaw(raw []byte) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.raw, tm.status, tm.elsewhere = raw, http.StatusOK, ""
}

func (tm *team) redirectTo(elsewhere string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.elsewhere = elsewhere
}

func (tm *team) certsURL() string { return tm.server.URL + "/cdn-cgi/access/certs" }

// padded is a sound key set made longer than size by a member no key set has.
func padded(t *testing.T, keys jwk.Set, size int) []byte {
	t.Helper()
	sound, err := json.Marshal(keys)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(sound, &document))
	document["padding"] = strings.Repeat("x", size)
	raw, err := json.Marshal(document)
	require.NoError(t, err)
	return raw
}

// withoutKeyID is the public key of a signer as a team would serve it with no key id.
func withoutKeyID(t *testing.T, s *signer) jwk.Set {
	t.Helper()
	key, err := jwk.Import(&s.private.PublicKey)
	require.NoError(t, err)
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))
	return set
}

// ellipticOnly is a set whose one key has a key id but is not an RSA key.
func ellipticOnly(t *testing.T, kid string) jwk.Set {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := jwk.Import(&private.PublicKey)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, kid))
	set := jwk.NewSet()
	require.NoError(t, set.AddKey(key))
	return set
}

// start makes a gate on the endpoint of a team, with the config taken as it is.
func (tm *team) start(ctx context.Context, now func() time.Time, logs *syncBuffer, interval time.Duration) (*accessgate.Gate, error) {
	return accessgate.NewRemote(ctx, accessgate.Config{
		TeamDomain: tm.domain(),
		Audience:   testAudience,
		Now:        now,
		Logger:     slog.New(slog.NewTextHandler(logs, nil)),
	}, tm.server.Client(), interval)
}

func (tm *team) fetched() int {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.hits
}

// movingClock is a clock a test moves by hand.
type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movingClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// syncBuffer is a log sink the background refresh and the test may touch together.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type remoteRig struct {
	*rig
	team  *team
	clock *movingClock
	logs  *syncBuffer
}

// never is an interval of background refresh no test lives long enough to see.
const never = 24 * time.Hour

func newRemoteRig(t *testing.T, keys jwk.Set, interval time.Duration) *remoteRig {
	t.Helper()
	tm := newTeam(t, keys)
	clock := &movingClock{now: testNow}
	logs := &syncBuffer{}
	gate, err := tm.start(t.Context(), clock.Now, logs, interval)
	require.NoError(t, err)
	return &remoteRig{rig: newRig(t, gate.Wrap, nil), team: tm, clock: clock, logs: logs}
}

// claims gives sound claims for a team served locally: its issuer is the address of the server.
func (r *remoteRig) claims() claims {
	c := validClaims()
	c.issuer = r.team.server.URL
	return c
}

func Test_Remote_ValidToken_FetchesNothingMore(t *testing.T) {
	key := newSigner(t, "key-1")
	r := newRemoteRig(t, setOf(t, key), never)

	for range 3 {
		require.Equal(t, http.StatusOK, r.call(key.sign(t, r.claims())).Code)
		require.Equal(t, testEmail, r.operator)
	}
	require.Equal(t, 1, r.team.fetched(), "the keys are fetched at start, not per request")
}

func Test_Remote_WrongAudience(t *testing.T) {
	key := newSigner(t, "key-1")
	r := newRemoteRig(t, setOf(t, key), never)

	c := r.claims()
	c.audience = []string{"aud-of-a-neighbour"}
	require.Equal(t, http.StatusUnauthorized, r.call(key.sign(t, c)).Code)
	require.False(t, r.reached)
}

func Test_Remote_UnknownKeyID_RefreshesAtMostOncePerGap(t *testing.T) {
	first, second, third := newSigner(t, "key-1"), newSigner(t, "key-2"), newSigner(t, "key-3")
	r := newRemoteRig(t, setOf(t, first), never)

	// The team rotates: a token signed by the new key brings the new set in.
	r.team.serve(setOf(t, first, second), http.StatusOK)
	require.Equal(t, http.StatusOK, r.call(second.sign(t, r.claims())).Code)
	require.Equal(t, 2, r.team.fetched())

	// A second unknown key id right after does not fetch again, whoever sends it.
	r.team.serve(setOf(t, first, second, third), http.StatusOK)
	for range 5 {
		require.Equal(t, http.StatusUnauthorized, r.call(third.sign(t, r.claims())).Code)
	}
	require.Equal(t, 2, r.team.fetched())
	require.Equal(t, http.StatusOK, r.call(first.sign(t, r.claims())).Code, "known keys keep working meanwhile")

	r.clock.advance(accessgate.MinRefreshGap)
	require.Equal(t, http.StatusOK, r.call(third.sign(t, r.claims())).Code)
	require.Equal(t, 3, r.team.fetched())
}

// Anybody can send tokens naming a key id nobody knows, and many at once: together they cost one
// fetch, not one each.
func Test_Remote_UnknownKeyID_ABurstCostsOneFetch(t *testing.T) {
	known, stranger := newSigner(t, "key-1"), newSigner(t, "key-2")
	tm := newTeam(t, setOf(t, known))
	gate, err := tm.start(t.Context(), func() time.Time { return testNow }, &syncBuffer{}, never)
	require.NoError(t, err)
	handler := gate.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	c := validClaims()
	c.issuer = tm.server.URL
	token := stranger.sign(t, c)

	const callers = 100
	codes := make([]int, callers)
	var ready, done sync.WaitGroup
	release := make(chan struct{})
	for i := range callers {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			request := httptest.NewRequest(http.MethodGet, testPath, http.NoBody)
			request.Header.Set("Cf-Access-Jwt-Assertion", token)
			recorder := httptest.NewRecorder()
			ready.Done()
			<-release
			handler.ServeHTTP(recorder, request)
			codes[i] = recorder.Code
		}()
	}
	ready.Wait()
	close(release)
	done.Wait()

	for _, code := range codes {
		require.Equal(t, http.StatusUnauthorized, code)
	}
	require.Equal(t, 2, tm.fetched(), "one fetch at start, one for the whole burst")
}

// spoiledAnswer is an answer of the key endpoint the gate must not take a key set from.
type spoiledAnswer struct {
	name  string
	spoil func(t *testing.T, tm *team)
}

// spoiledAnswers would each, if taken, bring in the key of the intruder beside the known one.
func spoiledAnswers(known, intruder *signer) []spoiledAnswer {
	return []spoiledAnswer{
		{"an error of the endpoint", func(t *testing.T, tm *team) {
			tm.serve(setOf(t, known, intruder), http.StatusInternalServerError)
		}},
		{"a set without a key", func(_ *testing.T, tm *team) {
			tm.serve(jwk.NewSet(), http.StatusOK)
		}},
		{"a set without an RSA key", func(t *testing.T, tm *team) {
			tm.serve(ellipticOnly(t, intruder.kid), http.StatusOK)
		}},
		{"a set whose key has no key id", func(t *testing.T, tm *team) {
			tm.serve(withoutKeyID(t, intruder), http.StatusOK)
		}},
		{"not a key set", func(_ *testing.T, tm *team) {
			tm.serveRaw([]byte("<html>sign in</html>"))
		}},
		// The other endpoint is one the client trusts as much as the first, and it serves a
		// sound set: only not following the redirect keeps it out.
		{"a redirect to another endpoint", func(t *testing.T, tm *team) {
			tm.redirectTo(newTeam(t, setOf(t, known, intruder)).certsURL())
		}},
		// Sound but for its length: only the bound keeps it out.
		{"a body beyond the bound", func(t *testing.T, tm *team) {
			tm.serveRaw(padded(t, setOf(t, known, intruder), accessgate.MaxKeySetBytes))
		}},
	}
}

func Test_Remote_FailedRefresh_KeepsTheLastKeys(t *testing.T) {
	known, intruder := newSigner(t, "key-1"), newSigner(t, "key-2")

	for _, answer := range spoiledAnswers(known, intruder) {
		t.Run(answer.name, func(t *testing.T) {
			r := newRemoteRig(t, setOf(t, known), never)
			answer.spoil(t, r.team)
			token := intruder.sign(t, r.claims())

			require.Equal(t, http.StatusUnauthorized, r.call(token).Code)
			require.Equal(t, 2, r.team.fetched(), "the refresh was tried")
			require.Equal(t, http.StatusOK, r.call(known.sign(t, r.claims())).Code)

			logged := r.logs.String()
			require.Contains(t, logged, "level=ERROR")
			require.NotContains(t, logged, token)
			require.NotContains(t, logged, testPath)

			// The endpoint recovers: the next refresh takes its set.
			r.team.serve(setOf(t, known, intruder), http.StatusOK)
			r.clock.advance(accessgate.MinRefreshGap)
			require.Equal(t, http.StatusOK, r.call(token).Code)
		})
	}
}

func Test_Remote_ABodyWithinTheBound_IsTaken(t *testing.T) {
	key := newSigner(t, "key-1")
	tm := newTeam(t, setOf(t, key))
	tm.serveRaw(padded(t, setOf(t, key), accessgate.MaxKeySetBytes-4096))

	gate, err := tm.start(t.Context(), time.Now, &syncBuffer{}, never)
	require.NoError(t, err)
	require.NotNil(t, gate)
}

func Test_Remote_BackgroundRefresh(t *testing.T) {
	retired, current := newSigner(t, "key-1"), newSigner(t, "key-2")
	r := newRemoteRig(t, setOf(t, retired), 10*time.Millisecond)
	require.Equal(t, http.StatusOK, r.call(retired.sign(t, r.claims())).Code)

	// A key the team withdraws stops being accepted without any request asking for a refresh:
	// its key id is known, so only the background refresh can drop it.
	r.team.serve(setOf(t, current), http.StatusOK)
	token := retired.sign(t, r.claims())
	require.Eventually(t, func() bool {
		return r.call(token).Code == http.StatusUnauthorized
	}, 5*time.Second, 10*time.Millisecond)

	// A failing endpoint then changes nothing.
	r.team.serve(nil, http.StatusServiceUnavailable)
	before := r.team.fetched()
	require.Eventually(t, func() bool { return r.team.fetched() >= before+2 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, http.StatusOK, r.call(current.sign(t, r.claims())).Code)
}

func Test_Remote_BackgroundRefresh_StopsWithTheContext(t *testing.T) {
	key := newSigner(t, "key-1")
	tm := newTeam(t, setOf(t, key))
	ctx, cancel := context.WithCancel(t.Context())
	gate, err := tm.start(ctx, time.Now, &syncBuffer{}, time.Millisecond)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return tm.fetched() >= 3 }, 5*time.Second, time.Millisecond,
		"the background refresh runs")

	select {
	case <-accessgate.RefreshStopped(gate):
		require.Fail(t, "the background refresh ended before its context")
	default:
	}

	cancel()
	// The refresh fetches from its own loop and nowhere else: once the loop has returned, no
	// fetch of it is on its way and none can start.
	select {
	case <-accessgate.RefreshStopped(gate):
	case <-time.After(10 * time.Second):
		require.Fail(t, "the background refresh outlived its context")
	}
}

func Test_New_FailsWhenTheKeysCannotBeFetched(t *testing.T) {
	known, intruder := newSigner(t, "key-1"), newSigner(t, "key-2")

	for _, answer := range spoiledAnswers(known, intruder) {
		t.Run(answer.name, func(t *testing.T) {
			tm := newTeam(t, setOf(t, known))
			answer.spoil(t, tm)
			gate, err := tm.start(t.Context(), time.Now, &syncBuffer{}, never)
			require.Error(t, err)
			require.Nil(t, gate, "no gate, so nothing can be served behind one that holds no key")
			require.Equal(t, 1, tm.fetched(), "the start does not insist")
		})
	}

	t.Run("nothing answers", func(t *testing.T) {
		tm := newTeam(t, setOf(t, known))
		tm.server.Close()
		gate, err := tm.start(t.Context(), time.Now, &syncBuffer{}, never)
		require.Error(t, err)
		require.Nil(t, gate)
	})
}

func soundConfig() accessgate.Config {
	return accessgate.Config{
		TeamDomain: "team.example.com",
		Audience:   testAudience,
		Now:        time.Now,
		Logger:     slog.New(slog.NewTextHandler(&syncBuffer{}, nil)),
	}
}

func Test_New_RefusesAConfigItCannotUse(t *testing.T) {
	cases := []struct {
		name   string
		change func(c *accessgate.Config)
	}{
		{"no team domain", func(c *accessgate.Config) { c.TeamDomain = "" }},
		{"a blank team domain", func(c *accessgate.Config) { c.TeamDomain = "  " }},
		{"a team domain with a scheme", func(c *accessgate.Config) { c.TeamDomain = "https://team.example.com" }},
		{"a team domain with a port", func(c *accessgate.Config) { c.TeamDomain = "team.example.com:8443" }},
		{"a team domain with a path", func(c *accessgate.Config) { c.TeamDomain = "team.example.com/other" }},
		{"a team domain with a trailing slash", func(c *accessgate.Config) { c.TeamDomain = "team.example.com/" }},
		{"a team domain with a trailing dot", func(c *accessgate.Config) { c.TeamDomain = "team.example.com." }},
		{"a team domain with a query", func(c *accessgate.Config) { c.TeamDomain = "team.example.com?x=1" }},
		{"a team domain with a user", func(c *accessgate.Config) { c.TeamDomain = "user@team.example.com" }},
		{"a team domain with a space inside", func(c *accessgate.Config) { c.TeamDomain = "team .example.com" }},
		{"no audience", func(c *accessgate.Config) { c.Audience = "" }},
		{"a blank audience", func(c *accessgate.Config) { c.Audience = "  " }},
		{"no clock", func(c *accessgate.Config) { c.Now = nil }},
		{"no logger", func(c *accessgate.Config) { c.Logger = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := soundConfig()
			c.change(&cfg)
			// The config is judged before anything is fetched: no network is needed to refuse it.
			gate, err := accessgate.New(t.Context(), cfg)
			require.Error(t, err)
			require.Nil(t, gate)
			if len(cfg.TeamDomain) > 3 {
				require.NotContains(t, err.Error(), cfg.TeamDomain, "the value is not echoed")
			}
		})
	}
}

func Test_New_NormalisesItsConfig(t *testing.T) {
	cfg := soundConfig()
	cfg.TeamDomain = "  Team.Example.COM\n"
	cfg.Audience = " " + testAudience + "\n"

	checked, err := accessgate.Checked(cfg)

	require.NoError(t, err)
	require.Equal(t, "team.example.com", checked.TeamDomain)
	require.Equal(t, testAudience, checked.Audience)
}
