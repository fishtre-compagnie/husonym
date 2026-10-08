package cpstore_test

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	cpdb "github.com/fishtre-compagnie/husonym/controlplane/gen/db"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func (s *seeded) attention() *cpstore.Attention {
	s.t.Helper()
	attention, err := s.store.Attention(s.t.Context(), today)
	require.NoError(s.t, err)
	return attention
}

func (s *seeded) keepPending(fingerprint, instanceID string, at time.Time) {
	s.t.Helper()
	kept, err := s.store.KeepPending(s.t.Context(), pendingOf(s.t, fingerprint, instanceID, at),
		cpstore.PendingCaps{PerFingerprint: 10, Total: 100})
	require.NoError(s.t, err)
	require.True(s.t, kept)
}

func (s *seeded) rejectSeal(licenseID string, at time.Time, times int) {
	s.t.Helper()
	for range times {
		require.NoError(s.t, s.store.CountSealRejection(s.t.Context(), licenseID, at))
	}
}

func Test_Attention_NothingStored(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)

	attention := s.attention()
	require.Empty(t, attention.SilentInstances)
	require.Empty(t, attention.ExpiringLicenses)
	require.Empty(t, attention.OldPending)
	require.Empty(t, attention.SealRejections)
	require.Empty(t, attention.SharedLicenses)

	counts, err := s.store.AttentionCounts(t.Context(), today)
	require.NoError(t, err)
	require.Equal(t, cpstore.AttentionCounts{}, counts)
}

func Test_Attention_SilentInstances(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	inAYear := today.AddDate(1, 0, 0)

	online := s.license("lic-online", "cust-1", "Acme", inAYear)
	s.report(&online, "inst-today", 0)
	s.report(&online, "inst-3", cpstore.SilentAfterDays)
	s.report(&online, "inst-4", cpstore.SilentAfterDays+1)
	s.report(&online, "inst-30", cpstore.RecentInstanceDays)
	s.report(&online, "inst-31", cpstore.RecentInstanceDays+1)

	// In force until the grace period runs out.
	inGrace := s.license("lic-grace", "cust-1", "Acme", today.AddDate(0, 0, -5))
	s.report(&inGrace, "inst-10", 10)
	// A mode this binary does not know is online.
	unknown := s.license("lic-unknown", "cust-1", "Acme", inAYear, withTelemetry("later"))
	s.report(&unknown, "inst-10", 10)

	offline := s.license("lic-offline", "cust-2", "Other", inAYear, withTelemetry(license.TelemetryOfflineReport))
	s.report(&offline, "inst-10", 10)
	none := s.license("lic-none", "cust-2", "Other", inAYear, withTelemetry(license.TelemetryNone))
	s.report(&none, "inst-10", 10)
	frozen := s.license("lic-frozen", "cust-2", "Other", today.AddDate(0, 0, -40))
	s.report(&frozen, "inst-10", 10)
	noGrace := s.license("lic-no-grace", "cust-2", "Other", today.Add(-time.Hour), withGrace(0))
	s.report(&noGrace, "inst-10", 10)

	silent := s.attention().SilentInstances

	type found struct{ license, instance string }
	got := make([]found, 0, len(silent))
	for i := range silent {
		got = append(got, found{silent[i].LicenseID, silent[i].InstanceID})
	}
	require.Equal(t, []found{
		{"lic-online", "inst-30"},
		{"lic-grace", "inst-10"},
		{"lic-unknown", "inst-10"},
		{"lic-online", "inst-4"},
	}, got, "the instance silent for the longest comes first")
	require.Equal(t, s.customerID("cust-1"), silent[0].CustomerID)
	require.Equal(t, "Acme", silent[0].CustomerName)
	require.True(t, daysAgo(cpstore.RecentInstanceDays).Equal(silent[0].LastReportDay))
	require.Equal(t, "v0.3.0", silent[0].HusonymVersion)
}

func Test_Attention_SilentInstances_TheDayCountsNotTheHour(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.report(&entry, "inst-1", cpstore.SilentAfterDays)

	for _, at := range []time.Time{daysAgo(0), daysAgo(0).Add(24*time.Hour - time.Second)} {
		attention, err := s.store.Attention(t.Context(), at)
		require.NoError(t, err)
		require.Empty(t, attention.SilentInstances, "at %s", at)
	}
	attention, err := s.store.Attention(t.Context(), daysAgo(0).Add(24*time.Hour))
	require.NoError(t, err)
	require.Len(t, attention.SilentInstances, 1)
}

func Test_Attention_ExpiringLicenses(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	inTenDays := today.AddDate(0, 0, 10)

	s.license("lic-within", "cust-1", "Acme", today.Add(cpstore.ExpiringWithin-time.Second))
	s.license("lic-at-30-days", "cust-1", "Acme", today.Add(cpstore.ExpiringWithin))
	s.license("lic-far", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.license("lic-grace", "cust-1", "Acme", today.AddDate(0, 0, -5), func(key *license.Key) { key.Plan = "team" })
	s.license("lic-frozen", "cust-1", "Acme", today.AddDate(0, 0, -40))
	s.license("lic-no-grace", "cust-1", "Acme", today.Add(-time.Hour), withGrace(0))
	s.license("lic-long-grace", "cust-1", "Acme", today.AddDate(0, 0, -40), withGrace(60))
	s.license("lic-soon", "cust-2", "Other", inTenDays)
	s.license("lic-renewed", "cust-2", "Other", inTenDays)
	s.license("lic-renewal", "cust-2", "Other", today.AddDate(1, 0, 10))
	cptest.Succeed(t, s.pool, "lic-renewal", "lic-renewed")

	expiring := s.attention().ExpiringLicenses

	ids := make([]string, 0, len(expiring))
	for i := range expiring {
		ids = append(ids, expiring[i].ID)
	}
	require.Equal(t, []string{"lic-long-grace", "lic-grace", "lic-soon", "lic-within"}, ids,
		"the license expiring first comes first")
	require.Equal(t, cpstore.LicenseSummary{
		ID:           "lic-grace",
		CustomerID:   s.customerID("cust-1"),
		CustomerName: "Acme",
		Plan:         "team",
		Telemetry:    license.TelemetryOnline,
		ExpiresAt:    expiring[1].ExpiresAt,
		State:        license.StateGrace,
	}, expiring[1])
	require.True(t, today.AddDate(0, 0, -5).Equal(expiring[1].ExpiresAt))
	require.Equal(t, license.StateExpiring, expiring[2].State)
}

// The query is asserted itself: the list would be the same if the frozen licenses were all
// loaded and dropped afterwards.
func Test_ListExpiringCandidates_LoadsNoLicenseWhoseGraceHasRunOut(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	graceEnd := today.AddDate(0, 0, -license.DefaultGraceDays)

	s.license("lic-frozen-long-ago", "cust-1", "Acme", today.AddDate(0, 0, -400))
	s.license("lic-frozen-yesterday", "cust-1", "Acme", graceEnd.AddDate(0, 0, -1))
	s.license("lic-frozen-now", "cust-1", "Acme", graceEnd)
	s.license("lic-last-second", "cust-1", "Acme", graceEnd.Add(time.Second))
	s.license("lic-long-grace", "cust-1", "Acme", today.AddDate(0, 0, -40), withGrace(60))
	s.license("lic-long-grace-over", "cust-1", "Acme", today.AddDate(0, 0, -61), withGrace(60))
	s.license("lic-no-grace", "cust-1", "Acme", today.Add(-time.Hour), withGrace(0))
	// A negative grace is no grace, as the key reads it.
	s.license("lic-negative-grace", "cust-1", "Acme", today.Add(-time.Hour))
	s.license("lic-negative-grace-soon", "cust-1", "Acme", today.AddDate(0, 0, 9))
	s.exec(`UPDATE controlplane.licenses SET grace_days = -5 WHERE id LIKE 'lic-negative-grace%'`)
	s.license("lic-soon", "cust-1", "Acme", today.AddDate(0, 0, 10))
	s.license("lic-far", "cust-1", "Acme", today.AddDate(1, 0, 0))
	// In the last second of its grace period on the first of April, see below.
	s.license("lic-spring", "cust-1", "Acme", time.Date(2027, 3, 18, 12, 0, 1, 0, time.UTC))

	// One connection, in a time zone whose days are not all 24 hours long: see below.
	conn, err := s.pool.Acquire(t.Context())
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(t.Context(), `SET TIME ZONE 'Europe/Paris'`)
	require.NoError(t, err)
	loadedAt := func(now time.Time) []string {
		t.Helper()
		rows, err := cpdb.New(conn).ListExpiringCandidates(t.Context(), cpdb.ListExpiringCandidatesParams{
			ExpiresBefore:    pgtype.Timestamptz{Time: now.Add(cpstore.ExpiringWithin), Valid: true},
			DefaultGraceDays: license.DefaultGraceDays,
			Now:              pgtype.Timestamptz{Time: now, Valid: true},
		})
		require.NoError(t, err)
		loaded := make([]string, 0, len(rows))
		for i := range rows {
			loaded = append(loaded, rows[i].ID)
		}
		return loaded
	}

	want := []string{"lic-long-grace", "lic-last-second", "lic-negative-grace-soon", "lic-soon"}
	require.Equal(t, want, loadedAt(today), "what froze is not loaded; what expires first comes first")

	expiring := s.attention().ExpiringLicenses
	listed := make([]string, 0, len(expiring))
	for i := range expiring {
		listed = append(listed, expiring[i].ID)
	}
	require.Equal(t, want, listed, "the bound of the query is the one the key is judged by")

	// The 14 days before spring are an hour short in the time zone of the session: counted in days
	// of that zone, the grace period would end an hour early and the license be left out.
	spring := time.Date(2027, 4, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, []string{"lic-spring"}, loadedAt(spring))
	attention, err := s.store.Attention(t.Context(), spring)
	require.NoError(t, err)
	require.Len(t, attention.ExpiringLicenses, 1)
	require.Equal(t, license.StateGrace, attention.ExpiringLicenses[0].State)
}

func Test_Attention_OldPending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	exactly := today.Add(-cpstore.OldPendingAfter)
	s.keepPending("fp-at-24h", "inst-1", exactly)
	s.keepPending("fp-young", "inst-1", today.Add(-time.Hour))
	s.keepPending("fp-old", "inst-1", exactly.Add(-time.Second))
	s.keepPending("fp-old", "inst-2", today.Add(-time.Hour))

	old := s.attention().OldPending

	require.Len(t, old, 1, "24 hours exactly is not more than 24 hours")
	require.Equal(t, "fp-old", old[0].KeyFingerprint)
	require.Equal(t, 2, old[0].Reports)
	require.Equal(t, 1, old[0].OldReports)
	require.True(t, exactly.Add(-time.Second).Equal(old[0].Oldest))
	require.Equal(t, []string{"inst-1", "inst-2"}, old[0].InstanceIDs)

	counts, err := s.store.AttentionCounts(t.Context(), today)
	require.NoError(t, err)
	require.Equal(t, 1, counts.OldPending, "the reports are counted, not the fingerprints")
}

func Test_Attention_SealRejections(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.license("lic-2", "cust-2", "Other", today.AddDate(1, 0, 0))
	s.rejectSeal("lic-1", daysAgo(cpstore.SealRejectionDays), 4)
	s.rejectSeal("lic-1", daysAgo(cpstore.SealRejectionDays-1), 1)
	s.rejectSeal("lic-1", today.Add(-time.Hour), 2)
	s.rejectSeal("lic-2", today, 3)

	rejections := s.attention().SealRejections

	require.Len(t, rejections, 3, "the eighth day back is no longer listed")
	require.Equal(t, cpstore.SealRejection{
		LicenseID:    "lic-2",
		CustomerID:   s.customerID("cust-2"),
		CustomerName: "Other",
		Day:          daysAgo(0),
		Count:        3,
		LastAt:       today,
	}, rejections[0], "the day last refused comes first")
	require.Equal(t, "lic-1", rejections[1].LicenseID)
	require.Equal(t, 2, rejections[1].Count)
	require.Equal(t, "lic-1", rejections[2].LicenseID)
	require.True(t, daysAgo(cpstore.SealRejectionDays-1).Equal(rejections[2].Day))

	counts, err := s.store.AttentionCounts(t.Context(), today)
	require.NoError(t, err)
	require.Equal(t, 5, counts.SealRejections, "what was refused on the current UTC day, whatever the license")
}

// Half past one in the morning of the 9th, two hours east of UTC, is half past eleven in the
// evening of the 8th in UTC: the day that counts is the 8th.
func Test_Attention_SealRejections_TheDayIsTheUTCOneWhateverTheZoneOfNow(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	now := time.Date(2026, 10, 9, 1, 30, 0, 0, time.FixedZone("two hours east", 2*60*60))
	utcDay := func(day int) time.Time { return time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC) }
	s.rejectSeal("lic-1", utcDay(1).Add(12*time.Hour), 1)
	s.rejectSeal("lic-1", utcDay(2).Add(time.Minute), 3)
	s.rejectSeal("lic-1", utcDay(8).Add(22*time.Hour), 2)

	counts, err := s.store.AttentionCounts(t.Context(), now)
	require.NoError(t, err)
	require.Equal(t, 2, counts.SealRejections, "what was refused on the UTC day of now, not on the day its own zone says")

	attention, err := s.store.Attention(t.Context(), now)
	require.NoError(t, err)
	days := make([]time.Time, 0, len(attention.SealRejections))
	for i := range attention.SealRejections {
		days = append(days, attention.SealRejections[i].Day)
	}
	require.Equal(t, []time.Time{utcDay(8), utcDay(2)}, days, "seven UTC days, the one of now included")

	detail, err := s.store.LicenseDetail(t.Context(), "lic-1", now)
	require.NoError(t, err)
	require.Len(t, detail.SealRejections, 2, "the page of the license counts the same days")
}

func Test_Attention_SharedLicenses(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	inAYear := today.AddDate(1, 0, 0)

	shared := s.license("lic-shared", "cust-1", "Acme", inAYear)
	s.report(&shared, "inst-a", cpstore.RecentInstanceDays)
	s.report(&shared, "inst-b", 0)
	s.report(&shared, "inst-c", cpstore.RecentInstanceDays+1)

	oneGone := s.license("lic-one-gone", "cust-1", "Acme", inAYear)
	s.report(&oneGone, "inst-a", cpstore.RecentInstanceDays+1)
	s.report(&oneGone, "inst-b", 0)

	// The same instance under two licenses shares neither.
	single := s.license("lic-single", "cust-2", "Other", inAYear)
	s.report(&single, "inst-a", 0)

	sharedLicenses := s.attention().SharedLicenses

	require.Len(t, sharedLicenses, 1)
	require.Equal(t, "lic-shared", sharedLicenses[0].ID)
	require.Equal(t, s.customerID("cust-1"), sharedLicenses[0].CustomerID)
	require.Equal(t, "Acme", sharedLicenses[0].CustomerName)
	require.Equal(t, license.StateValid, sharedLicenses[0].State)
	require.Equal(t, 2, sharedLicenses[0].RecentInstances)
}

func Test_AttentionCounts_AgreeWithTheLists(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	valid := s.license("lic-valid", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.report(&valid, "inst-a", 5)
	s.report(&valid, "inst-b", 6)
	s.report(&valid, "inst-c", 0)
	soon := s.license("lic-soon", "cust-1", "Acme", today.AddDate(0, 0, 10))
	s.report(&soon, "inst-a", 7)
	s.license("lic-grace", "cust-2", "Other", today.AddDate(0, 0, -5))
	frozen := s.license("lic-frozen", "cust-2", "Other", today.AddDate(0, 0, -40))
	s.report(&frozen, "inst-a", 5)
	s.report(&frozen, "inst-b", 5)
	s.keepPending("fp-1", "inst-1", today.Add(-25*time.Hour))
	s.keepPending("fp-1", "inst-2", today.Add(-26*time.Hour))
	s.keepPending("fp-1", "inst-3", today.Add(-time.Hour))
	s.keepPending("fp-2", "inst-1", today.Add(-30*time.Hour))
	s.keepPending("fp-3", "inst-1", today.Add(-time.Hour))
	s.rejectSeal("lic-valid", daysAgo(1), 7)
	s.rejectSeal("lic-valid", today, 2)

	attention := s.attention()
	counts, err := s.store.AttentionCounts(t.Context(), today)
	require.NoError(t, err)

	oldReports := 0
	for i := range attention.OldPending {
		oldReports += attention.OldPending[i].OldReports
	}
	require.Equal(t, cpstore.AttentionCounts{
		SilentInstances:  len(attention.SilentInstances),
		ExpiringLicenses: len(attention.ExpiringLicenses),
		OldPending:       oldReports,
		SealRejections:   2,
		SharedLicenses:   len(attention.SharedLicenses),
	}, counts)
	require.Equal(t, cpstore.AttentionCounts{
		SilentInstances:  3,
		ExpiringLicenses: 2,
		OldPending:       3,
		SealRejections:   2,
		SharedLicenses:   2,
	}, counts)
	require.Len(t, attention.OldPending, 2)
	require.Len(t, attention.SealRejections, 2)
}
