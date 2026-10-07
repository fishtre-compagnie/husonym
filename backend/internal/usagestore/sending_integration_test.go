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

// claimOf is a claim on the reports of the days in [from, to], whenever they were prepared.
func claimOf(from, to, notAttemptedSince, at time.Time) ReportClaim {
	return ReportClaim{From: from, To: to, NotAttemptedSince: notAttemptedSince, At: at}
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
	require.NoError(t, store.StartSending(ctx, october6))
	for i := range 3 {
		saveReportOf(t, store, october6.Add(time.Duration(i)*24*time.Hour))
	}
	from, to := october6, october6.Add(48*time.Hour)

	got, err := store.ClaimReport(ctx, claimOf(from, to, sendingNow.Add(-time.Hour), sendingNow))
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
	next, err := store.ClaimReport(ctx, claimOf(from, to, sendingNow.Add(-time.Hour), sendingNow.Add(time.Minute)))
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
	require.NoError(t, store.StartSending(ctx, october6))
	saveReportOf(t, store, october6)
	day := october6.Add(24 * time.Hour)
	saveReportOf(t, store, day)

	// Outside the range.
	out, err := store.ClaimReport(ctx, claimOf(day.Add(24*time.Hour), day.Add(48*time.Hour), sendingNow, sendingNow))
	require.NoError(t, err)
	require.Nil(t, out)

	// The range is inclusive on both ends.
	in, err := store.ClaimReport(ctx, claimOf(day, day, sendingNow, sendingNow))
	require.NoError(t, err)
	require.NotNil(t, in)
	require.True(t, day.Equal(in.Day))

	// Attempted since the cut-off: not due. Attempted before it: due again.
	recent, err := store.ClaimReport(ctx, claimOf(day, day, sendingNow.Add(-time.Hour), sendingNow.Add(time.Minute)))
	require.NoError(t, err)
	require.Nil(t, recent)
	retry, err := store.ClaimReport(ctx, claimOf(day, day, sendingNow.Add(time.Minute), sendingNow.Add(2*time.Minute)))
	require.NoError(t, err)
	require.NotNil(t, retry)
	sendings, err := store.ListReportSendings(ctx, day, day)
	require.NoError(t, err)
	require.Equal(t, int32(2), sendings[0].Attempts)

	// A report sent is not given again.
	require.NoError(t, store.MarkReportSent(ctx, day, sendingNow))
	sent, err := store.ClaimReport(ctx, claimOf(day, day, sendingNow.Add(time.Hour), sendingNow.Add(time.Hour)))
	require.NoError(t, err)
	require.Nil(t, sent)
}

// With a bound on the preparation, a report prepared after it is not due, and an older one that
// is prepared by then is taken in its place.
func Test_ClaimReport_LeavesAReportPreparedAfterTheBound(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	require.NoError(t, store.StartSending(ctx, october6))
	saveReportOf(t, store, october6)
	next := october6.Add(24 * time.Hour)
	saveReportOf(t, store, next)
	// saveReportOf prepares a report a minute after its day closed.
	preparedAt := october6.Add(24*time.Hour + time.Minute)

	claim := claimOf(october6, next, sendingNow.Add(-time.Hour), sendingNow)
	claim.PreparedBy = new(preparedAt.Add(-time.Second))
	none, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.Nil(t, none)
	sendings, err := store.ListReportSendings(ctx, october6, next)
	require.NoError(t, err)
	require.Zero(t, sendings[0].Attempts+sendings[1].Attempts, "a report that is not due is not counted as tried")

	// The bound is inclusive, and the report of the next day is still after it.
	claim.PreparedBy = &preparedAt
	oldest, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.NotNil(t, oldest)
	require.True(t, october6.Equal(oldest.Day))
	again, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.Nil(t, again)

	// Without a bound the next one is due.
	claim.PreparedBy = nil
	unbounded, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.NotNil(t, unbounded)
	require.True(t, next.Equal(unbounded.Day))
}

// A claim that leaves the diagnostics out skips a report whose document carries them, whatever
// the document holds besides, and the listing tells which reports those are.
func Test_ClaimReport_LeavesAReportThatCarriesDiagnosticsWhenToldTo(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	require.NoError(t, store.StartSending(ctx, october6))
	with, without, nested := october6, october6.Add(24*time.Hour), october6.Add(48*time.Hour)
	for day, document := range map[time.Time]string{
		with:    "{\"day\": \"2026-10-06\", \"diagnostics\": {\"runs\": {\"completed\": 3}}}\n",
		without: "{\"day\": \"2026-10-07\", \"sources\": 2}\n",
		// The word is there, and not as the block of the report.
		nested: "{\"day\": \"2026-10-08\", \"license\": {\"diagnostics\": \"diagnostics\"}}\n",
	} {
		saved, err := store.SaveReport(ctx, StoredReport{
			Day: day, Document: []byte(document), Seal: "seal", KeyFingerprint: "fp", PreparedAt: day.Add(25 * time.Hour),
		})
		require.NoError(t, err)
		require.True(t, saved)
	}

	sendings, err := store.ListReportSendings(ctx, with, nested)
	require.NoError(t, err)
	require.Len(t, sendings, 3)
	require.False(t, sendings[0].Diagnostics, "the newest: the word is nested")
	require.False(t, sendings[1].Diagnostics)
	require.True(t, sendings[2].Diagnostics, "the oldest carries the diagnostics")

	claim := claimOf(with, nested, sendingNow.Add(-time.Hour), sendingNow)
	claim.WithoutDiagnostics = true
	for _, want := range []time.Time{without, nested} {
		got, err := store.ClaimReport(ctx, claim)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.True(t, want.Equal(got.Day), "got the report of %s", got.Day)
	}
	none, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.Nil(t, none)
	sendings, err = store.ListReportSendings(ctx, with, with)
	require.NoError(t, err)
	require.Zero(t, sendings[0].Attempts, "a report that stays is not counted as tried")

	// Told otherwise, the report is due, and comes as it is stored.
	claim.WithoutDiagnostics = false
	got, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.True(t, with.Equal(got.Day))
	require.Equal(t, "{\"day\": \"2026-10-06\", \"diagnostics\": {\"runs\": {\"completed\": 3}}}\n", string(got.Document))
}

// The claim itself asks whether the instance sends: a replica that read that it does, before
// another one recorded that it does not, gets no report and counts no attempt.
func Test_ClaimReport_GivesNothingWhileTheInstanceDoesNotSend(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	saveReportOf(t, store, october6)
	next := october6.Add(24 * time.Hour)
	saveReportOf(t, store, next)
	claim := claimOf(october6, next, sendingNow.Add(-time.Hour), sendingNow)

	never, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.Nil(t, never, "an instance that never sent")

	require.NoError(t, store.StartSending(ctx, october6))
	first, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.True(t, october6.Equal(first.Day))

	require.NoError(t, store.StopSending(ctx))
	stopped, err := store.ClaimReport(ctx, claim)
	require.NoError(t, err)
	require.Nil(t, stopped, "an instance that stopped sending")
	sendings, err := store.ListReportSendings(ctx, next, next)
	require.NoError(t, err)
	require.Zero(t, sendings[0].Attempts)
	require.Nil(t, sendings[0].LastAttemptAt)
}

func Test_ClaimReport_TwoCallsAtOnceNeverGetTheSameReport(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	_, store := migratedDatabase(ctx, t)
	require.NoError(t, store.StartSending(ctx, october6))
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
				results[j], errs[j] = store.ClaimReport(ctx, claimOf(day, day, sendingNow.Add(-time.Hour), sendingNow))
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
