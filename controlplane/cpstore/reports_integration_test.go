package cpstore_test

import (
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	instanceA    = "123e4567-e89b-12d3-a456-426614174000"
	maxInstances = 50
)

var (
	received = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	caps     = cpstore.PendingCaps{PerFingerprint: 3, Total: 5}
)

func day(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.DateOnly, value)
	require.NoError(t, err)
	return parsed
}

// storeWithLicense returns a store that knows the license lic-1.
func storeWithLicense(t *testing.T) (*cpstore.Store, *pgxpool.Pool) {
	t.Helper()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	require.True(t, add(t, store, issuer, &entry))
	return store, pool
}

func reportOf(t *testing.T, reportDay, document string) *cpstore.Report {
	t.Helper()
	return &cpstore.Report{
		InstanceID:     instanceA,
		Day:            day(t, reportDay),
		LicenseID:      "lic-1",
		Document:       []byte(document),
		Seal:           "seal",
		HusonymVersion: "v0.3.0",
		InstallKind:    "helm",
		ReceivedAt:     received,
	}
}

func pendingOf(t *testing.T, fingerprint, instance string, at time.Time) *cpstore.PendingReport {
	t.Helper()
	return &cpstore.PendingReport{
		KeyFingerprint: fingerprint,
		InstanceID:     instance,
		Day:            day(t, "2026-10-06"),
		Document:       []byte(`{"n":1}`),
		Seal:           "seal",
		ReceivedAt:     at,
	}
}

func count(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

func Test_StoreReport_FirstStays_RepeatAndConflictAreToldApart(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	store, pool := storeWithLicense(t)

	outcome, err := store.StoreReport(ctx, reportOf(t, "2026-10-06", `{"n":1}`), maxInstances)
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportStored, outcome)

	outcome, err = store.StoreReport(ctx, reportOf(t, "2026-10-06", `{"n":1}`), maxInstances)
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportRepeat, outcome)

	later := reportOf(t, "2026-10-06", `{"n":2}`)
	later.ReceivedAt = received.Add(2 * time.Hour)
	outcome, err = store.StoreReport(ctx, later, maxInstances)
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportConflict, outcome)

	// A conflict received earlier, as a promoted pending report can be, is counted and does
	// not move the moment of the last conflict backwards.
	between := reportOf(t, "2026-10-06", `{"n":3}`)
	between.ReceivedAt = received.Add(time.Hour)
	outcome, err = store.StoreReport(ctx, between, maxInstances)
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportConflict, outcome)

	var document string
	var conflicts int
	var lastConflictAt *time.Time
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT document, conflicts, last_conflict_at FROM controlplane.usage_reports`,
	).Scan(&document, &conflicts, &lastConflictAt))
	require.JSONEq(t, `{"n":1}`, document)
	require.Equal(t, 2, conflicts)
	require.NotNil(t, lastConflictAt)
	require.True(t, later.ReceivedAt.Equal(*lastConflictAt))
}

func Test_StoreReport_TwoLicenses_ShareNothing(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	store, pool := storeWithLicense(t)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-2", "cust-2", "Other")
	require.True(t, add(t, store, issuer, &entry))

	first := reportOf(t, "2026-10-06", `{"n":1}`)
	second := reportOf(t, "2026-10-06", `{"n":2}`)
	second.LicenseID = "lic-2"
	second.HusonymVersion = "v0.9.0"
	second.InstallKind = "compose"
	second.ReceivedAt = received.Add(time.Hour)
	for _, report := range []*cpstore.Report{first, second} {
		outcome, err := store.StoreReport(ctx, report, maxInstances)
		require.NoError(t, err)
		require.Equal(t, cpstore.ReportStored, outcome)
	}

	for _, want := range []*cpstore.Report{first, second} {
		var document, version, kind string
		var conflicts int
		var firstSeen, lastSeen time.Time
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT r.document, r.conflicts, i.husonym_version, i.install_kind, i.first_seen_at, i.last_seen_at
			 FROM controlplane.usage_reports r
			 JOIN controlplane.instances i USING (license_id, instance_id)
			 WHERE r.license_id = $1 AND r.instance_id = $2`, want.LicenseID, instanceA,
		).Scan(&document, &conflicts, &version, &kind, &firstSeen, &lastSeen))
		require.Equal(t, string(want.Document), document) //nolint:testifylint // the exact bytes stored
		require.Zero(t, conflicts)
		require.Equal(t, want.HusonymVersion, version)
		require.Equal(t, want.InstallKind, kind)
		require.True(t, want.ReceivedAt.Equal(firstSeen))
		require.True(t, want.ReceivedAt.Equal(lastSeen))
	}
	require.Equal(t, 2, count(t, pool, "instances"))
}

func Test_StoreReport_InstancesOfALicenseAreCapped(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	store, pool := storeWithLicense(t)
	put := func(instance, reportDay string) cpstore.ReportOutcome {
		report := reportOf(t, reportDay, `{"n":1}`)
		report.InstanceID = instance
		outcome, err := store.StoreReport(ctx, report, 2)
		require.NoError(t, err)
		return outcome
	}

	require.Equal(t, cpstore.ReportStored, put("i-1", "2026-10-05"))
	require.Equal(t, cpstore.ReportStored, put("i-2", "2026-10-05"), "the last instance under the cap")
	require.Equal(t, cpstore.ReportTooManyInstances, put("i-3", "2026-10-05"))
	require.Equal(t, 2, count(t, pool, "instances"))
	require.Equal(t, 2, count(t, pool, "usage_reports"))

	require.Equal(t, cpstore.ReportStored, put("i-1", "2026-10-06"), "an instance already seen is not held by the cap")
	require.Equal(t, cpstore.ReportRepeat, put("i-2", "2026-10-05"))
}

func Test_StoreReport_FirstSeenIsTheEarliestReception(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	store, pool := storeWithLicense(t)

	_, err := store.StoreReport(ctx, reportOf(t, "2026-10-06", `{"n":1}`), maxInstances)
	require.NoError(t, err)
	// Received before, stored after: a pending report that was promoted.
	promoted := reportOf(t, "2026-10-07", `{"n":2}`)
	promoted.ReceivedAt = received.Add(-time.Hour)
	_, err = store.StoreReport(ctx, promoted, maxInstances)
	require.NoError(t, err)

	var firstSeen, lastSeen time.Time
	var lastDay string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT first_seen_at, last_seen_at, last_report_day::text FROM controlplane.instances`,
	).Scan(&firstSeen, &lastSeen, &lastDay))
	require.True(t, promoted.ReceivedAt.Equal(firstSeen))
	require.True(t, received.Equal(lastSeen), "the last news does not move backwards")
	require.Equal(t, "2026-10-07", lastDay)
}

func Test_StoreReport_AnEarlierDayLeavesTheInstanceAsItWas(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	store, pool := storeWithLicense(t)

	first := reportOf(t, "2026-10-06", `{"n":1}`)
	_, err := store.StoreReport(ctx, first, maxInstances)
	require.NoError(t, err)

	earlier := reportOf(t, "2026-10-05", `{"n":2}`)
	earlier.ReceivedAt = received.Add(time.Hour)
	earlier.HusonymVersion = "v0.2.0"
	earlier.InstallKind = "compose"
	outcome, err := store.StoreReport(ctx, earlier, maxInstances)
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportStored, outcome)

	read := func() (firstSeen, lastSeen, lastDay time.Time, version string, kind *string) {
		require.NoError(t, pool.QueryRow(ctx,
			`SELECT first_seen_at, last_seen_at, last_report_day, husonym_version, install_kind
			 FROM controlplane.instances WHERE instance_id = $1`, instanceA,
		).Scan(&firstSeen, &lastSeen, &lastDay, &version, &kind))
		return firstSeen, lastSeen, lastDay, version, kind
	}
	firstSeen, lastSeen, lastDay, version, kind := read()
	require.True(t, received.Equal(firstSeen))
	require.True(t, received.Equal(lastSeen))
	require.True(t, day(t, "2026-10-06").Equal(lastDay))
	require.Equal(t, "v0.3.0", version)
	require.Equal(t, "helm", *kind)
	require.Equal(t, 2, count(t, pool, "usage_reports"))

	// A later day advances what tells the latest state; a report without the diagnostics keeps
	// the kind of installation already known.
	next := reportOf(t, "2026-10-07", `{"n":3}`)
	next.ReceivedAt = received.Add(24 * time.Hour)
	next.HusonymVersion = "v0.4.0"
	next.InstallKind = ""
	_, err = store.StoreReport(ctx, next, maxInstances)
	require.NoError(t, err)

	firstSeen, lastSeen, lastDay, version, kind = read()
	require.True(t, received.Equal(firstSeen))
	require.True(t, next.ReceivedAt.Equal(lastSeen))
	require.True(t, day(t, "2026-10-07").Equal(lastDay))
	require.Equal(t, "v0.4.0", version)
	require.Equal(t, "helm", *kind)
}

func Test_StoreReport_Concurrent_StoresOnce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	for name, documents := range map[string][2]string{
		"same document":       {`{"n":1}`, `{"n":1}`},
		"different documents": {`{"n":1}`, `{"n":2}`},
	} {
		t.Run(name, func(t *testing.T) {
			store, pool := storeWithLicense(t)
			reports := [2]*cpstore.Report{
				reportOf(t, "2026-10-06", documents[0]),
				reportOf(t, "2026-10-06", documents[1]),
			}

			outcomes := make([]cpstore.ReportOutcome, len(reports))
			errs := make([]error, len(reports))
			var wg sync.WaitGroup
			for i := range reports {
				wg.Go(func() {
					outcomes[i], errs[i] = store.StoreReport(t.Context(), reports[i], maxInstances)
				})
			}
			wg.Wait()

			for _, err := range errs {
				require.NoError(t, err)
			}
			other := cpstore.ReportRepeat
			if documents[0] != documents[1] {
				other = cpstore.ReportConflict
			}
			require.ElementsMatch(t, []cpstore.ReportOutcome{cpstore.ReportStored, other}, outcomes)
			require.Equal(t, 1, count(t, pool, "usage_reports"))
			require.Equal(t, 1, count(t, pool, "instances"))
		})
	}
}

func Test_CountSealRejection_CountsPerLicenseAndUTCDay(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	store, pool := storeWithLicense(t)
	paris := time.FixedZone("UTC+2", 2*60*60)

	// 01:30 at UTC+2 is still the day before in UTC.
	require.NoError(t, store.CountSealRejection(ctx, "lic-1", time.Date(2026, 10, 7, 1, 30, 0, 0, paris)))
	require.NoError(t, store.CountSealRejection(ctx, "lic-1", time.Date(2026, 10, 6, 23, 45, 0, 0, time.UTC)))
	require.NoError(t, store.CountSealRejection(ctx, "lic-1", time.Date(2026, 10, 7, 0, 15, 0, 0, time.UTC)))

	rows, err := pool.Query(ctx, `SELECT day::text, count FROM controlplane.seal_rejections ORDER BY day`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var d string
		var n int
		require.NoError(t, rows.Scan(&d, &n))
		got[d] = n
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]int{"2026-10-06": 2, "2026-10-07": 1}, got)
}

func Test_KeepPending_Caps(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	keep := func(fingerprint, instance string) bool {
		kept, err := store.KeepPending(ctx, pendingOf(t, fingerprint, instance, received), caps)
		require.NoError(t, err)
		return kept
	}

	require.True(t, keep("fp-1", "i-1"))
	require.True(t, keep("fp-1", "i-2"))
	require.True(t, keep("fp-1", "i-3"))
	require.False(t, keep("fp-1", "i-4"), "the cap of a fingerprint is reached")
	require.True(t, keep("fp-1", "i-1"), "a report already pending is not counted again")

	require.True(t, keep("fp-2", "i-1"))
	require.True(t, keep("fp-2", "i-2"))
	require.False(t, keep("fp-3", "i-1"), "the cap of the whole is reached")
	require.True(t, keep("fp-2", "i-2"), "a report already pending is not counted again")
	require.Equal(t, 5, count(t, pool, "pending_reports"))
}

func Test_KeepPending_Concurrent_KeepsOnce(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)

	const callers = 4
	kept := make([]bool, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() {
			kept[i], errs[i] = store.KeepPending(t.Context(), pendingOf(t, "fp-1", "i-1", received), caps)
		})
	}
	wg.Wait()

	for i := range callers {
		require.NoError(t, errs[i])
		require.True(t, kept[i])
	}
	require.Equal(t, 1, count(t, pool, "pending_reports"))
}

func Test_KeepPending_AnotherSealIsAnotherReport(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)

	first := pendingOf(t, "fp-1", "i-1", received)
	other := pendingOf(t, "fp-1", "i-1", received)
	other.Seal = "another seal"
	other.Document = []byte(`{"n":2}`)
	for _, p := range []*cpstore.PendingReport{first, other, first} {
		kept, err := store.KeepPending(ctx, p, caps)
		require.NoError(t, err)
		require.True(t, kept)
	}

	pending, err := store.PendingReports(ctx, "fp-1")
	require.NoError(t, err)
	require.Len(t, pending, 2)

	// Each one counts against the caps.
	third := pendingOf(t, "fp-1", "i-1", received)
	third.Seal = "a third seal"
	kept, err := store.KeepPending(ctx, third, caps)
	require.NoError(t, err)
	require.True(t, kept)
	fourth := pendingOf(t, "fp-1", "i-1", received)
	fourth.Seal = "a fourth seal"
	kept, err = store.KeepPending(ctx, fourth, caps)
	require.NoError(t, err)
	require.False(t, kept)
}

func Test_KeepPending_AlreadyThere_ChangesNothing(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)

	kept, err := store.KeepPending(ctx, pendingOf(t, "fp-1", "i-1", received), caps)
	require.NoError(t, err)
	require.True(t, kept)

	again := pendingOf(t, "fp-1", "i-1", received.Add(time.Hour))
	again.Document = []byte(`{"n":2}`)
	kept, err = store.KeepPending(ctx, again, caps)
	require.NoError(t, err)
	require.True(t, kept)

	pending, err := store.PendingReports(ctx, "fp-1")
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.JSONEq(t, `{"n":1}`, string(pending[0].Document))
	require.True(t, received.Equal(pending[0].ReceivedAt))
	require.True(t, day(t, "2026-10-06").Equal(pending[0].Day))
}

func Test_PendingFingerprintsNowKnown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	issuer := cptest.NewIssuer(t)
	entry := issuer.Entry("lic-1", "cust-1", "Acme")
	known := telemetry.KeyFingerprint(entry.Encoded)

	for _, p := range []*cpstore.PendingReport{
		pendingOf(t, known, "i-1", received),
		pendingOf(t, known, "i-2", received),
		pendingOf(t, "unknown", "i-1", received),
	} {
		kept, err := store.KeepPending(ctx, p, caps)
		require.NoError(t, err)
		require.True(t, kept)
	}

	got, err := store.PendingFingerprintsNowKnown(ctx)
	require.NoError(t, err)
	require.Empty(t, got)

	require.True(t, add(t, store, issuer, &entry))
	got, err = store.PendingFingerprintsNowKnown(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{known}, got)
}

func Test_PurgePending_RemovesWhatIsOlder(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	pool := cptest.NewDatabase(t)
	store := cpstore.New(pool)
	limit := received.Add(-time.Hour)

	for instance, at := range map[string]time.Time{
		"older":   limit.Add(-time.Second),
		"at":      limit,
		"younger": limit.Add(time.Second),
	} {
		kept, err := store.KeepPending(ctx, pendingOf(t, "fp-1", instance, at), caps)
		require.NoError(t, err)
		require.True(t, kept)
	}

	purged, err := store.PurgePending(ctx, limit)
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)

	pending, err := store.PendingReports(ctx, "fp-1")
	require.NoError(t, err)
	instances := make([]string, 0, len(pending))
	for i := range pending {
		instances = append(instances, pending[i].InstanceID)
	}
	require.ElementsMatch(t, []string{"at", "younger"}, instances)
}
