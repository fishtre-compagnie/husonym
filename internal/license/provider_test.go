package license

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"log/slog"
	"os"
	"path/filepath"
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

type providerFixture struct {
	pub   ed25519.PublicKey
	priv  ed25519.PrivateKey
	clock *testClock
	logs  *bytes.Buffer
}

func newProviderFixture(t *testing.T) *providerFixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return &providerFixture{
		pub:   pub,
		priv:  priv,
		clock: newTestClock(time.Now().UTC()),
		logs:  &bytes.Buffer{},
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
	}, f.priv)
	require.NoError(t, err)
	return issued.Encoded
}

// newLogger returns a logger writing to the fixture's buffer. The buffer is not safe for
// concurrent use, which is fine: the Provider logs under its own lock.
func (f *providerFixture) newProvider(src Source) *Provider {
	logger := slog.New(slog.NewTextHandler(f.logs, nil))
	return newProvider(src, f.pub, f.clock.Now, logger)
}

func writeLicenseFile(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o600))
	require.NoError(t, os.Rename(tmp, path))
}

func maxJobsOf(t *testing.T, p *Provider) int {
	t.Helper()
	limits := p.Limits()
	require.NotNil(t, limits)
	require.NotNil(t, limits.MaxJobs)
	return *limits.MaxJobs
}

const day = 24 * time.Hour

func Test_Provider_FollowsTheClock(t *testing.T) {
	f := newProviderFixture(t)
	expiresIn := 60 * day
	p := f.newProvider(Source{Value: f.issue(t, expiresIn, 1)})

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
	}
}

func Test_Provider_NoKey(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProvider(Source{})

	require.Equal(t, StateNone, p.State())
	require.False(t, p.IsValid())
	require.Nil(t, p.Limits())
	require.NoError(t, p.Problem())
	require.Equal(t, f.clock.Now(), p.ExpiresAt())
	require.Equal(t, f.clock.Now(), p.GracePeriodEndsAt())
}

func Test_Provider_UnreadableValue(t *testing.T) {
	f := newProviderFixture(t)
	p := f.newProvider(Source{Value: "not-a-key"})

	require.Equal(t, StateNone, p.State())
	require.False(t, p.IsValid())
	require.Error(t, p.Problem())
	require.Nil(t, p.Limits())
	require.Contains(t, f.logs.String(), "level=ERROR")
	require.NotContains(t, f.logs.String(), "not-a-key")
}

func Test_Provider_File_PicksUpANewKey(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	writeLicenseFile(t, path, f.issue(t, 90*day, 1))
	p := f.newProvider(Source{File: path})
	require.Equal(t, 1, maxJobsOf(t, p))

	writeLicenseFile(t, path, f.issue(t, 90*day, 2))
	f.clock.Advance(61 * time.Second)

	require.Equal(t, 2, maxJobsOf(t, p))
	require.NoError(t, p.Problem())
}

func Test_Provider_File_RechecksAtMostOncePerMinute(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	writeLicenseFile(t, path, f.issue(t, 90*day, 1))
	p := f.newProvider(Source{File: path})
	require.Equal(t, 1, maxJobsOf(t, p))

	writeLicenseFile(t, path, f.issue(t, 90*day, 2))
	f.clock.Advance(59 * time.Second)
	require.Equal(t, 1, maxJobsOf(t, p))

	f.clock.Advance(2 * time.Second)
	require.Equal(t, 2, maxJobsOf(t, p))
}

func Test_Provider_File_KeepsTheKeyInPlace(t *testing.T) {
	cases := map[string]func(t *testing.T, f *providerFixture, path string){
		"unreadable content": func(t *testing.T, _ *providerFixture, path string) {
			writeLicenseFile(t, path, "garbage")
		},
		"bad signature": func(t *testing.T, _ *providerFixture, path string) {
			_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
			require.NoError(t, err)
			issued, err := Issue(&IssueRequest{
				IssuedTo:   "Mallory",
				CustomerId: "cus_mallory",
				ExpiresAt:  time.Now().UTC().Add(90 * day),
			}, otherPriv)
			require.NoError(t, err)
			writeLicenseFile(t, path, issued.Encoded)
		},
		"empty file": func(t *testing.T, _ *providerFixture, path string) {
			writeLicenseFile(t, path, "")
		},
		"removed file": func(t *testing.T, _ *providerFixture, path string) {
			require.NoError(t, os.Remove(path))
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			f := newProviderFixture(t)
			path := filepath.Join(t.TempDir(), "license")
			good := f.issue(t, 90*day, 1)
			writeLicenseFile(t, path, good)
			p := f.newProvider(Source{File: path})
			require.NoError(t, p.Problem())

			damage(t, f, path)
			f.clock.Advance(61 * time.Second)

			require.Equal(t, 1, maxJobsOf(t, p))
			require.Equal(t, StateValid, p.State())
			require.Error(t, p.Problem())

			// The same faulty content is logged once, however often it is re-read.
			f.clock.Advance(61 * time.Second)
			require.Error(t, p.Problem())
			f.clock.Advance(61 * time.Second)
			require.Equal(t, 1, bytes.Count(f.logs.Bytes(), []byte("level=ERROR")))

			// Restoring a good key clears the problem.
			writeLicenseFile(t, path, f.issue(t, 90*day, 3))
			f.clock.Advance(61 * time.Second)
			require.NoError(t, p.Problem())
			require.Equal(t, 3, maxJobsOf(t, p))
		})
	}
}

func Test_Provider_File_AcceptsAnExpiredKey(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	writeLicenseFile(t, path, f.issue(t, 365*day, 1))
	p := f.newProvider(Source{File: path})
	require.Equal(t, StateValid, p.State())

	// B expires well before A, and the clock is already past its grace period.
	writeLicenseFile(t, path, f.issue(t, 30*day, 2))
	f.clock.Advance(60 * day)

	require.Equal(t, StateFrozen, p.State())
	require.False(t, p.IsValid())
	require.NoError(t, p.Problem())
}

func Test_Provider_File_TrailingNewline(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	writeLicenseFile(t, path, f.issue(t, 90*day, 1)+"\n  ")

	p := f.newProvider(Source{File: path})

	require.NoError(t, p.Problem())
	require.Equal(t, StateValid, p.State())
}

func Test_Provider_File_MissingAtStart(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	p := f.newProvider(Source{File: path})

	require.Equal(t, StateNone, p.State())
	require.Error(t, p.Problem())

	writeLicenseFile(t, path, f.issue(t, 90*day, 1))
	f.clock.Advance(61 * time.Second)

	require.Equal(t, StateValid, p.State())
	require.NoError(t, p.Problem())
}

func Test_Provider_FileWinsOverValue(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	writeLicenseFile(t, path, f.issue(t, 90*day, 2))

	p := f.newProvider(Source{Value: f.issue(t, 90*day, 1), File: path})

	require.Equal(t, 2, maxJobsOf(t, p))
	require.Contains(t, f.logs.String(), "level=WARN")
}

func Test_Provider_ConcurrentReads(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	keyA := f.issue(t, 90*day, 1)
	keyB := f.issue(t, 90*day, 2)
	writeLicenseFile(t, path, keyA)
	p := f.newProvider(Source{File: path})

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if i%2 == 0 {
				writeLicenseFile(t, path, keyB)
			} else {
				writeLicenseFile(t, path, keyA)
			}
			f.clock.Advance(61 * time.Second)
		}
	}()

	var readers sync.WaitGroup
	for range 50 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for range 200 {
				valid := p.IsValid()
				limits := p.Limits()
				if !valid || limits == nil || limits.MaxJobs == nil ||
					(*limits.MaxJobs != 1 && *limits.MaxJobs != 2) {
					t.Errorf("incoherent read: valid=%v limits=%+v", valid, limits)
					return
				}
			}
		}()
	}
	readers.Wait()
	close(stop)
	writer.Wait()
}
