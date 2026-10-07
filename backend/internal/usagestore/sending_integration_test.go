package usagestore

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

var sendingNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func saveReportOf(t *testing.T, store *Store, day time.Time) {
	t.Helper()
	saved, err := store.SaveReport(t.Context(), StoredReport{
		Day: day, Document: []byte("{\"day\": \"" + day.Format(time.DateOnly) + "\"}\n"), Seal: "seal",
		KeyFingerprint: "fp", PreparedAt: day.Add(24*time.Hour + time.Minute),
	})
	require.NoError(t, err)
	require.True(t, saved)
}

func Test_StartSending_KeepsTheFirstDate(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)

	none, err := store.SendingSince(ctx)
	require.NoError(t, err)
	require.Nil(t, none)

	require.NoError(t, store.StartSending(ctx, sendingNow))
	require.NoError(t, store.StartSending(ctx, sendingNow.Add(time.Hour)))
	since, err := store.SendingSince(ctx)
	require.NoError(t, err)
	require.NotNil(t, since)
	require.True(t, sendingNow.Equal(*since))

	require.NoError(t, store.StopSending(ctx))
	stopped, err := store.SendingSince(ctx)
	require.NoError(t, err)
	require.Nil(t, stopped)

	later := sendingNow.Add(48 * time.Hour)
	require.NoError(t, store.StartSending(ctx, later))
	again, err := store.SendingSince(ctx)
	require.NoError(t, err)
	require.NotNil(t, again)
	require.True(t, later.Equal(*again))
}

func Test_ClaimReport_GivesTheOldestDueReportAndCountsTheAttempt(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	for i := range 3 {
		saveReportOf(t, store, october6.Add(time.Duration(i)*24*time.Hour))
	}
	from, to := october6, october6.Add(48*time.Hour)

	got, err := store.ClaimReport(ctx, from, to, sendingNow.Add(-time.Hour), sendingNow)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, october6.Equal(got.Day))
	require.Equal(t, []byte("{\"day\": \"2026-10-06\"}\n"), got.Document)
	require.Equal(t, "seal", got.Seal)
	require.Equal(t, "fp", got.KeyFingerprint)

	sendings, err := store.ListReportSendings(ctx, from, to)
	require.NoError(t, err)
	require.Len(t, sendings, 3)
	oldest := sendings[2]
	require.Equal(t, int32(1), oldest.Attempts)
	require.NotNil(t, oldest.LastAttemptAt)
	require.True(t, sendingNow.Equal(*oldest.LastAttemptAt))
	require.Nil(t, oldest.SentAt)
	require.Zero(t, sendings[0].Attempts)
	require.Nil(t, sendings[0].LastAttemptAt)

	// The oldest was tried since the cut-off: the next one is due.
	next, err := store.ClaimReport(ctx, from, to, sendingNow.Add(-time.Hour), sendingNow.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, next)
	require.True(t, october6.Add(24*time.Hour).Equal(next.Day))
}

func Test_ClaimReport_SkipsWhatDoesNotQualify(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	saveReportOf(t, store, october6)
	day := october6.Add(24 * time.Hour)
	saveReportOf(t, store, day)

	// Outside the range.
	out, err := store.ClaimReport(ctx, day.Add(24*time.Hour), day.Add(48*time.Hour), sendingNow, sendingNow)
	require.NoError(t, err)
	require.Nil(t, out)

	// The range is inclusive on both ends.
	in, err := store.ClaimReport(ctx, day, day, sendingNow, sendingNow)
	require.NoError(t, err)
	require.NotNil(t, in)
	require.True(t, day.Equal(in.Day))

	// Attempted since the cut-off: not due. Attempted before it: due again.
	recent, err := store.ClaimReport(ctx, day, day, sendingNow.Add(-time.Hour), sendingNow.Add(time.Minute))
	require.NoError(t, err)
	require.Nil(t, recent)
	retry, err := store.ClaimReport(ctx, day, day, sendingNow.Add(time.Minute), sendingNow.Add(2*time.Minute))
	require.NoError(t, err)
	require.NotNil(t, retry)
	sendings, err := store.ListReportSendings(ctx, day, day)
	require.NoError(t, err)
	require.Equal(t, int32(2), sendings[0].Attempts)

	// A report sent is not given again.
	require.NoError(t, store.MarkReportSent(ctx, day, sendingNow))
	sent, err := store.ClaimReport(ctx, day, day, sendingNow.Add(time.Hour), sendingNow.Add(time.Hour))
	require.NoError(t, err)
	require.Nil(t, sent)
}

func Test_ClaimReport_TwoCallsAtOnceNeverGetTheSameReport(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	const reports = 20
	for i := range reports {
		day := october6.Add(time.Duration(i) * 24 * time.Hour)
		saveReportOf(t, store, day)

		start := make(chan struct{})
		results := make([]*StoredReport, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for j := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				results[j], errs[j] = store.ClaimReport(ctx, day, day, sendingNow.Add(-time.Hour), sendingNow)
			}()
		}
		close(start)
		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		claimed := 0
		for _, r := range results {
			if r != nil {
				claimed++
			}
		}
		require.Equal(t, 1, claimed, "report %d must go to exactly one caller", i)
		sendings, err := store.ListReportSendings(ctx, day, day)
		require.NoError(t, err)
		require.Equal(t, int32(1), sendings[0].Attempts)
	}
}

func Test_MarkReportSent_KeepsTheFirstDate(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	saveReportOf(t, store, october6)

	never, err := store.LastSentAt(ctx)
	require.NoError(t, err)
	require.Nil(t, never)

	require.NoError(t, store.MarkReportSent(ctx, october6, sendingNow))
	require.NoError(t, store.MarkReportSent(ctx, october6, sendingNow.Add(time.Hour)))
	sendings, err := store.ListReportSendings(ctx, october6, october6)
	require.NoError(t, err)
	require.Len(t, sendings, 1)
	require.NotNil(t, sendings[0].SentAt)
	require.True(t, sendingNow.Equal(*sendings[0].SentAt))

	// Marking a day that has no report changes nothing and is no error.
	require.NoError(t, store.MarkReportSent(ctx, october6.Add(24*time.Hour), sendingNow))
}

func Test_ListReportSendings_NewestFirstWithinTheRange(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	for i := range 4 {
		saveReportOf(t, store, october6.Add(time.Duration(i)*24*time.Hour))
	}

	got, err := store.ListReportSendings(ctx, october6.Add(24*time.Hour), october6.Add(72*time.Hour))
	require.NoError(t, err)
	require.Len(t, got, 3)
	for i, want := range []int{3, 2, 1} {
		require.True(t, october6.Add(time.Duration(want)*24*time.Hour).Equal(got[i].Day))
	}
	require.True(t, october6.Add(72*time.Hour+time.Minute).Equal(got[1].PreparedAt))
}

func Test_LastSentAt_IsTheLatestSending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	saveReportOf(t, store, october6)
	saveReportOf(t, store, october6.Add(24*time.Hour))

	require.NoError(t, store.MarkReportSent(ctx, october6.Add(24*time.Hour), sendingNow))
	require.NoError(t, store.MarkReportSent(ctx, october6, sendingNow.Add(time.Hour)))
	at, err := store.LastSentAt(ctx)
	require.NoError(t, err)
	require.NotNil(t, at)
	require.True(t, sendingNow.Add(time.Hour).Equal(*at))
}

func Test_SendingMigrationDown_LeavesTheReportTables(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	container, _ := migratedDatabase(ctx, t)

	columns := func(table string, names ...string) int {
		var n int
		require.NoError(t, container.DB.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.columns
			 WHERE table_schema = 'husonym_api' AND table_name = $1 AND column_name = ANY($2)`,
			table, names).Scan(&n))
		return n
	}
	require.Equal(t, 3, columns("usage_reports", "sent_at", "attempts", "last_attempt_at"))
	require.Equal(t, 1, columns("instance", "sending_since"))

	down, err := os.ReadFile(schemaDir + "/20261009100000_adds-usage-report-sending.down.sql")
	require.NoError(t, err)
	_, err = container.DB.Exec(ctx, string(down))
	require.NoError(t, err)

	require.Zero(t, columns("usage_reports", "sent_at", "attempts", "last_attempt_at"))
	require.Zero(t, columns("instance", "sending_since"))
	require.Equal(t, 5, columns("usage_reports", "day", "document", "seal", "key_fingerprint", "prepared_at"),
		"the report tables of the previous migration stay")
	require.Equal(t, 1, columns("instance", "id"))
}
