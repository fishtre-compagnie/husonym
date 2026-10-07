package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func runImport(t *testing.T, registryPath string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"import-registry", "--registry", registryPath})
	err = cmd.ExecuteContext(t.Context())
	return out.String(), errOut.String(), err
}

func Test_ImportRegistry_MissingFile_Fails(t *testing.T) {
	t.Setenv(databaseURLEnv, "postgres://unused")

	stdout, _, err := runImport(t, filepath.Join(t.TempDir(), "nothing.json"))

	require.Error(t, err)
	require.Empty(t, stdout)
}

func Test_ImportRegistry_RefusedEntry_FailsWithOnlyTheCounts(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	t.Setenv(databaseURLEnv, pool.Config().ConnString())

	// Signed by a throwaway key, so it does not verify against the embedded keyring.
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Secret Customer Name")
	path := filepath.Join(t.TempDir(), "registry.json")
	require.NoError(t, (&license.Registry{Entries: []license.RegistryEntry{entry}}).Save(path))

	stdout, stderr, err := runImport(t, path)

	require.Error(t, err)
	require.Equal(t, "added: 0\nalready there: 0\nrefused: 1\n", stdout)
	require.NotContains(t, stderr, "Usage")
	require.NotContains(t, stderr, "Secret Customer Name")
	require.NotContains(t, stderr, entry.Encoded)
}
