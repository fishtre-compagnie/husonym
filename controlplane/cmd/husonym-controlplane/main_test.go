package main

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// runImport runs import-registry on a registry file, verified against the embedded keyring.
func runImport(t *testing.T, registryPath string) (stdout, stderr string, err error) {
	t.Helper()
	return runImportWith(t, license.EmbeddedKeyring, registryPath)
}

func runImportWith(
	t *testing.T, keyring func() (license.Keyring, error), registryPath string,
) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := newRootCmd(keyring)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"import-registry", "--registry", registryPath})
	err = cmd.ExecuteContext(t.Context())
	return out.String(), errOut.String(), err
}

// importBench is a database with one report pending for a license the registry file holds.
type importBench struct {
	pool     *pgxpool.Pool
	entry    license.RegistryEntry
	registry string
	keyring  func() (license.Keyring, error)
}

func newImportBench(t *testing.T) *importBench {
	t.Helper()
	pool := cptest.NewDatabase(t)
	t.Setenv(databaseURLEnv, pool.Config().ConnString())
	issuer := cptest.NewIssuer(t)
	b := &importBench{
		pool:     pool,
		entry:    issuer.Entry("lic-1", "cust-1", "Secret Customer Name"),
		registry: filepath.Join(t.TempDir(), "registry.json"),
		keyring:  func() (license.Keyring, error) { return issuer.Keyring(), nil },
	}
	require.NoError(t, (&license.Registry{Entries: []license.RegistryEntry{b.entry}}).Save(b.registry))

	sealed := cptest.ReportFor(t, &b.entry, "123e4567-e89b-12d3-a456-426614174000", time.Now())
	outcome, err := intake.New(cpstore.New(pool), time.Now).Receive(t.Context(), sealed.Document, sealed.Seal, sealed.Fingerprint)
	require.NoError(t, err)
	require.Equal(t, intake.Pending, outcome)
	return b
}

func (b *importBench) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	require.NoError(t, b.pool.QueryRow(t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
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

func Test_ImportRegistry_ImportsAndPromotesWhatWasPending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newImportBench(t)

	stdout, stderr, err := runImportWith(t, b.keyring, b.registry)

	require.NoError(t, err)
	require.Equal(t, "added: 1\nalready there: 0\nrefused: 0\n", stdout)
	require.Empty(t, stderr)
	require.Equal(t, 1, b.count(t, "licenses"))
	require.Equal(t, 1, b.count(t, "usage_reports"))
	require.Zero(t, b.count(t, "pending_reports"))

	stdout, _, err = runImportWith(t, b.keyring, b.registry)
	require.NoError(t, err)
	require.Equal(t, "added: 0\nalready there: 1\nrefused: 0\n", stdout)
}

func Test_ImportRegistry_PromotionFailure_StillPrintsTheCounts(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newImportBench(t)
	// The licenses can be written, the reports cannot be stored.
	_, err := b.pool.Exec(t.Context(), `ALTER TABLE controlplane.usage_reports RENAME TO usage_reports_away`)
	require.NoError(t, err)

	stdout, stderr, err := runImportWith(t, b.keyring, b.registry)

	require.Error(t, err)
	require.Equal(t, "added: 1\nalready there: 0\nrefused: 0\n", stdout)
	require.NotContains(t, stderr, "Usage")
	require.NotContains(t, stderr, "Secret Customer Name")
	require.NotContains(t, stderr, b.entry.Encoded)
	require.Equal(t, 1, b.count(t, "licenses"), "the import stays")
	require.Equal(t, 1, b.count(t, "pending_reports"), "what was pending waits for the maintenance")
}
