package cpstore_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// today is the instant the console tests judge dates at.
var today = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// daysAgo is the day n days before the one of today.
func daysAgo(n int) time.Time {
	return time.Date(today.Year(), today.Month(), today.Day()-n, 0, 0, 0, 0, time.UTC)
}

// seeded is a store with what the console tests put in it.
type seeded struct {
	t      *testing.T
	store  *cpstore.Store
	pool   *pgxpool.Pool
	issuer *cptest.Issuer
}

func newSeeded(t *testing.T) *seeded {
	t.Helper()
	pool := cptest.NewDatabase(t)
	return &seeded{t: t, store: cpstore.New(pool), pool: pool, issuer: cptest.NewIssuer(t)}
}

// license records a license expiring at expiresAt. The key says nothing of its telemetry, of its
// grace period or of its plan unless mutate does.
func (s *seeded) license(
	id, customerID, issuedTo string, expiresAt time.Time, mutate ...func(key *license.Key),
) license.RegistryEntry {
	s.t.Helper()
	key := &license.Key{
		Version:    "v1",
		Id:         id,
		IssuedTo:   issuedTo,
		CustomerId: customerID,
		IssuedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:  expiresAt,
	}
	for _, change := range mutate {
		change(key)
	}
	entry := s.issuer.EntryOf(key)
	cptest.AddLicense(s.t, s.store, s.issuer, &entry)
	return entry
}

// report stores the report of an instance for the day n days before today.
func (s *seeded) report(entry *license.RegistryEntry, instanceID string, n int) {
	s.t.Helper()
	cptest.StoreReport(s.t, s.store, entry, instanceID, daysAgo(n), daysAgo(n).Add(26*time.Hour))
}

func (s *seeded) exec(sql string, args ...any) {
	s.t.Helper()
	_, err := s.pool.Exec(s.t.Context(), sql, args...)
	require.NoError(s.t, err)
}

func (s *seeded) customerID(externalID string) uuid.UUID {
	s.t.Helper()
	var id uuid.UUID
	require.NoError(s.t, s.pool.QueryRow(s.t.Context(),
		`SELECT id FROM controlplane.customers WHERE external_id = $1`, externalID).Scan(&id))
	return id
}

func withTelemetry(mode license.TelemetryMode) func(key *license.Key) {
	return func(key *license.Key) { key.Telemetry = string(mode) }
}

func withGrace(days int) func(key *license.Key) {
	return func(key *license.Key) { key.GraceDays = &days }
}

func Test_Customers_SummarizesEachCustomer_OrderedByName(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	lapsed := today.AddDate(-1, 0, 0)
	inAHundredDays := today.AddDate(0, 0, 100)
	inAYear := today.AddDate(1, 0, 0)

	zeta := s.license("lic-z", "cust-z", "Zeta", lapsed)
	s.report(&zeta, "inst-gone", cpstore.RecentInstanceDays+1)

	old := s.license("lic-a-old", "cust-a", "Acme", lapsed)
	next := s.license("lic-a-next", "cust-a", "Acme", inAHundredDays)
	s.license("lic-a-later", "cust-a", "Acme", inAYear)
	s.report(&old, "inst-1", cpstore.RecentInstanceDays+1)
	s.report(&next, "inst-1", cpstore.RecentInstanceDays)
	s.report(&next, "inst-2", 0)

	customers, err := s.store.Customers(t.Context(), today)
	require.NoError(t, err)
	require.Len(t, customers, 2)

	acme := customers[0]
	require.Equal(t, s.customerID("cust-a"), acme.ID)
	require.Equal(t, "cust-a", acme.ExternalID)
	require.Equal(t, "Acme", acme.Name)
	require.Equal(t, 3, acme.Licenses)
	require.Equal(t, 2, acme.RecentInstances, "an instance last seen more than 30 days ago does not count")
	require.True(t, inAHundredDays.Equal(acme.NearestExpiry), "the next expiry to come, not one already past")

	require.Equal(t, "Zeta", customers[1].Name)
	require.Equal(t, 1, customers[1].Licenses)
	require.Zero(t, customers[1].RecentInstances)
	require.True(t, lapsed.Equal(customers[1].NearestExpiry), "the last expiry when every license has expired")
}

func Test_Customer_Unknown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))

	_, err := s.store.Customer(t.Context(), uuid.New(), today)

	require.ErrorIs(t, err, cpstore.ErrNotFound)
}

func Test_Customer_GivesItsLicensesAndItsInstances(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	inAYear := today.AddDate(1, 0, 0)
	inTenDays := today.AddDate(0, 0, 10)
	former := s.license("lic-former", "cust-1", "Acme", inTenDays,
		withTelemetry(license.TelemetryNone), func(key *license.Key) { key.Plan = "team" })
	current := s.license("lic-current", "cust-1", "Acme", inAYear)
	cptest.Succeed(t, s.pool, "lic-current", "lic-former")
	s.report(&former, "inst-1", 12)
	s.report(&current, "inst-1", 1)
	s.exec(`UPDATE controlplane.customers SET note = 'a note' WHERE external_id = 'cust-1'`)

	other := s.license("lic-other", "cust-2", "Other", inAYear)
	s.report(&other, "inst-9", 1)

	customer, err := s.store.Customer(t.Context(), s.customerID("cust-1"), today)
	require.NoError(t, err)

	require.Equal(t, s.customerID("cust-1"), customer.ID)
	require.Equal(t, "cust-1", customer.ExternalID)
	require.Equal(t, "Acme", customer.Name)
	require.Equal(t, "a note", customer.Note)
	require.False(t, customer.CreatedAt.IsZero())

	require.Len(t, customer.Licenses, 2)
	require.Equal(t, cpstore.LicenseSummary{
		ID:           "lic-current",
		CustomerID:   customer.ID,
		CustomerName: "Acme",
		Telemetry:    license.TelemetryOnline,
		ExpiresAt:    customer.Licenses[0].ExpiresAt,
		State:        license.StateValid,
	}, customer.Licenses[0], "a key that says nothing of its telemetry is online")
	require.True(t, inAYear.Equal(customer.Licenses[0].ExpiresAt))
	require.Equal(t, cpstore.LicenseSummary{
		ID:           "lic-former",
		CustomerID:   customer.ID,
		CustomerName: "Acme",
		Plan:         "team",
		Telemetry:    license.TelemetryNone,
		ExpiresAt:    customer.Licenses[1].ExpiresAt,
		State:        license.StateExpiring,
		HasSuccessor: true,
	}, customer.Licenses[1])

	require.Len(t, customer.Instances, 2, "the instances of another customer are not its own")
	require.Equal(t, "lic-current", customer.Instances[0].LicenseID)
	require.Equal(t, "inst-1", customer.Instances[0].InstanceID)
	require.True(t, daysAgo(1).Equal(customer.Instances[0].LastReportDay))
	require.Equal(t, "v0.3.0", customer.Instances[0].HusonymVersion)
	require.Equal(t, "helm", customer.Instances[0].InstallKind)
	require.Equal(t, "lic-former", customer.Instances[1].LicenseID)
}

func Test_LicenseDetail_Unknown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)

	_, err := s.store.LicenseDetail(t.Context(), "lic-none", today)

	require.ErrorIs(t, err, cpstore.ErrNotFound)
}

func Test_LicenseDetail_GivesWhatIsStored_NeverTheKey(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	maxSources := 4
	expiresAt := today.AddDate(0, 0, -2)
	s.license("lic-0", "cust-1", "Acme", today.AddDate(-1, 0, 0))
	entry := s.license("lic-1", "cust-1", "Acme", expiresAt, withGrace(5), withTelemetry(license.TelemetryOfflineReport),
		func(key *license.Key) {
			key.Plan = "team"
			key.Features = []string{}
			key.Limits = &license.Limits{MaxSources: &maxSources}
		})
	s.license("lic-2", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.license("lic-3", "cust-1", "Acme", today.AddDate(1, 0, 0))
	cptest.Succeed(t, s.pool, "lic-1", "lic-0")
	cptest.Succeed(t, s.pool, "lic-2", "lic-1")
	cptest.Succeed(t, s.pool, "lic-3", "lic-1")
	s.exec(`UPDATE controlplane.licenses SET note = 'renewed by hand' WHERE id = 'lic-1'`)
	s.report(&entry, "inst-1", 2)
	s.report(&entry, "inst-2", 1)

	lastWeek := daysAgo(cpstore.SealRejectionDays - 1)
	require.NoError(t, s.store.CountSealRejection(t.Context(), "lic-1", daysAgo(cpstore.SealRejectionDays)))
	require.NoError(t, s.store.CountSealRejection(t.Context(), "lic-1", lastWeek))
	require.NoError(t, s.store.CountSealRejection(t.Context(), "lic-1", today))
	require.NoError(t, s.store.CountSealRejection(t.Context(), "lic-1", today))
	require.NoError(t, s.store.CountSealRejection(t.Context(), "lic-2", today))

	detail, err := s.store.LicenseDetail(t.Context(), "lic-1", today)
	require.NoError(t, err)

	require.Equal(t, "lic-1", detail.ID)
	require.Equal(t, s.customerID("cust-1"), detail.CustomerID)
	require.Equal(t, "Acme", detail.CustomerName)
	require.NotEmpty(t, detail.KeyFingerprint)
	require.Equal(t, entry.Kid, detail.Kid)
	require.Equal(t, "team", detail.Plan)
	require.NotNil(t, detail.Features, "a key that allows no feature is not one that lists none")
	require.Empty(t, detail.Features)
	require.Equal(t, &license.Limits{MaxSources: &maxSources}, detail.Limits)
	require.Equal(t, license.TelemetryOfflineReport, detail.Telemetry)
	require.Equal(t, "offline_report", detail.StoredTelemetry)
	require.True(t, expiresAt.Equal(detail.ExpiresAt))
	require.False(t, detail.IssuedAt.IsZero())
	require.NotNil(t, detail.GraceDays)
	require.Equal(t, 5, *detail.GraceDays)
	require.Equal(t, entry.KeyFingerprint, detail.SigningKeyFingerprint)
	require.Equal(t, "renewed by hand", detail.Note)
	require.Equal(t, "registry", detail.Origin)
	require.False(t, detail.CreatedAt.IsZero())
	require.Equal(t, license.StateGrace, detail.State)
	require.Equal(t, "lic-0", detail.PredecessorID)
	require.Equal(t, []string{"lic-2", "lic-3"}, detail.SuccessorIDs)
	require.True(t, detail.HasSuccessor)

	require.Len(t, detail.Instances, 2)
	require.Equal(t, "inst-2", detail.Instances[0].InstanceID, "the instance heard of last comes first")

	require.Len(t, detail.SealRejections, 2, "neither the eighth day back nor another license")
	require.Equal(t, 2, detail.SealRejections[0].Count)
	require.True(t, daysAgo(0).Equal(detail.SealRejections[0].Day))
	require.Equal(t, 1, detail.SealRejections[1].Count)
	require.True(t, lastWeek.Equal(detail.SealRejections[1].Day))

	shown, err := json.Marshal(detail)
	require.NoError(t, err)
	require.NotContains(t, string(shown), entry.Encoded)

	frozen, err := s.store.LicenseDetail(t.Context(), "lic-1", expiresAt.AddDate(0, 0, 5))
	require.NoError(t, err)
	require.Equal(t, license.StateFrozen, frozen.State)
}

func Test_LicenseDetail_ALicenseAlone(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))

	detail, err := s.store.LicenseDetail(t.Context(), "lic-1", today)
	require.NoError(t, err)

	require.Nil(t, detail.Features)
	require.Nil(t, detail.Limits)
	require.Nil(t, detail.GraceDays)
	require.Empty(t, detail.StoredTelemetry)
	require.Equal(t, license.TelemetryOnline, detail.Telemetry)
	require.Empty(t, detail.PredecessorID)
	require.Empty(t, detail.SuccessorIDs)
	require.False(t, detail.HasSuccessor)
	require.Empty(t, detail.Instances)
	require.Empty(t, detail.SealRejections)
}

func Test_Instance_Unknown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.license("lic-2", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.report(&entry, "inst-1", 1)

	_, err := s.store.Instance(t.Context(), "lic-1", "inst-none")
	require.ErrorIs(t, err, cpstore.ErrNotFound)

	_, err = s.store.Instance(t.Context(), "lic-2", "inst-1")
	require.ErrorIs(t, err, cpstore.ErrNotFound, "an instance is known under its license only")
}

func Test_Instance_GivesItsReports_NewestFirst(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	maxSources := 4
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0), func(key *license.Key) {
		key.Limits = &license.Limits{MaxSources: &maxSources}
	})
	s.report(&entry, "inst-1", 3)
	s.report(&entry, "inst-1", 1)
	s.report(&entry, "inst-2", 0)
	// Reports whose document tells no number of sources: one that is not JSON, one that is JSON
	// of another shape.
	s.exec(`INSERT INTO controlplane.usage_reports (license_id, instance_id, day, document, seal, received_at, conflicts)
		VALUES ('lic-1', 'inst-1', $1, 'not json', 'seal', $3, 2), ('lic-1', 'inst-1', $2, '{"sources":"many"}', 'seal', $3, 0)`,
		daysAgo(2), daysAgo(4), today)

	instance, err := s.store.Instance(t.Context(), "lic-1", "inst-1")
	require.NoError(t, err)

	require.Equal(t, "lic-1", instance.LicenseID)
	require.Equal(t, "inst-1", instance.InstanceID)
	require.Equal(t, s.customerID("cust-1"), instance.CustomerID)
	require.Equal(t, "Acme", instance.CustomerName)
	require.True(t, daysAgo(1).Equal(instance.LastReportDay))
	require.True(t, daysAgo(3).Add(26*time.Hour).Equal(instance.FirstSeenAt))
	require.True(t, daysAgo(1).Add(26*time.Hour).Equal(instance.LastSeenAt))
	require.Equal(t, "v0.3.0", instance.HusonymVersion)
	require.Equal(t, "helm", instance.InstallKind)
	require.Equal(t, &license.Limits{MaxSources: &maxSources}, instance.Limits)

	require.Len(t, instance.Reports, 4, "the reports of another instance are not its own")
	three := 3
	require.Equal(t, cpstore.ReportSummary{
		Day: instance.Reports[0].Day, ReceivedAt: instance.Reports[0].ReceivedAt, Sources: &three,
	}, instance.Reports[0])
	require.True(t, daysAgo(1).Equal(instance.Reports[0].Day))
	require.True(t, daysAgo(1).Add(26*time.Hour).Equal(instance.Reports[0].ReceivedAt))
	require.True(t, daysAgo(2).Equal(instance.Reports[1].Day))
	require.Equal(t, 2, instance.Reports[1].Conflicts)
	require.Nil(t, instance.Reports[1].Sources, "a document that does not parse tells no number")
	require.True(t, daysAgo(3).Equal(instance.Reports[2].Day))
	require.Equal(t, &three, instance.Reports[2].Sources)
	require.True(t, daysAgo(4).Equal(instance.Reports[3].Day))
	require.Nil(t, instance.Reports[3].Sources)
}

func Test_Instance_ListsNoMoreReportsThanTheCap(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.report(&entry, "inst-1", 0)
	s.exec(`INSERT INTO controlplane.usage_reports (license_id, instance_id, day, document, seal, received_at)
		SELECT 'lic-1', 'inst-1', $1::date - n, '{}', 'seal', $2 FROM generate_series(1, $3::int) AS n`,
		daysAgo(0), today, cpstore.InstanceReportsCap)

	instance, err := s.store.Instance(t.Context(), "lic-1", "inst-1")
	require.NoError(t, err)

	require.Len(t, instance.Reports, cpstore.InstanceReportsCap)
	require.True(t, daysAgo(0).Equal(instance.Reports[0].Day))
	last := instance.Reports[cpstore.InstanceReportsCap-1]
	require.True(t, daysAgo(cpstore.InstanceReportsCap-1).Equal(last.Day), "the oldest one is the one left out")
}

func Test_Report_Unknown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.report(&entry, "inst-1", 1)

	_, err := s.store.Report(t.Context(), "lic-1", "inst-1", daysAgo(2))

	require.ErrorIs(t, err, cpstore.ErrNotFound)
}

func Test_Report_GivesWhatWasReceived(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	entry := s.license("lic-1", "cust-1", "Acme", today.AddDate(1, 0, 0))
	s.report(&entry, "inst-1", 1)
	sealed := cptest.ReportFor(t, &entry, "inst-1", daysAgo(1))

	// Any instant of the day names the day.
	report, err := s.store.Report(t.Context(), "lic-1", "inst-1", daysAgo(1).Add(13*time.Hour))
	require.NoError(t, err)

	require.Equal(t, "lic-1", report.LicenseID)
	require.Equal(t, "inst-1", report.InstanceID)
	require.Equal(t, s.customerID("cust-1"), report.CustomerID)
	require.Equal(t, "Acme", report.CustomerName)
	require.True(t, daysAgo(1).Equal(report.Day))
	require.Equal(t, sealed.Document, report.Document)
	require.Equal(t, sealed.Seal, report.Seal)
	require.True(t, daysAgo(1).Add(26*time.Hour).Equal(report.ReceivedAt))
	require.Zero(t, report.Conflicts)
	require.True(t, report.LastConflictAt.IsZero())

	outcome, err := s.store.StoreReport(t.Context(), &cpstore.Report{
		InstanceID: "inst-1", Day: daysAgo(1), LicenseID: "lic-1", Document: []byte(`{"other":1}`), Seal: "seal",
		HusonymVersion: "v0.3.0", ReceivedAt: today,
	}, cpstore.InstanceCap{Max: 10})
	require.NoError(t, err)
	require.Equal(t, cpstore.ReportConflict, outcome)

	report, err = s.store.Report(t.Context(), "lic-1", "inst-1", daysAgo(1))
	require.NoError(t, err)
	require.Equal(t, 1, report.Conflicts)
	require.True(t, today.Equal(report.LastConflictAt))
	require.Equal(t, sealed.Document, report.Document, "the first document received stays")
}

func Test_PendingByFingerprint_GroupsWhatIsPending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)
	wide := cpstore.PendingCaps{PerFingerprint: 10, Total: 100}
	keep := func(fingerprint, instance, seal string, at time.Time) {
		t.Helper()
		pending := pendingOf(t, fingerprint, instance, at)
		pending.Seal = seal
		kept, err := s.store.KeepPending(t.Context(), pending, wide)
		require.NoError(t, err)
		require.True(t, kept)
	}
	exactly := today.Add(-cpstore.OldPendingAfter)
	keep("fp-young", "inst-1", "seal", exactly)
	keep("fp-young", "inst-2", "seal", today.Add(-time.Hour))
	keep("fp-old", "inst-b", "seal", exactly.Add(-time.Second))
	keep("fp-old", "inst-a", "seal", today.Add(-48*time.Hour))
	keep("fp-old", "inst-a", "other seal", today.Add(-time.Minute))

	groups, err := s.store.PendingByFingerprint(t.Context(), today)
	require.NoError(t, err)

	require.Len(t, groups, 2)
	require.Equal(t, "fp-old", groups[0].KeyFingerprint, "the group waiting for the longest comes first")
	require.Equal(t, 3, groups[0].Reports)
	require.Equal(t, 2, groups[0].OldReports)
	require.True(t, today.Add(-48*time.Hour).Equal(groups[0].Oldest))
	require.True(t, today.Add(-time.Minute).Equal(groups[0].Newest))
	require.Equal(t, []string{"inst-a", "inst-b"}, groups[0].InstanceIDs)

	require.Equal(t, "fp-young", groups[1].KeyFingerprint)
	require.Equal(t, 2, groups[1].Reports)
	require.Zero(t, groups[1].OldReports, "24 hours exactly is not more than 24 hours")
	require.Equal(t, []string{"inst-1", "inst-2"}, groups[1].InstanceIDs)
}

func Test_PendingByFingerprint_NothingPending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	s := newSeeded(t)

	groups, err := s.store.PendingByFingerprint(t.Context(), today)

	require.NoError(t, err)
	require.Empty(t, groups)
}
