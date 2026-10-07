package licensestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/jackc/pgx/v5"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// setEnvironment gives the two variables their values for the time of the test.
func setEnvironment(t *testing.T, value, file string) {
	t.Helper()
	viper.Set("EE_LICENSE", value)
	viper.Set("EE_LICENSE_FILE", file)
	t.Cleanup(func() {
		viper.Set("EE_LICENSE", "")
		viper.Set("EE_LICENSE_FILE", "")
	})
}

// memoryTable makes the database double of a fixture behave like the table: it keeps what is
// inserted and answers the latest key issued. It is safe for concurrent use.
type memoryTable struct {
	mu   sync.Mutex
	rows []db_queries.InsertLicenseKeyParams
	// reads counts the offers that reached the database.
	reads int
	// failures is how many of the next reads the database does not answer.
	failures int
}

// failsNext makes the database fail the next n offers that reach it.
func (m *memoryTable) failsNext(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = n
}

func newMemoryTable(f *fixture) *memoryTable {
	table := &memoryTable{}
	f.querier.EXPECT().GetCurrentLicenseKey(mock.Anything, mock.Anything).RunAndReturn(
		func(context.Context, db_queries.DBTX) (db_queries.HusonymApiLicenseKey, error) {
			table.mu.Lock()
			defer table.mu.Unlock()
			table.reads++
			if table.failures > 0 {
				table.failures--
				return db_queries.HusonymApiLicenseKey{}, errors.New("the database does not answer")
			}
			if len(table.rows) == 0 {
				return db_queries.HusonymApiLicenseKey{}, pgx.ErrNoRows
			}
			current := table.rows[0]
			for _, row := range table.rows[1:] {
				if row.IssuedAt.Time.After(current.IssuedAt.Time) {
					current = row
				}
			}
			return db_queries.HusonymApiLicenseKey{Key: current.Key, IssuedAt: current.IssuedAt, Origin: current.Origin}, nil
		}).Maybe()
	f.querier.EXPECT().InsertLicenseKey(mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, _ db_queries.DBTX, arg db_queries.InsertLicenseKeyParams) (db_queries.HusonymApiLicenseKey, error) {
			table.mu.Lock()
			defer table.mu.Unlock()
			table.rows = append(table.rows, arg)
			return db_queries.HusonymApiLicenseKey{}, nil
		}).Maybe()
	return table
}

func (m *memoryTable) stored() []db_queries.InsertLicenseKeyParams {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]db_queries.InsertLicenseKeyParams{}, m.rows...)
}

func (m *memoryTable) offersSeen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reads
}

func countLevel(logs *bytes.Buffer, level string) int {
	return bytes.Count(logs.Bytes(), []byte("level="+level))
}

func Test_OfferFromEnvironment(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	debugLogger := func(logs *bytes.Buffer) *slog.Logger {
		return slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}

	t.Run("the key of EE_LICENSE alone is stored as coming from the environment", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		logs := &bytes.Buffer{}
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		setEnvironment(t, value, "")

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		stored := table.stored()
		require.Len(t, stored, 1)
		require.Equal(t, value, stored[0].Key)
		require.Equal(t, "environment", stored[0].Origin)
		require.False(t, stored[0].CreatedByUserID.Valid)
		require.Equal(t, 1, countLevel(logs, "INFO"))
		require.Zero(t, countLevel(logs, "ERROR"))
		require.NotContains(t, logs.String(), value)
	})

	t.Run("a key older than the stored one is ignored, which is said once and is no error", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		current := signedKey(t, f.priv, now, now.Add(time.Hour))
		_, err := f.store.Offer(t.Context(), current, OriginInterface, nil)
		require.NoError(t, err)

		logs := &bytes.Buffer{}
		older := signedKey(t, f.priv, now.Add(-time.Hour), now.Add(time.Hour))
		setEnvironment(t, older, "")

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		require.Len(t, table.stored(), 1)
		require.Equal(t, 1, countLevel(logs, "INFO"))
		require.Contains(t, logs.String(), "ignored")
		require.Zero(t, countLevel(logs, "ERROR"))
		require.NotContains(t, logs.String(), older)
		require.NotContains(t, logs.String(), current)
	})

	t.Run("the key already in force is only said at debug level", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		_, err := f.store.Offer(t.Context(), value, OriginInterface, nil)
		require.NoError(t, err)

		logs := &bytes.Buffer{}
		setEnvironment(t, value, "")

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		require.Len(t, table.stored(), 1)
		require.Equal(t, 1, countLevel(logs, "DEBUG"))
		require.Zero(t, countLevel(logs, "INFO"))
		require.Zero(t, countLevel(logs, "ERROR"))
	})

	t.Run("the file and the variable are both offered, and the newest stays in force", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		logs := &bytes.Buffer{}
		ofTheFile := signedKey(t, f.priv, now.Add(-time.Hour), now.Add(time.Hour))
		ofTheVariable := signedKey(t, f.priv, now, now.Add(time.Hour))
		path := filepath.Join(t.TempDir(), "license.key")
		require.NoError(t, os.WriteFile(path, []byte(ofTheFile+"\n"), 0o600))
		setEnvironment(t, ofTheVariable, path)

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		stored := table.stored()
		require.Len(t, stored, 2)
		require.Equal(t, ofTheFile, stored[0].Key)
		require.Equal(t, "file", stored[0].Origin)
		require.Equal(t, ofTheVariable, stored[1].Key)
		require.Equal(t, "environment", stored[1].Origin)
	})

	t.Run("a file that cannot be read is logged, and the variable is still offered", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		logs := &bytes.Buffer{}
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		setEnvironment(t, value, filepath.Join(t.TempDir(), "absent.key"))

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		require.Equal(t, 1, countLevel(logs, "ERROR"))
		require.Len(t, table.stored(), 1)
		require.Equal(t, "environment", table.stored()[0].Origin)
	})

	t.Run("a value that is not a key is an error log giving the reason, never the value", func(t *testing.T) {
		f := newFixture(t, false)
		logs := &bytes.Buffer{}
		setEnvironment(t, "not-a-key", "")

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		require.Equal(t, 1, countLevel(logs, "ERROR"))
		require.Contains(t, logs.String(), "not valid base64")
		require.NotContains(t, logs.String(), "not-a-key")
	})

	t.Run("nothing set offers nothing", func(t *testing.T) {
		f := newFixture(t, false)
		logs := &bytes.Buffer{}
		setEnvironment(t, "", "")

		OfferFromEnvironment(t.Context(), f.store, debugLogger(logs))

		require.Empty(t, logs.String())
		// The doubles fail the test on any call: the database was not touched.
	})
}

// EE_LICENSE is read once: when the database did not answer for it at the start, nothing else
// would offer it again, and an instance that had a license would run without one.
func Test_OfferFromEnvironment_TellsWhenTheVariableWasNotAnswered(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("a database that does not answer leaves the variable to be offered again", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		table.failsNext(1)
		setEnvironment(t, signedKey(t, f.priv, now, now.Add(time.Hour)), "")

		require.False(t, OfferFromEnvironment(t.Context(), f.store, quiet))
		require.Empty(t, table.stored())
	})

	t.Run("an accepted key is an answer", func(t *testing.T) {
		f := newFixture(t, true)
		newMemoryTable(f)
		setEnvironment(t, signedKey(t, f.priv, now, now.Add(time.Hour)), "")

		require.True(t, OfferFromEnvironment(t.Context(), f.store, quiet))
	})

	t.Run("a refused key is an answer", func(t *testing.T) {
		f := newFixture(t, false)
		setEnvironment(t, "not-a-key", "")

		require.True(t, OfferFromEnvironment(t.Context(), f.store, quiet))
	})

	t.Run("no variable leaves nothing to offer", func(t *testing.T) {
		f := newFixture(t, false)
		setEnvironment(t, "", "")

		require.True(t, OfferFromEnvironment(t.Context(), f.store, quiet))
	})
}

func Test_OfferEnvironmentUntilAnswered(t *testing.T) {
	const every = time.Millisecond
	now := time.Now().UTC().Truncate(time.Second)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("the key is stored once the database answers, and offered no more", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		table.failsNext(2)
		value := signedKey(t, f.priv, now, now.Add(time.Hour))
		setEnvironment(t, value, "")

		accepted := 0
		OfferEnvironmentUntilAnswered(t.Context(), f.store, every, func() { accepted++ }, quiet)

		stored := table.stored()
		require.Len(t, stored, 1)
		require.Equal(t, value, stored[0].Key)
		require.Equal(t, "environment", stored[0].Origin)
		require.Equal(t, 1, accepted, "the provider is refreshed at once")
		// Two attempts failed, the third was answered: there is no fourth.
		require.Equal(t, 3, table.offersSeen())
	})

	t.Run("an answer that is not an acceptance ends it too", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		current := signedKey(t, f.priv, now, now.Add(time.Hour))
		_, err := f.store.Offer(t.Context(), current, OriginInterface, nil)
		require.NoError(t, err)
		table.failsNext(1)
		setEnvironment(t, signedKey(t, f.priv, now.Add(-time.Hour), now.Add(time.Hour)), "")

		accepted := 0
		OfferEnvironmentUntilAnswered(t.Context(), f.store, every, func() { accepted++ }, quiet)

		require.Len(t, table.stored(), 1)
		require.Zero(t, accepted)
		require.Equal(t, 3, table.offersSeen())
	})

	t.Run("it returns when its context is done", func(t *testing.T) {
		f := newFixture(t, true)
		table := newMemoryTable(f)
		table.failsNext(1 << 30)
		setEnvironment(t, signedKey(t, f.priv, now, now.Add(time.Hour)), "")

		ctx, cancel := context.WithCancel(t.Context())
		returned := make(chan struct{})
		go func() {
			defer close(returned)
			OfferEnvironmentUntilAnswered(ctx, f.store, every, func() {}, quiet)
		}()
		require.Eventually(t, func() bool { return table.offersSeen() > 1 }, 5*time.Second, every)
		cancel()
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Fatal("the offers did not stop once the context was done")
		}
	})
}

// writeWhole replaces the file in one step, so that the watcher never reads half a key.
func writeWhole(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".tmp"
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o600))
	require.NoError(t, os.Rename(tmp, path))
}

func Test_WatchFile_OffersANewContentOnce(t *testing.T) {
	const every = 5 * time.Millisecond
	now := time.Now().UTC().Truncate(time.Second)

	f := newFixture(t, true)
	table := newMemoryTable(f)
	first := signedKey(t, f.priv, now, now.Add(time.Hour))
	second := signedKey(t, f.priv, now.Add(time.Minute), now.Add(time.Hour))
	path := filepath.Join(t.TempDir(), "license.key")
	writeWhole(t, path, first)

	var accepted atomic.Int64
	ctx, cancel := context.WithCancel(t.Context())
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		WatchFile(ctx, f.store, path, every, func() { accepted.Add(1) }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	require.Eventually(t, func() bool { return accepted.Load() == 1 }, 5*time.Second, every)
	// Many intervals go by with the same content: it is not offered again.
	time.Sleep(40 * every)
	require.Equal(t, 1, table.offersSeen())
	require.EqualValues(t, 1, accepted.Load())

	writeWhole(t, path, second)
	require.Eventually(t, func() bool { return accepted.Load() == 2 }, 5*time.Second, every)
	time.Sleep(40 * every)
	require.Equal(t, 2, table.offersSeen())
	require.EqualValues(t, 2, accepted.Load())

	stored := table.stored()
	require.Len(t, stored, 2)
	require.Equal(t, second, stored[1].Key)
	require.Equal(t, "file", stored[1].Origin)

	cancel()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("WatchFile did not return once its context was done")
	}
}
