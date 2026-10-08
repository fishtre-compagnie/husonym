package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// unreachableDatabase is an address nothing listens at.
const unreachableDatabase = "postgres://nobody@127.0.0.1:1/none?connect_timeout=2"

// errRefused stands for what a connection to an address nothing listens at fails with.
var errRefused = errors.New("dial tcp 127.0.0.1:5432: connect: connection refused")

// waitFor sets the wait of the commands for the length of a test.
func waitFor(t *testing.T, wait databaseWait) {
	t.Helper()
	before := startWait
	startWait = wait
	t.Cleanup(func() { startWait = before })
}

// lockedBuffer is a buffer a command writes to while a test waits on it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// attempts is a database that fails the first attempts with one error, then answers.
type attempts struct {
	failing int
	err     error
	made    int
}

func (a *attempts) connect(context.Context) error {
	a.made++
	if a.made <= a.failing {
		return a.err
	}
	return nil
}

func Test_DatabaseWait_OfTheCommands_IsTwoSecondsApartForAMinute(t *testing.T) {
	require.Equal(t, 2*time.Second, startWait.every)
	require.Equal(t, 20*time.Second, startWait.atMost)
	require.LessOrEqual(t, startWait.attempt, startWait.atMost)
}

func Test_DatabaseWait_ADatabaseReachedAtOnce_SaysNothing(t *testing.T) {
	var logs bytes.Buffer
	database := &attempts{}
	wait := databaseWait{every: time.Hour, atMost: 2 * time.Hour, attempt: time.Second}

	require.NoError(t, wait.until(t.Context(), database.connect, slog.New(slog.NewTextHandler(&logs, nil))))

	require.Equal(t, 1, database.made)
	require.Empty(t, logs.String())
}

func Test_DatabaseWait_ADatabaseReachedAfterAFewAttempts_GoesOn_AndSaysOnceThatItWaited(t *testing.T) {
	var logs bytes.Buffer
	database := &attempts{failing: 3, err: errRefused}
	wait := databaseWait{every: 5 * time.Millisecond, atMost: time.Minute, attempt: time.Second}

	require.NoError(t, wait.until(t.Context(), database.connect, slog.New(slog.NewTextHandler(&logs, nil))))

	require.Equal(t, 4, database.made)
	logged := logs.String()
	require.Equal(t, 1, strings.Count(logged, "\n"), "one line: %s", logged)
	require.Contains(t, logged, "level=INFO")
	require.Contains(t, logged, `msg="`+waitingForDatabase+`"`+"\n", "the words and nothing after them")
	require.NotContains(t, logged, "127.0.0.1")
}

func Test_DatabaseWait_ADatabaseNeverReached_GivesUpOnceTheTimeIsUp(t *testing.T) {
	var logs bytes.Buffer
	database := &attempts{failing: 1 << 30, err: errRefused}
	wait := databaseWait{every: 20 * time.Millisecond, atMost: 200 * time.Millisecond, attempt: time.Second}
	started := time.Now()

	// No error of its own: the step that needs the database tells what is wrong.
	require.NoError(t, wait.until(t.Context(), database.connect, slog.New(slog.NewTextHandler(&logs, nil))))

	waited := time.Since(started)
	require.GreaterOrEqual(t, waited, wait.atMost-2*wait.every)
	require.Less(t, waited, wait.atMost+time.Second)
	require.Greater(t, database.made, 2)
	require.LessOrEqual(t, database.made, int(wait.atMost/wait.every)+1)
	require.Equal(t, 1, strings.Count(logs.String(), "\n"), "one line, however long it waited")
}

// What a wait does not mend is not waited for: the database answered, and refused.
func Test_DatabaseWait_ADatabaseThatAnsweredAndRefused_IsNotTriedAgain(t *testing.T) {
	for name, code := range map[string]string{
		"a wrong password":             "28P01",
		"a role that does not exist":   "28000",
		"a database that is not there": "3D000",
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			database := &attempts{failing: 1 << 30, err: fmt.Errorf("failed to connect: %w", &pgconn.PgError{Code: code})}
			wait := databaseWait{every: time.Hour, atMost: 2 * time.Hour, attempt: time.Second}

			require.NoError(t, wait.until(t.Context(), database.connect, slog.New(slog.NewTextHandler(&logs, nil))))

			require.Equal(t, 1, database.made)
			require.Empty(t, logs.String())
		})
	}
}

// PostgreSQL answers so while it starts: it is there, and not ready yet.
func Test_DatabaseWait_ADatabaseThatIsStartingUp_IsWaitedFor(t *testing.T) {
	database := &attempts{failing: 2, err: &pgconn.PgError{Code: "57P03"}}
	wait := databaseWait{every: 5 * time.Millisecond, atMost: time.Minute, attempt: time.Second}

	require.NoError(t, wait.until(t.Context(), database.connect, slog.New(slog.NewTextHandler(io.Discard, nil))))

	require.Equal(t, 3, database.made)
}

func Test_DatabaseWait_AContextThatEnds_EndsTheWaitAtOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wait := databaseWait{every: time.Hour, atMost: 2 * time.Hour, attempt: 30 * time.Minute}

	t.Run("between two attempts", func(t *testing.T) {
		database := &attempts{failing: 1 << 30, err: errRefused}
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(50*time.Millisecond, cancel)
		started := time.Now()

		err := wait.until(ctx, database.connect, logger)

		require.ErrorIs(t, err, errStartInterrupted)
		require.Less(t, time.Since(started), 5*time.Second)
		require.Equal(t, 1, database.made)
	})

	t.Run("during an attempt nothing answers", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(50*time.Millisecond, cancel)
		started := time.Now()

		err := wait.until(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}, logger)

		require.ErrorIs(t, err, errStartInterrupted)
		require.Less(t, time.Since(started), 5*time.Second)
	})

	t.Run("already ended", func(t *testing.T) {
		database := &attempts{}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		require.ErrorIs(t, wait.until(ctx, database.connect, logger), errStartInterrupted)
	})
}

// An attempt nothing answers is cut at its own bound, and the next one is made.
func Test_DatabaseWait_AnAttemptNothingAnswers_IsCutAndTriedAgain(t *testing.T) {
	made := 0
	wait := databaseWait{every: 5 * time.Millisecond, atMost: time.Minute, attempt: 20 * time.Millisecond}

	err := wait.until(t.Context(), func(ctx context.Context) error {
		made++
		if made < 3 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, err)
	require.Equal(t, 3, made)
}

func Test_Unreachable_AnAddressNothingListensAt_IsWaitedFor(t *testing.T) {
	var logs lockedBuffer
	waitFor(t, databaseWait{every: 10 * time.Millisecond, atMost: 100 * time.Millisecond, attempt: time.Second})

	require.NoError(t, awaitDatabase(t.Context(), unreachableDatabase, slog.New(slog.NewTextHandler(&logs, nil))))

	// It was tried again, which is what the line tells: the real failure of a real connection is
	// one a wait may mend.
	require.Contains(t, logs.String(), waitingForDatabase)
	require.NotContains(t, logs.String(), "nobody")
}

func Test_AwaitDatabase_AnAddressThatCannotBeRead_IsNotWaitedFor(t *testing.T) {
	var logs bytes.Buffer
	waitFor(t, databaseWait{every: time.Hour, atMost: 2 * time.Hour, attempt: time.Second})
	started := time.Now()

	require.NoError(t, awaitDatabase(t.Context(), "postgres://someone:hunter2@%zz/none", slog.New(slog.NewTextHandler(&logs, nil))))

	require.Less(t, time.Since(started), 5*time.Second)
	require.Empty(t, logs.String())
}

// run runs the command with args until it ends or ctx does, and gives what it logged and its
// error.
func run(ctx context.Context, args ...string) (logged string, err error) {
	var logs lockedBuffer
	cmd := newRootCmd(license.EmbeddedKeyring)
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(&logs)
	err = cmd.ExecuteContext(ctx)
	return logs.String(), err
}

// Both servers give up with the error they always had, once the time is up.
func Test_Serve_ADatabaseNeverReached_FailsWithItsOwnErrorOnceTheTimeIsUp(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"public":     {[]string{"serve", "public"}, "open the migrations: could not connect to the database"},
		"backoffice": {[]string{"serve", "backoffice", "--" + insecureNoAccessFlag}, "unable to reach the database"},
	} {
		t.Run(name, func(t *testing.T) {
			waitFor(t, databaseWait{every: 20 * time.Millisecond, atMost: 300 * time.Millisecond, attempt: time.Second})
			t.Setenv(databaseURLEnv, unreachableDatabase)
			t.Setenv(listenAddrEnv, "127.0.0.1:0")
			t.Setenv(metricsAddrEnv, "127.0.0.1:0")
			t.Setenv(signingKeyFileEnv, "")
			started := time.Now()

			logged, err := run(t.Context(), tc.args...)

			require.EqualError(t, err, tc.want)
			require.GreaterOrEqual(t, time.Since(started), 200*time.Millisecond, "it waited")
			require.Equal(t, 1, strings.Count(logged, waitingForDatabase), "one line says it waited: %s", logged)
			require.NotContains(t, logged, "nobody")
		})
	}
}

func Test_Serve_ToldToStopWhileItWaits_StopsAtOnce(t *testing.T) {
	for name, args := range map[string][]string{
		"public":     {"serve", "public"},
		"backoffice": {"serve", "backoffice", "--" + insecureNoAccessFlag},
	} {
		t.Run(name, func(t *testing.T) {
			waitFor(t, databaseWait{every: time.Hour, atMost: 2 * time.Hour, attempt: 5 * time.Second})
			t.Setenv(databaseURLEnv, unreachableDatabase)
			t.Setenv(listenAddrEnv, "127.0.0.1:0")
			t.Setenv(metricsAddrEnv, "127.0.0.1:0")
			t.Setenv(signingKeyFileEnv, "")
			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(200*time.Millisecond, cancel)
			started := time.Now()

			_, err := run(ctx, args...)

			require.ErrorIs(t, err, errStartInterrupted)
			require.Less(t, time.Since(started), 10*time.Second)
		})
	}
}

// An address that cannot be read is told at once, in the words it always was.
func Test_ServePublic_ADatabaseAddressThatCannotBeRead_FailsAtOnce(t *testing.T) {
	waitFor(t, databaseWait{every: time.Hour, atMost: 2 * time.Hour, attempt: time.Second})
	t.Setenv(databaseURLEnv, "postgres://someone:hunter2@%zz/none")
	started := time.Now()

	logged, err := run(t.Context(), "serve", "public")

	require.Error(t, err)
	require.NotContains(t, err.Error(), "hunter2")
	require.Less(t, time.Since(started), 10*time.Second)
	require.NotContains(t, logged, waitingForDatabase)
}

// forwardLater listens, once delay has elapsed, on a free address of this machine it returns at
// once, and forwards every connection to target: a database that becomes reachable after a while.
func forwardLater(t *testing.T, target string, delay time.Duration) (addr string) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr = probe.Addr().String()
	require.NoError(t, probe.Close())

	var mu sync.Mutex
	var listener net.Listener
	timer := time.AfterFunc(delay, func() {
		opened, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		mu.Lock()
		listener = opened
		mu.Unlock()
		for {
			inbound, err := opened.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = inbound.Close() }()
				outbound, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer func() { _ = outbound.Close() }()
				go func() { _, _ = io.Copy(outbound, inbound) }()
				_, _ = io.Copy(inbound, outbound)
			}()
		}
	})
	t.Cleanup(func() {
		timer.Stop()
		mu.Lock()
		defer mu.Unlock()
		if listener != nil {
			_ = listener.Close()
		}
	})
	return addr
}

// throughForwarder is the address of the database of pool, reached at addr instead of its own.
func throughForwarder(t *testing.T, databaseURL, addr string) string {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	require.NoError(t, err)
	parsed.Host = addr
	return parsed.String()
}

// freeAddr is an address of this machine nothing listens at.
func freeAddr(t *testing.T) string {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = probe.Close() }()
	return probe.Addr().String()
}

// What every rollout shows: the database cannot be reached when the server starts, and can a
// moment later. The server waits, says so once, and starts.
func Test_Serve_ADatabaseThatBecomesReachable_Starts(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	for name, args := range map[string][]string{
		"public":     {"serve", "public"},
		"backoffice": {"serve", "backoffice", "--" + insecureNoAccessFlag},
	} {
		t.Run(name, func(t *testing.T) {
			pool := cptest.NewDatabase(t)
			databaseURL := pool.Config().ConnString()
			parsed, err := url.Parse(databaseURL)
			require.NoError(t, err)
			waitFor(t, databaseWait{every: 100 * time.Millisecond, atMost: 30 * time.Second, attempt: 5 * time.Second})
			listenAddr := freeAddr(t)
			t.Setenv(databaseURLEnv, throughForwarder(t, databaseURL, forwardLater(t, parsed.Host, 700*time.Millisecond)))
			t.Setenv(listenAddrEnv, listenAddr)
			t.Setenv(metricsAddrEnv, freeAddr(t))
			t.Setenv(signingKeyFileEnv, "")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			type ended struct {
				err    error
				logged string
			}
			stopped := make(chan ended, 1)
			go func() {
				logged, err := run(ctx, args...)
				stopped <- ended{err, logged}
			}()

			require.Eventually(t, func() bool {
				resp, err := http.Get("http://" + listenAddr + "/healthz") //nolint:noctx // a local test server
				if err != nil {
					return false
				}
				_ = resp.Body.Close()
				return resp.StatusCode == http.StatusOK
			}, 30*time.Second, 50*time.Millisecond, "the server started")

			cancel()
			select {
			case end := <-stopped:
				require.NoError(t, end.err)
				require.Equal(t, 1, strings.Count(end.logged, waitingForDatabase), "one line says it waited: %s", end.logged)
			case <-time.After(20 * time.Second):
				t.Fatal("the server did not stop when its context ended")
			}
		})
	}
}

// A database that is reached and whose migrations fail is not waited for: the failure is told
// at once, as it always was.
func Test_ServePublic_AMigrationThatFails_IsNotTriedAgain(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	_, err := pool.Exec(t.Context(), `UPDATE public.schema_migrations SET dirty = true`)
	require.NoError(t, err)
	waitFor(t, databaseWait{every: 20 * time.Second, atMost: 2 * time.Minute, attempt: 5 * time.Second})
	t.Setenv(databaseURLEnv, pool.Config().ConnString())
	t.Setenv(listenAddrEnv, "127.0.0.1:0")
	t.Setenv(metricsAddrEnv, "127.0.0.1:0")
	started := time.Now()

	logged, err := run(t.Context(), "serve", "public")

	require.ErrorContains(t, err, "apply the migrations")
	require.ErrorContains(t, err, "Dirty database version")
	require.Less(t, time.Since(started), 15*time.Second, "one attempt, and no wait")
	require.NotContains(t, logged, waitingForDatabase)
	var version int
	var dirty bool
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT version, dirty FROM public.schema_migrations`).Scan(&version, &dirty))
	require.True(t, dirty, "nothing was applied over it")
}

// A database that is reached and refuses the password is told at once, by both servers.
func Test_Serve_ADatabaseThatRefusesThePassword_IsNotWaitedFor(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	parsed, err := url.Parse(pool.Config().ConnString())
	require.NoError(t, err)
	parsed.User = url.UserPassword(parsed.User.Username(), "not-the-password")
	for name, args := range map[string][]string{
		"public":     {"serve", "public"},
		"backoffice": {"serve", "backoffice", "--" + insecureNoAccessFlag},
	} {
		t.Run(name, func(t *testing.T) {
			waitFor(t, databaseWait{every: 20 * time.Second, atMost: 2 * time.Minute, attempt: 5 * time.Second})
			t.Setenv(databaseURLEnv, parsed.String())
			t.Setenv(listenAddrEnv, "127.0.0.1:0")
			t.Setenv(metricsAddrEnv, "127.0.0.1:0")
			t.Setenv(signingKeyFileEnv, "")
			started := time.Now()

			logged, err := run(t.Context(), args...)

			require.Error(t, err)
			require.NotContains(t, err.Error(), "not-the-password")
			require.Less(t, time.Since(started), 15*time.Second, "one attempt, and no wait")
			require.NotContains(t, logged, waitingForDatabase)
			require.NotContains(t, logged, "not-the-password")
		})
	}
}
