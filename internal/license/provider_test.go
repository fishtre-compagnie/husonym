package license

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testClock is an adjustable clock, safe for concurrent use.
type testClock struct{ nanos atomic.Int64 }

func newTestClock(t time.Time) *testClock {
	c := &testClock{}
	c.nanos.Store(t.UnixNano())
	return c
}

func (c *testClock) Now() time.Time          { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *testClock) Advance(d time.Duration) { c.nanos.Add(int64(d)) }

// memoryLoader is a Loader whose answer the test sets, and which counts its calls. It is
// safe for concurrent use.
type memoryLoader struct {
	calls atomic.Int64

	mu    sync.Mutex
	value string
	err   error
}

func (l *memoryLoader) load(context.Context) (string, error) {
	l.calls.Add(1)
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.value, l.err
}

// gives makes the loader answer the value from now on.
func (l *memoryLoader) gives(value string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.value, l.err = value, nil
}

// fails makes the loader answer the error from now on.
func (l *memoryLoader) fails(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.value, l.err = "", err
}

type providerFixture struct {
	pub    ed25519.PublicKey
	priv   ed25519.PrivateKey
	clock  *testClock
	logs   *bytes.Buffer
	loader *memoryLoader
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return &providerFixture{
		pub:    pub,
		priv:   priv,
		clock:  newTestClock(time.Now().UTC()),
		logs:   &bytes.Buffer{},
		loader: &memoryLoader{},
	}
}

// issue mints a key expiring after the given duration from the fixture clock's start.
func (f *providerFixture) issue(t *testing.T, expiresIn time.Duration, maxJobs int) string {
	t.Helper()
	issued, err := Issue(&IssueRequest{
		IssuedTo:   "Acme",
		CustomerId: "cus_acme",
		ExpiresAt:  time.Now().UTC().Add(expiresIn),
		GraceDays:  ptr(14),
		Limits:     &Limits{MaxJobs: ptr(maxJobs)},
	}, f.priv, Keyring{LegacyKid: f.pub})
	require.NoError(t, err)
	return issued.Encoded
}

// newProvider returns a Provider fed by the fixture's loader and logging to the fixture's
// buffer. The buffer is not safe for concurrent use, which is fine: the Provider only logs
// from Refresh, and refreshes run one at a time.
func (f *providerFixture) newProvider() *Provider {
	logger := slog.New(slog.NewTextHandler(f.logs, nil))
	return newProvider(f.loader.load, Keyring{LegacyKid: f.pub}, f.clock.Now, logger)
}

// newProviderWith returns a Provider that already took the given key value.
func (f *providerFixture) newProviderWith(t *testing.T, value string) *Provider {
	t.Helper()
	f.loader.gives(value)
	p := f.newProvider()
	require.NoError(t, p.Refresh(t.Context()))
	return p
}

func (f *providerFixture) errorLogs() int {
	return bytes.Count(f.logs.Bytes(), []byte("level=ERROR"))
}

func maxJobsOf(t *testing.T, p *Provider) int {
	t.Helper()
	limits := p.Limits()
	require.NotNil(t, limits)
	require.NotNil(t, limits.MaxJobs)
	return *limits.MaxJobs
}

const day = 24 * time.Hour

func Test_Provider_StartsWithoutAKey(t *testing.T) {
	f := newProviderFixture(t)
	f.loader.gives(f.issue(t, 90*day, 1))
	p := f.newProvider()

	// The loader has a key to give, but nothing was refreshed yet.
	require.Equal(t, StateNone, p.State())
	require.False(t, p.IsValid())
	require.False(t, p.HasFeature(FeatureMcp))
	require.Nil(t, p.Limits())
	require.NoError(t, p.Problem())
	require.Equal(t, f.clock.Now(), p.ExpiresAt())
	require.Equal(t, f.clock.Now(), p.GracePeriodEndsAt())
	require.Equal(t, Description{State: StateNone}, p.Describe())
	require.Zero(t, f.loader.calls.Load())
}

func Test_Provider_RefreshTakesTheLoadedKey(t *testing.T) {
	t.Run("the first key", func(t *testing.T) {
		f := newProviderFixture(t)
		value := f.issue(t, 90*day, 1)
		p := f.newProviderWith(t, value)

		require.Equal(t, StateValid, p.State())
		require.True(t, p.IsValid())
		require.Equal(t, 1, maxJobsOf(t, p))
		require.NoError(t, p.Problem())

		// The replacement is logged with what identifies the key, never with its value.
		parsed, err := ParseWith(value, Keyring{LegacyKid: f.pub})
		require.NoError(t, err)
		require.Contains(t, f.logs.String(), "level=INFO")
		require.Contains(t, f.logs.String(), "a license key is now in force")
		require.Contains(t, f.logs.String(), parsed.Id)
		require.NotContains(t, f.logs.String(), value)
	})

	t.Run("a newer key replaces it", func(t *testing.T) {
		f := newProviderFixture(t)
		p := f.newProviderWith(t, f.issue(t, 90*day, 1))

		f.loader.gives(f.issue(t, 90*day, 2))
		require.Equal(t, 1, maxJobsOf(t, p), "nothing changes before the refresh")
		require.NoError(t, p.Refresh(t.Context()))

		require.Equal(t, 2, maxJobsOf(t, p))
		require.NoError(t, p.Problem())
	})

	t.Run("a well-signed key replaces it whatever its dates say", func(t *testing.T) {
		f := newProviderFixture(t)
		p := f.newProviderWith(t, f.issue(t, 365*day, 1))
		require.Equal(t, StateValid, p.State())

		// B expires well before A, and the clock is already past its grace period.
		f.loader.gives(f.issue(t, 30*day, 2))
		f.clock.Advance(60 * day)
		require.NoError(t, p.Refresh(t.Context()))

		require.Equal(t, StateFrozen, p.State())
		require.False(t, p.IsValid())
		require.NoError(t, p.Problem())
	})

	t.Run("surrounding blanks are not part of the key", func(t *testing.T) {
		f := newProviderFixture(t)
		p := f.newProviderWith(t, f.issue(t, 90*day, 1)+"\n  ")

		require.NoError(t, p.Problem())
		require.Equal(t, StateValid, p.State())
	})

	t.Run("the same value is not parsed again", func(t *testing.T) {
		f := newProviderFixture(t)
		p := f.newProviderWith(t, f.issue(t, 90*day, 1))
		held := p.Describe().Key
		require.NotNil(t, held)

		require.NoError(t, p.Refresh(t.Context()))
		require.NoError(t, p.Refresh(t.Context()))

		// A second parse would have built another Key.
		require.Same(t, held, p.Describe().Key)
		require.Equal(t, 1, bytes.Count(f.logs.Bytes(), []byte("level=INFO")))
	})

	t.Run("nothing to load leaves the instance without a key", func(t *testing.T) {
		f := newProviderFixture(t)
		p := f.newProvider()

		require.NoError(t, p.Refresh(t.Context()))

		require.Equal(t, StateNone, p.State())
		require.NoError(t, p.Problem())
		require.Empty(t, f.logs.String())
	})
}

func Test_Provider_ALoaderErrorKeepsTheKeyInPlace(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProviderWith(t, f.issue(t, 90*day, 1))
	unreachable := errors.New("the store is unreachable")

	f.loader.fails(unreachable)
	err := p.Refresh(t.Context())

	require.ErrorIs(t, err, unreachable)
	require.ErrorIs(t, p.Problem(), unreachable)
	// It is told apart from a key that was loaded and refused, which is not one.
	require.ErrorIs(t, p.Problem(), ErrKeyNotLoaded)
	require.Equal(t, err, p.Problem())
	require.Equal(t, 1, maxJobsOf(t, p))
	require.Equal(t, StateValid, p.State())

	// The same problem is logged once, however often the refresh meets it.
	require.Error(t, p.Refresh(t.Context()))
	require.Error(t, p.Refresh(t.Context()))
	require.Equal(t, 1, f.errorLogs())

	// Another problem is another line.
	refused := errors.New("the store refused the request")
	f.loader.fails(refused)
	require.Error(t, p.Refresh(t.Context()))
	require.Error(t, p.Refresh(t.Context()))
	require.Equal(t, 2, f.errorLogs())
	require.Equal(t, 1, maxJobsOf(t, p))

	// A good load clears the problem.
	f.loader.gives(f.issue(t, 90*day, 3))
	require.NoError(t, p.Refresh(t.Context()))
	require.NoError(t, p.Problem())
	require.Equal(t, 3, maxJobsOf(t, p))

	// Once cleared, the same problem coming back is news again.
	f.loader.fails(refused)
	require.Error(t, p.Refresh(t.Context()))
	require.Equal(t, 3, f.errorLogs())
}

func Test_Provider_ALoaderErrorWithoutAKey(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProvider()

	f.loader.fails(errors.New("the store is unreachable"))
	require.Error(t, p.Refresh(t.Context()))
	require.Equal(t, StateNone, p.State())
	require.Error(t, p.Problem())

	f.loader.gives(f.issue(t, 90*day, 1))
	require.NoError(t, p.Refresh(t.Context()))
	require.Equal(t, StateValid, p.State())
	require.NoError(t, p.Problem())
}

// A refresh cut short because the process shuts down fails for a reason that says nothing
// about the key: it is neither kept as the problem nor logged.
func Test_Provider_ALoaderErrorUnderADoneContextIsNotAProblem(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProviderWith(t, f.issue(t, 90*day, 1))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	f.loader.fails(context.Canceled)

	require.ErrorIs(t, p.Refresh(ctx), context.Canceled)
	require.NoError(t, p.Problem())
	require.Zero(t, f.errorLogs())
	require.Equal(t, StateValid, p.State())

	// The same failure under a context that still runs is a problem like any other.
	require.Error(t, p.Refresh(t.Context()))
	require.Error(t, p.Problem())
	require.Equal(t, 1, f.errorLogs())
}

// A deadline that runs out is not a shutdown: the loader did not answer in the time it was
// given, which is what an operator needs to read when a process starts without its key.
func Test_Provider_ALoaderErrorPastItsDeadlineIsAProblem(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProvider()

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	f.loader.fails(context.DeadlineExceeded)

	require.ErrorIs(t, p.Refresh(ctx), context.DeadlineExceeded)
	require.ErrorIs(t, p.Problem(), ErrKeyNotLoaded)
	require.ErrorIs(t, p.Problem(), context.DeadlineExceeded)
	require.Equal(t, 1, f.errorLogs())
}

func Test_Provider_AnInvalidKeyKeepsTheKeyInPlace(t *testing.T) {
	cases := map[string]func(t *testing.T) string{
		"unreadable content": func(*testing.T) string {
			return "not-a-key"
		},
		"bad signature": func(t *testing.T) string {
			otherPub, otherPriv, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			issued, err := Issue(&IssueRequest{
				IssuedTo:   "Mallory",
				CustomerId: "cus_mallory",
				ExpiresAt:  time.Now().UTC().Add(90 * day),
			}, otherPriv, Keyring{LegacyKid: otherPub})
			require.NoError(t, err)
			return issued.Encoded
		},
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			f := newProviderFixture(t)
			p := f.newProviderWith(t, f.issue(t, 90*day, 1))
			value := invalid(t)

			f.loader.gives(value)
			err := p.Refresh(t.Context())

			require.Error(t, err)
			require.Equal(t, err, p.Problem())
			require.Equal(t, 1, maxJobsOf(t, p))
			require.Equal(t, StateValid, p.State())

			// The same faulty value is logged once, and never reproduced.
			require.Error(t, p.Refresh(t.Context()))
			require.Error(t, p.Refresh(t.Context()))
			require.Equal(t, 1, f.errorLogs())
			require.NotContains(t, f.logs.String(), value)

			// A good key clears the problem.
			f.loader.gives(f.issue(t, 90*day, 3))
			require.NoError(t, p.Refresh(t.Context()))
			require.NoError(t, p.Problem())
			require.Equal(t, 3, maxJobsOf(t, p))
		})
	}

	t.Run("without a key in place the instance stays without one", func(t *testing.T) {
		f := newProviderFixture(t)
		f.loader.gives("not-a-key")
		p := f.newProvider()

		require.Error(t, p.Refresh(t.Context()))

		require.Equal(t, StateNone, p.State())
		require.False(t, p.IsValid())
		require.Nil(t, p.Limits())
		require.Error(t, p.Problem())
	})
}

func Test_Provider_AnEmptyLoadAfterAKeyKeepsIt(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProviderWith(t, f.issue(t, 90*day, 1))

	f.loader.gives("")
	require.NoError(t, p.Refresh(t.Context()))

	require.Equal(t, 1, maxJobsOf(t, p))
	require.Equal(t, StateValid, p.State())
	require.NoError(t, p.Problem())
	require.Zero(t, f.errorLogs())

	// An empty answer also ends a problem: the store answered.
	unreachable := errors.New("the store is unreachable")
	f.loader.fails(unreachable)
	require.Error(t, p.Refresh(t.Context()))
	require.Error(t, p.Problem())
	f.loader.gives("  \n")
	require.NoError(t, p.Refresh(t.Context()))
	require.NoError(t, p.Problem())
	require.Equal(t, 1, maxJobsOf(t, p))

	// The problem ended, so the same one coming back is logged again.
	f.loader.fails(unreachable)
	require.Error(t, p.Refresh(t.Context()))
	require.Equal(t, 2, f.errorLogs())
}

func Test_Provider_FollowsTheClockBetweenRefreshes(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProviderWith(t, f.issue(t, 60*day, 1))
	loads := f.loader.calls.Load()

	steps := []struct {
		advance time.Duration
		state   State
		valid   bool
	}{
		{0, StateValid, true},                    // T-60d
		{29 * day, StateValid, true},             // T-31d
		{2 * day, StateExpiring, true},           // T-29d
		{29*day + time.Hour, StateGrace, true},   // T+1h
		{14*day - 2*time.Hour, StateGrace, true}, // end of grace minus 1h
		{2 * time.Hour, StateFrozen, false},      // past the grace
		{30 * day, StateFrozen, false},
	}
	for _, step := range steps {
		f.clock.Advance(step.advance)
		require.Equal(t, step.state, p.State())
		require.Equal(t, step.valid, p.IsValid())
		require.Equal(t, step.state, p.Describe().State)
	}
	require.Equal(t, loads, f.loader.calls.Load())
}

func Test_Provider_HasFeature(t *testing.T) {
	f := newProviderFixture(t)
	issueWith := func(features []string) string {
		issued, err := Issue(&IssueRequest{
			IssuedTo:   "Acme",
			CustomerId: "cus_acme",
			ExpiresAt:  time.Now().UTC().Add(60 * day),
			GraceDays:  ptr(14),
			Features:   features,
		}, f.priv, Keyring{LegacyKid: f.pub})
		require.NoError(t, err)
		return issued.Encoded
	}

	t.Run("an explicit list allows what it names and nothing else", func(t *testing.T) {
		p := f.newProviderWith(t, issueWith([]string{string(FeatureMcp)}))
		require.True(t, p.HasFeature(FeatureMcp))
		require.False(t, p.HasFeature(FeatureSso))
	})

	t.Run("a key without a list allows every feature", func(t *testing.T) {
		p := f.newProviderWith(t, issueWith(nil))
		require.True(t, p.HasFeature(FeatureSso))
	})

	t.Run("no key allows nothing", func(t *testing.T) {
		p := f.newProviderWith(t, "")
		require.False(t, p.HasFeature(FeatureMcp))
	})

	t.Run("the grace period allows what the list names, a frozen key nothing", func(t *testing.T) {
		g := newProviderFixture(t)
		g.pub, g.priv = f.pub, f.priv
		p := g.newProviderWith(t, issueWith([]string{string(FeatureMcp)}))

		g.clock.Advance(61 * day)
		require.Equal(t, StateGrace, p.State())
		require.True(t, p.HasFeature(FeatureMcp))
		require.False(t, p.HasFeature(FeatureSso))

		g.clock.Advance(14 * day)
		require.Equal(t, StateFrozen, p.State())
		require.False(t, p.HasFeature(FeatureMcp))
	})
}

func Test_Provider_Describe(t *testing.T) {
	f := newProviderFixture(t)
	value := f.issue(t, 90*day, 1)
	p := f.newProviderWith(t, value)
	parsed, err := ParseWith(value, Keyring{LegacyKid: f.pub})
	require.NoError(t, err)

	described := p.Describe()
	require.Equal(t, StateValid, described.State)
	require.Equal(t, parsed, described.Key)
	require.NoError(t, described.Problem)

	// A refused load shows next to the key it did not replace.
	f.loader.gives("not-a-key")
	refused := p.Refresh(t.Context())
	require.Error(t, refused)
	f.clock.Advance(91 * day)

	described = p.Describe()
	require.Equal(t, StateGrace, described.State)
	require.Equal(t, parsed, described.Key)
	require.Equal(t, refused, described.Problem)
	// A key that was loaded and refused is not a key that could not be loaded.
	require.NotErrorIs(t, described.Problem, ErrKeyNotLoaded)
}

// InForce says of a description what IsValid says of the provider: true through grace.
func Test_Description_InForce(t *testing.T) {
	for state, want := range map[State]bool{
		StateNone:     false,
		StateValid:    true,
		StateExpiring: true,
		StateGrace:    true,
		StateFrozen:   false,
	} {
		require.Equal(t, want, Description{State: state}.InForce(), state)
	}

	f := newProviderFixture(t)
	p := f.newProviderWith(t, f.issue(t, 90*day, 1))
	for _, elapsed := range []time.Duration{0, 80 * day, 15 * day, 20 * day} {
		f.clock.Advance(elapsed)
		require.Equal(t, p.IsValid(), p.Describe().InForce(), p.State())
	}
	require.Equal(t, StateFrozen, p.State())
}

func Test_Provider_ReadsNeverCallTheLoader(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProviderWith(t, f.issue(t, 90*day, 1))
	loads := f.loader.calls.Load()
	require.EqualValues(t, 1, loads)

	for range 1000 {
		p.IsValid()
		p.HasFeature(FeatureMcp)
		p.State()
		p.Limits()
		p.ExpiresAt()
		p.GracePeriodEndsAt()
		_ = p.Problem()
		p.Describe()
		// However long the process runs, a read stays a read.
		f.clock.Advance(time.Hour)
	}

	require.Equal(t, loads, f.loader.calls.Load())
}

// Test_Provider_ReadsDoNotWaitForTheLoader holds a refresh inside its loader and reads
// meanwhile: a read that waited for the loader would never come back.
func Test_Provider_ReadsDoNotWaitForTheLoader(t *testing.T) {
	f := newProviderFixture(t)
	value := f.issue(t, 90*day, 1)
	entered := make(chan struct{})
	release := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(f.logs, nil))
	p := newProvider(func(context.Context) (string, error) {
		close(entered)
		<-release
		return value, nil
	}, Keyring{LegacyKid: f.pub}, f.clock.Now, logger)

	refreshed := make(chan error, 1)
	go func() { refreshed <- p.Refresh(context.Background()) }()
	<-entered

	read := make(chan bool, 1)
	go func() { read <- p.IsValid() }()
	select {
	case valid := <-read:
		require.False(t, valid, "the key is not there before the loader answers")
	case <-time.After(10 * time.Second):
		t.Fatal("a read waited for the loader")
	}

	close(release)
	require.NoError(t, <-refreshed)
	require.True(t, p.IsValid())
}

func Test_Provider_RefreshEveryStopsWithItsContext(t *testing.T) {
	t.Run("it refreshes until its context ends", func(t *testing.T) {
		f := newProviderFixture(t)
		f.loader.gives(f.issue(t, 90*day, 1))
		p := f.newProvider()
		ctx, cancel := context.WithCancel(t.Context())
		returned := make(chan struct{})
		go func() {
			defer close(returned)
			p.RefreshEvery(ctx, time.Millisecond)
		}()

		require.Eventually(t, func() bool { return f.loader.calls.Load() >= 3 }, 10*time.Second, time.Millisecond)
		require.Equal(t, 1, maxJobsOf(t, p))

		cancel()
		select {
		case <-returned:
		case <-time.After(10 * time.Second):
			t.Fatal("RefreshEvery outlived its context")
		}
		loads := f.loader.calls.Load()
		time.Sleep(20 * time.Millisecond)
		require.Equal(t, loads, f.loader.calls.Load(), "nothing is loaded once it returned")
	})

	t.Run("it does not refresh on entry", func(t *testing.T) {
		f := newProviderFixture(t)
		f.loader.gives(f.issue(t, 90*day, 1))
		p := f.newProvider()
		ctx, cancel := context.WithCancel(t.Context())
		returned := make(chan struct{})
		go func() {
			defer close(returned)
			p.RefreshEvery(ctx, time.Hour)
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()
		<-returned

		require.Zero(t, f.loader.calls.Load())
		require.Equal(t, StateNone, p.State())
	})
}

// Test_Provider_ConcurrentReads is meaningful under -race: each answer must come from
// one key and one instant, whatever the refreshes and the clock do meanwhile.
func Test_Provider_ConcurrentReads(t *testing.T) {
	f := newProviderFixture(t)
	keyA := f.issue(t, 90*day, 1)
	keyB := f.issue(t, 90*day, 2)
	parsedA, err := ParseWith(keyA, Keyring{LegacyKid: f.pub})
	require.NoError(t, err)
	parsedB, err := ParseWith(keyB, Keyring{LegacyKid: f.pub})
	require.NoError(t, err)
	p := f.newProviderWith(t, keyA)

	// Two refreshers swap the keys while the readers read; each one gives the loader a key
	// and refreshes, so refreshes overlap.
	stop := make(chan struct{})
	var refreshers sync.WaitGroup
	refreshErrs := make([]error, 2)
	for n := range refreshErrs {
		refreshers.Add(1)
		go func() {
			defer refreshers.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				key := keyB
				if i%2 == 1 {
					key = keyA
				}
				f.loader.gives(key)
				if refreshErrs[n] = p.Refresh(context.Background()); refreshErrs[n] != nil {
					return
				}
				f.clock.Advance(61 * time.Second)
			}
		}()
	}

	var readers sync.WaitGroup
	for range 50 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			var previous time.Time
			for range 200 {
				snap := p.snapshot()
				switch {
				case snap.key == nil:
					t.Error("the key disappeared")
					return
				case snap.problem != nil:
					t.Errorf("unexpected problem: %v", snap.problem)
					return
				case snap.key.Id == parsedA.Id && *snap.key.Limits.MaxJobs != 1,
					snap.key.Id == parsedB.Id && *snap.key.Limits.MaxJobs != 2,
					snap.key.Id != parsedA.Id && snap.key.Id != parsedB.Id:
					t.Errorf("answer mixes two keys: %+v", snap.key)
					return
				case snap.now.Before(previous):
					t.Errorf("the instant went backwards: %v after %v", snap.now, previous)
					return
				}
				previous = snap.now
			}
		}()
	}
	readers.Wait()
	close(stop)
	refreshers.Wait()
	for _, refreshErr := range refreshErrs {
		require.NoError(t, refreshErr)
	}

	// Once everything is quiet, one more refresh brings what the loader gives.
	f.loader.gives(keyB)
	require.NoError(t, p.Refresh(t.Context()))
	require.Equal(t, 2, maxJobsOf(t, p))
}
