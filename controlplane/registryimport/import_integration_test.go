package registryimport_test

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/registryimport"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func Test_Import_Twice_ChangesNothing(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	registry := &license.Registry{Entries: []license.RegistryEntry{
		issuer.Entry("lic-1", "cust-1", "Acme"),
		issuer.Entry("lic-2", "cust-2", "Globex"),
	}}

	first, err := registryimport.Run(ctx, store, registry, issuer.Keyring())
	require.NoError(t, err)
	require.Equal(t, registryimport.Result{Added: 2}, first)

	second, err := registryimport.Run(ctx, store, registry, issuer.Keyring())
	require.NoError(t, err)
	require.Equal(t, registryimport.Result{AlreadyThere: 2}, second)

	var licenses int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.licenses`).Scan(&licenses))
	require.Equal(t, 2, licenses)
}

func Test_Import_RefusesAKeyThatDoesNotVerify(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	foreign := cptest.NewIssuer(t).Entry("lic-foreign", "cust-3", "Initech")
	renamed := issuer.Entry("lic-real", "cust-4", "Hooli")
	renamed.Id = "lic-other" // the id of the entry differs from the one inside the key
	registry := &license.Registry{Entries: []license.RegistryEntry{
		issuer.Entry("lic-1", "cust-1", "Acme"),
		foreign,
		renamed,
		issuer.Entry("lic-2", "cust-2", "Globex"),
	}}

	result, err := registryimport.Run(ctx, store, registry, issuer.Keyring())
	require.NoError(t, err)
	require.Equal(t, registryimport.Result{Added: 2, Refused: 2}, result)

	var ids []string
	rows, err := pool.Query(ctx, `SELECT id FROM controlplane.licenses ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id string
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"lic-1", "lic-2"}, ids)
}
