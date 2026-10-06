package license

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// setLicenseEnv sets what LoaderFromEnv reads and captures what it logs, for the time of
// the test. Tests that call it cannot run in parallel: both are process-wide.
func setLicenseEnv(t *testing.T, value, file string) *bytes.Buffer {
	t.Helper()
	logs := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	viper.Set("EE_LICENSE", value)
	viper.Set("EE_LICENSE_FILE", file)
	t.Cleanup(func() {
		viper.Set("EE_LICENSE", "")
		viper.Set("EE_LICENSE_FILE", "")
		slog.SetDefault(previous)
	})
	return logs
}

func Test_LoaderFromEnv_Nothing(t *testing.T) {
	logs := setLicenseEnv(t, "", "")

	value, err := LoaderFromEnv()(t.Context())

	require.NoError(t, err)
	require.Empty(t, value)
	require.Empty(t, logs.String())
}

func Test_LoaderFromEnv_Value(t *testing.T) {
	logs := setLicenseEnv(t, "the-key", "")

	value, err := LoaderFromEnv()(t.Context())

	require.NoError(t, err)
	require.Equal(t, "the-key", value)
	require.Empty(t, logs.String())
}

func Test_LoaderFromEnv_FileIsReadOnEachCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "license")
	require.NoError(t, os.WriteFile(path, []byte("first-key\n"), 0o600))
	logs := setLicenseEnv(t, "", path)
	load := LoaderFromEnv()

	value, err := load(t.Context())
	require.NoError(t, err)
	require.Equal(t, "first-key\n", value)

	require.NoError(t, os.WriteFile(path, []byte("second-key"), 0o600))
	value, err = load(t.Context())
	require.NoError(t, err)
	require.Equal(t, "second-key", value)
	require.Empty(t, logs.String())
}

func Test_LoaderFromEnv_FileWinsOverValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "license")
	require.NoError(t, os.WriteFile(path, []byte("key-of-the-file"), 0o600))
	logs := setLicenseEnv(t, "key-of-the-variable", path)
	load := LoaderFromEnv()

	for range 3 {
		value, err := load(t.Context())
		require.NoError(t, err)
		require.Equal(t, "key-of-the-file", value)
	}

	// The warning is given once, when the loader is built, not on each load.
	require.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("level=WARN")))
	require.Contains(t, logs.String(), "both EE_LICENSE and EE_LICENSE_FILE are set: the file wins")
}

func Test_LoaderFromEnv_FileThatCannotBeUsed(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "license")
		setLicenseEnv(t, "key-of-the-variable", path)

		value, err := LoaderFromEnv()(t.Context())

		require.ErrorIs(t, err, os.ErrNotExist)
		require.NotContains(t, err.Error(), path)
		require.Empty(t, value, "the variable is ignored once a file is named")
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "license")
		require.NoError(t, os.WriteFile(path, []byte(" \n"), 0o600))
		setLicenseEnv(t, "", path)

		value, err := LoaderFromEnv()(t.Context())

		require.EqualError(t, err, "the license file is empty")
		require.Empty(t, value)
	})
}

// Test_LoaderFromEnv_FeedsTheProvider is what an operator sees: a file that is replaced,
// damaged or removed, through the Provider that reads it.
func Test_LoaderFromEnv_FeedsTheProvider(t *testing.T) {
	f := newProviderFixture(t)
	path := filepath.Join(t.TempDir(), "license")
	require.NoError(t, os.WriteFile(path, []byte(f.issue(t, 90*day, 1)+"\n"), 0o600))
	setLicenseEnv(t, "", path)
	p := newProvider(LoaderFromEnv(), Keyring{LegacyKid: f.pub}, f.clock.Now, slog.New(slog.NewTextHandler(f.logs, nil)))

	require.NoError(t, p.Refresh(t.Context()))
	require.Equal(t, 1, maxJobsOf(t, p))

	for name, damage := range map[string]func() error{
		"emptied": func() error { return os.WriteFile(path, nil, 0o600) },
		"removed": func() error { return os.Remove(path) },
	} {
		require.NoError(t, damage(), name)
		require.Error(t, p.Refresh(t.Context()), name)
		require.Error(t, p.Problem(), name)
		require.Equal(t, 1, maxJobsOf(t, p), name)
		require.Equal(t, StateValid, p.State(), name)
	}

	require.NoError(t, os.WriteFile(path, []byte(f.issue(t, 90*day, 2)), 0o600))
	require.NoError(t, p.Refresh(t.Context()))
	require.NoError(t, p.Problem())
	require.Equal(t, 2, maxJobsOf(t, p))
}
