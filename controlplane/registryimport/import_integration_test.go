package registryimport_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
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

	first, err := registryimport.Run(ctx, store, intake.New(store, time.Now), registry, issuer.Keyring())
	require.NoError(t, err)
	require.Equal(t, registryimport.Result{Added: 2}, first)

	second, err := registryimport.Run(ctx, store, intake.New(store, time.Now), registry, issuer.Keyring())
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

	result, err := registryimport.Run(ctx, store, intake.New(store, time.Now), registry, issuer.Keyring())
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

// The issuer never mints such a key; one that is there all the same has no customer to go under.
func Test_Import_RefusesAKeyWithoutACustomer(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	orphan := issuer.EntryOf(&license.Key{
		Version:   "v1",
		Id:        "lic-orphan",
		IssuedTo:  "Nobody",
		IssuedAt:  time.Now().UTC().Truncate(time.Second),
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second),
	})
	// The entry names a customer; the key, which is what counts, does not.
	orphan.CustomerId = "cust-9"
	registry := &license.Registry{Entries: []license.RegistryEntry{orphan, issuer.Entry("lic-1", "cust-1", "Acme")}}

	result, err := registryimport.Run(ctx, store, intake.New(store, time.Now), registry, issuer.Keyring())

	require.NoError(t, err)
	require.Equal(t, registryimport.Result{Added: 1, Refused: 1}, result)
	var licenses, customers int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.licenses`).Scan(&licenses))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.customers`).Scan(&customers))
	require.Equal(t, 1, licenses)
	require.Equal(t, 1, customers)
}

const instance = "123e4567-e89b-12d3-a456-426614174000"

func Test_Import_PromotesThePendingReportsOfTheLicensesItAdds(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	receiver := intake.New(store, time.Now)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	sealed := cptest.ReportFor(t, &entry, instance, time.Now())
	outcome, err := receiver.Receive(ctx, sealed.Document, sealed.Seal, sealed.Fingerprint)
	require.NoError(t, err)
	require.Equal(t, intake.Pending, outcome)

	registry := &license.Registry{Entries: []license.RegistryEntry{entry}}
	result, err := registryimport.Run(ctx, store, receiver, registry, issuer.Keyring())

	require.NoError(t, err)
	require.Equal(t, registryimport.Result{Added: 1}, result)
	var pending, stored int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.pending_reports`).Scan(&pending))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.usage_reports`).Scan(&stored))
	require.Zero(t, pending)
	require.Equal(t, 1, stored)
}

// The product trims the key it is given before it derives the fingerprint and the seal: an entry
// written with whitespace around its key is the license of that fingerprint all the same.
func Test_Import_AKeyWithWhitespaceAround_IsFoundByTheFingerprintTheProductSends(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	receiver := intake.New(store, time.Now)
	issuer := cptest.NewIssuer(t)
	newline := issuer.Entry("lic-1", "cust-1", "Acme")
	spaces := issuer.Entry("lic-2", "cust-2", "Globex")
	// What the product holds, and seals with.
	trimmed := []license.RegistryEntry{newline, spaces}
	newline.Encoded = "\n" + newline.Encoded + "\r\n"
	spaces.Encoded = " \t" + spaces.Encoded + "  "
	registry := &license.Registry{Entries: []license.RegistryEntry{newline, spaces}}

	result, err := registryimport.Run(ctx, store, receiver, registry, issuer.Keyring())
	require.NoError(t, err)
	require.Equal(t, registryimport.Result{Added: 2}, result)

	for i := range trimmed {
		sealed := cptest.ReportFor(t, &trimmed[i], instance, time.Now())
		outcome, err := receiver.Receive(ctx, sealed.Document, sealed.Seal, sealed.Fingerprint)
		require.NoError(t, err)
		require.Equal(t, intake.Stored, outcome, trimmed[i].Id)
	}
}

type failingPromoter struct{}

func (failingPromoter) PromotePending(context.Context, string) (int, int, error) {
	return 0, 0, errors.New("promotion is down")
}

func Test_Import_PromotionFailure_IsReportedAndKeepsTheImport(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	registry := &license.Registry{Entries: []license.RegistryEntry{issuer.Entry("lic-1", "cust-1", "Acme")}}

	result, err := registryimport.Run(ctx, store, failingPromoter{}, registry, issuer.Keyring())

	require.ErrorContains(t, err, "promotion is down")
	require.Equal(t, registryimport.Result{Added: 1}, result)
	var licenses int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM controlplane.licenses`).Scan(&licenses))
	require.Equal(t, 1, licenses)
}
