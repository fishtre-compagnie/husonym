package accessgate_test

import (
	"bytes"
	"context"
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
}

func newTeam(t *testing.T, keys jwk.Set) *team {
	t.Helper()
	tm := &team{keys: keys, status: http.StatusOK}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /cdn-cgi/access/certs", func(w http.ResponseWriter, _ *http.Request) {
		tm.mu.Lock()
		defer tm.mu.Unlock()
		tm.hits++
		if tm.status != http.StatusOK {
			w.WriteHeader(tm.status)
			return
		}
		_ = json.NewEncoder(w).Encode(tm.keys)
	})
	tm.server = httptest.NewTLSServer(mux)
	t.Cleanup(tm.server.Close)
	return tm
}

func (tm *team) domain() string { return strings.TrimPrefix(tm.server.URL, "https://") }

func (tm *team) serve(keys jwk.Set, status int) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.keys, tm.status = keys, status
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
	gate, err := accessgate.NewRemote(t.Context(), accessgate.Config{
		TeamDomain: tm.domain(),
		Audience:   testAudience,
		Now:        clock.Now,
		Logger:     slog.New(slog.NewTextHandler(logs, nil)),
	}, tm.server.Client(), interval)
	require.NoError(t, err)
	return &remoteRig{rig: newRig(t, gate.Wrap, nil), team: tm, clock: clock, logs: logs}
}

// claimsOf gives sound claims for a team served locally: its issuer is the address of the server.
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

func Test_Remote_FailedRefresh_KeepsTheLastKeys(t *testing.T) {
	known, stranger := newSigner(t, "key-1"), newSigner(t, "key-2")
	r := newRemoteRig(t, setOf(t, known), never)

	for _, failure := range []struct {
		name   string
		keys   jwk.Set
		status int
	}{
		{"an error of the endpoint", setOf(t, known), http.StatusInternalServerError},
		{"a set without a key", jwk.NewSet(), http.StatusOK},
	} {
		t.Run(failure.name, func(t *testing.T) {
			r.team.serve(failure.keys, failure.status)
			before := r.team.fetched()
			token := stranger.sign(t, r.claims())

			require.Equal(t, http.StatusUnauthorized, r.call(token).Code)
			require.Equal(t, before+1, r.team.fetched(), "the refresh was tried")
			require.Equal(t, http.StatusOK, r.call(known.sign(t, r.claims())).Code)

			logged := r.logs.String()
			require.Contains(t, logged, "level=ERROR")
			require.NotContains(t, logged, token)
			require.NotContains(t, logged, testPath)
			r.clock.advance(accessgate.MinRefreshGap)
		})
	}
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
	_, err := accessgate.NewRemote(ctx, accessgate.Config{
		TeamDomain: tm.domain(),
		Audience:   testAudience,
		Now:        time.Now,
		Logger:     slog.New(slog.NewTextHandler(&syncBuffer{}, nil)),
	}, tm.server.Client(), 10*time.Millisecond)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return tm.fetched() >= 3 }, 5*time.Second, 10*time.Millisecond)

	cancel()
	// One fetch may be on its way when the context ends; none starts after.
	time.Sleep(50 * time.Millisecond)
	settled := tm.fetched()
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, settled, tm.fetched())
}

func Test_New_FailsWhenTheKeysCannotBeFetched(t *testing.T) {
	key := newSigner(t, "key-1")
	logger := slog.New(slog.NewTextHandler(&syncBuffer{}, nil))

	cases := []struct {
		name   string
		keys   jwk.Set
		status int
	}{
		{"an error of the endpoint", setOf(t, key), http.StatusInternalServerError},
		{"a set without a key", jwk.NewSet(), http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tm := newTeam(t, c.keys)
			tm.serve(c.keys, c.status)
			gate, err := accessgate.NewRemote(t.Context(), accessgate.Config{
				TeamDomain: tm.domain(), Audience: testAudience, Now: time.Now, Logger: logger,
			}, tm.server.Client(), never)
			require.Error(t, err)
			require.Nil(t, gate, "no gate, so nothing can be served behind one that holds no key")
		})
	}

	t.Run("nothing answers", func(t *testing.T) {
		tm := newTeam(t, setOf(t, key))
		domain, client := tm.domain(), tm.server.Client()
		tm.server.Close()
		gate, err := accessgate.NewRemote(t.Context(), accessgate.Config{
			TeamDomain: domain, Audience: testAudience, Now: time.Now, Logger: logger,
		}, client, never)
		require.Error(t, err)
		require.Nil(t, gate)
	})
}

func Test_New_RefusesAnIncompleteConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&syncBuffer{}, nil))
	sound := accessgate.Config{
		TeamDomain: "team.example.com", Audience: testAudience, Now: time.Now, Logger: logger,
	}
	cases := []struct {
		name   string
		change func(c *accessgate.Config)
	}{
		{"no team domain", func(c *accessgate.Config) { c.TeamDomain = "" }},
		{"a team domain with a scheme", func(c *accessgate.Config) { c.TeamDomain = "https://team.example.com" }},
		{"a team domain with a path", func(c *accessgate.Config) { c.TeamDomain = "team.example.com/other" }},
		{"a team domain with a trailing slash", func(c *accessgate.Config) { c.TeamDomain = "team.example.com/" }},
		{"a team domain with a query", func(c *accessgate.Config) { c.TeamDomain = "team.example.com?x=1" }},
		{"a team domain with a user", func(c *accessgate.Config) { c.TeamDomain = "user@team.example.com" }},
		{"a team domain with a space", func(c *accessgate.Config) { c.TeamDomain = " team.example.com" }},
		{"no audience", func(c *accessgate.Config) { c.Audience = "" }},
		{"a blank audience", func(c *accessgate.Config) { c.Audience = "  " }},
		{"no clock", func(c *accessgate.Config) { c.Now = nil }},
		{"no logger", func(c *accessgate.Config) { c.Logger = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := sound
			c.change(&cfg)
			// The config is judged before anything is fetched: no network is needed to refuse it.
			gate, err := accessgate.New(t.Context(), cfg)
			require.Error(t, err)
			require.Nil(t, gate)
		})
	}
}
