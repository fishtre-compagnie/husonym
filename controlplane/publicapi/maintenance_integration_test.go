package publicapi_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/controlplane/publicapi"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// startMaintenance runs the maintenance with a short tick until the test ends.
func startMaintenance(t *testing.T, receiver *intake.Intake, store *cpstore.Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		publicapi.RunMaintenance(ctx, receiver, store, slog.New(slog.NewTextHandler(io.Discard, nil)),
			time.Now, 10*time.Millisecond)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func Test_RunMaintenance_PromotesAReportWhoseLicenseCameAfterIt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	receiver := intake.New(store, time.Now)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	startMaintenance(t, receiver, store)

	// The report comes while the license is not there; the license is recorded behind the back
	// of the intake, as import-registry does while an instance posts.
	sealed := cptest.ReportFor(t, &entry, instanceA, time.Now())
	outcome, err := receiver.Receive(t.Context(), sealed.Document, sealed.Seal, sealed.Fingerprint)
	require.NoError(t, err)
	require.Equal(t, intake.Pending, outcome)
	added, err := store.AddLicense(t.Context(), issuer.Key(&entry), &entry, "registry")
	require.NoError(t, err)
	require.True(t, added)

	require.Eventually(t, func() bool {
		return count(t, pool, "usage_reports") == 1 && count(t, pool, "pending_reports") == 0
	}, 10*time.Second, 20*time.Millisecond)
}

func Test_RunMaintenance_PurgesThePendingReportsKeptTooLong(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	longAgo := time.Now().Add(-intake.PendingKept - 24*time.Hour)
	old := intake.New(store, func() time.Time { return longAgo })
	for _, at := range []time.Time{longAgo, time.Now()} {
		sealed := cptest.ReportFor(t, &entry, instanceA, at)
		receiver := old
		if at.After(longAgo) {
			receiver = intake.New(store, time.Now)
		}
		outcome, err := receiver.Receive(t.Context(), sealed.Document, sealed.Seal, sealed.Fingerprint)
		require.NoError(t, err)
		require.Equal(t, intake.Pending, outcome)
	}
	require.Equal(t, 2, count(t, pool, "pending_reports"))

	startMaintenance(t, intake.New(store, time.Now), store)

	require.Eventually(t, func() bool { return count(t, pool, "pending_reports") == 1 },
		10*time.Second, 20*time.Millisecond)
}
