package usagereport

import (
	"bytes"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// Two replicas that send at once, one report due: the destination gets one request, whose body
// is the stored document byte for byte, and the report is marked as sent.
func Test_SendDue_TwoReplicasAtOnceSendAReportOnce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, err := tcpostgres.NewPostgresTestContainer(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.TearDown(t.Context()) })
	require.NoError(t, neomigrate.Up(ctx, container.URL, "../../sql/postgresql/schema", testutil.GetTestLogger(t)))
	pool, err := pgxpool.New(ctx, container.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := usagestore.New(husonymdb.New(pool, db_queries.New()))

	stored := *storedReport
	stored.Day = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	stored.PreparedAt = sendNow.Add(-11 * time.Hour)
	saved, err := store.SaveReport(ctx, stored)
	require.NoError(t, err)
	require.True(t, saved)
	require.NoError(t, store.StartSending(ctx, sendNow.AddDate(0, 0, -3)))

	destination := newReceiver(t, http.StatusNoContent, nil)
	logger := slog.New(slog.DiscardHandler)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = sendDueAt(ctx, NewSender(
				store, &fakeLicense{inForce: true}, &fakeKeyMode{mode: license.TelemetryOnline}, "",
				transportTo(t, destination.URL), logger,
			), sendNow)
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	requests := destination.got()
	require.Len(t, requests, 1)
	require.True(t, bytes.Equal(stored.Document, requests[0].body), "the body is not the stored document: %q", requests[0].body)
	require.Equal(t, stored.Seal, requests[0].header.Get("Husonym-Seal"))
	require.Equal(t, stored.KeyFingerprint, requests[0].header.Get("Husonym-Key-Fingerprint"))

	sendings, err := store.ListReportSendings(ctx, stored.Day, stored.Day)
	require.NoError(t, err)
	require.Len(t, sendings, 1)
	require.NotNil(t, sendings[0].SentAt)
	require.True(t, sendNow.Equal(*sendings[0].SentAt))
	require.EqualValues(t, 1, sendings[0].Attempts)

	// A later pass has nothing left to send.
	require.NoError(t, sendDueAt(ctx, NewSender(
		store, &fakeLicense{inForce: true}, &fakeKeyMode{mode: license.TelemetryOnline}, "",
		transportTo(t, destination.URL), logger,
	), sendNow.Add(7*time.Hour)))
	require.Len(t, destination.got(), 1)
}
