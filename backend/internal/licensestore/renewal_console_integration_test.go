package licensestore

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/issuing"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// consoleIssue issues a license the way the operator console does: the draft is carried through
// its form, which draws the id, signed, then recorded, as the successor of the draft's own
// Succeeds. It returns what was issued.
func consoleIssue(
	t *testing.T, store *cpstore.Store, signer *issuing.Signer, draft *issuing.Draft, at time.Time,
) *license.IssuedLicense {
	t.Helper()
	confirmed, problems := issuing.ParseDraft(draft.Form(), at)
	require.Empty(t, problems)
	require.NotNil(t, confirmed)
	issued, key, err := signer.Issue(confirmed, at)
	require.NoError(t, err)
	added, err := store.RecordIssuedLicense(
		t.Context(), "operator@example.com", key, issued, signer.PublicKeyFingerprint(), confirmed.Succeeds, confirmed.Note, at)
	require.NoError(t, err)
	require.True(t, added)
	return issued
}

// A license the console issues as a renewal is one the product takes as a renewal: drafted from
// the license it succeeds, signed and recorded by the console's own path, then offered to the
// real store of an instance that holds the predecessor, it becomes the key in force. Nothing of
// the rule is bent for it: the same customer, and issued after the key it succeeds.
func Test_Offer_ALicenseTheConsoleIssuesAsARenewal_IsAcceptedAsOne(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()

	// The console, on the database of the control plane, signing with a key of the ring.
	pub, priv := newPair(t)
	ring := license.Keyring{license.LegacyKid: pub}
	signer, err := issuing.NewSigner(priv, ring)
	require.NoError(t, err)
	controlPlane := cpstore.New(cptest.NewDatabase(t))
	// The first license was issued an hour ago; its renewal is issued now.
	now := time.Now().UTC().Truncate(time.Second)
	before := now.Add(-time.Hour)
	customerID, err := controlPlane.CreateCustomer(ctx, "operator@example.com",
		cpstore.NewCustomer{ExternalID: "cust-001", Name: "Acme Co."}, before)
	require.NoError(t, err)
	customer, err := controlPlane.Customer(ctx, customerID, before)
	require.NoError(t, err)

	first := consoleIssue(t, controlPlane, signer, issuing.ShortDraft(customer, before), before)
	previous, err := controlPlane.LicenseDetail(ctx, first.Id, now)
	require.NoError(t, err)
	renewal, err := issuing.RenewalDraft(previous, customer, now)
	require.NoError(t, err)
	renewed := consoleIssue(t, controlPlane, signer, renewal, now)
	require.NotEqual(t, first.Id, renewed.Id)
	successor, cutShort, err := controlPlane.LatestSuccessor(ctx, first.Id)
	require.NoError(t, err)
	require.False(t, cutShort)
	require.NotNil(t, successor, "the console recorded the renewal as the successor")
	require.Equal(t, renewed.Id, successor.Id)

	// The instance, on its own database, trusting the same ring and holding the first license.
	instance := storeOn(ctx, t, migratedDatabase(ctx, t).URL, ring)
	held, err := instance.Offer(ctx, first.Encoded, OriginInterface, nil)
	require.NoError(t, err)
	require.Equal(t, Accepted, held.Outcome)

	// What the control plane would answer is the stored value of the successor.
	result, err := instance.Offer(ctx, successor.Encoded, OriginRenewal, nil)

	require.NoError(t, err)
	require.Equal(t, Accepted, result.Outcome, result.Reason)
	require.Equal(t, renewed.Id, result.Key.Id)
	require.Equal(t, "cust-001", result.Key.CustomerId)
	current, err := instance.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, successor.Encoded, current)
	installation, err := instance.Installation(ctx, renewed.Id)
	require.NoError(t, err)
	require.NotNil(t, installation)
	require.Equal(t, OriginRenewal, installation.Origin)

	// Offered again, as it is at every ask until another is issued, it changes nothing.
	again, err := instance.Offer(ctx, successor.Encoded, OriginRenewal, nil)
	require.NoError(t, err)
	require.Equal(t, Unchanged, again.Outcome)
}
