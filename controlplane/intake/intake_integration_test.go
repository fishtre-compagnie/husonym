package intake_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/fishtre-compagnie/husonym/controlplane/cptest"
	"github.com/fishtre-compagnie/husonym/controlplane/intake"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	instanceA = "123e4567-e89b-12d3-a456-426614174000"
	instanceB = "223e4567-e89b-12d3-a456-426614174000"
	instanceC = "323e4567-e89b-12d3-a456-426614174000"
)

var received = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// bench is an intake over a fresh database, with one license minted and not yet known.
type bench struct {
	t           *testing.T
	pool        *pgxpool.Pool
	store       *cpstore.Store
	intake      *intake.Intake
	issuer      *cptest.Issuer
	entry       license.RegistryEntry
	fingerprint string
	// now is what the intake reads the time from.
	now time.Time
}

func newBench(t *testing.T) *bench {
	t.Helper()
	b := &bench{t: t, pool: cptest.NewDatabase(t), issuer: cptest.NewIssuer(t), now: received}
	b.store = cpstore.New(b.pool)
	b.intake = intake.New(b.store, func() time.Time { return b.now })
	b.entry = b.issuer.Entry("lic-1", "cust-1", "Acme")
	b.fingerprint = telemetry.KeyFingerprint(b.entry.Encoded)
	return b
}

// anotherLicense is the same bench seen by the holder of another license, which the store knows.
// The time is the one of the bench it comes from.
func (b *bench) anotherLicense(id string) *bench {
	b.t.Helper()
	other := *b
	other.entry = b.issuer.Entry(id, "cust-"+id, "Other")
	other.fingerprint = telemetry.KeyFingerprint(other.entry.Encoded)
	other.addLicense()
	return &other
}

// newBenchWithLicense is a bench whose license is known to the store.
func newBenchWithLicense(t *testing.T) *bench {
	t.Helper()
	b := newBench(t)
	b.addLicense()
	return b
}

func (b *bench) addLicense() {
	b.t.Helper()
	added, err := b.store.AddLicense(b.t.Context(), b.issuer.Key(&b.entry), &b.entry, "registry")
	require.NoError(b.t, err)
	require.True(b.t, added)
}

// report is the report of an instance for a day under the license of the bench.
func (b *bench) report(instance, day string) *telemetry.Report {
	return &telemetry.Report{
		SchemaVersion: telemetry.SchemaVersion,
		Day:           day,
		GeneratedAt:   "2026-10-07T00:05:12Z",
		Identification: telemetry.Identification{
			KeyFingerprint: b.fingerprint,
			LicenseID:      "0123456789abcdef",
			InstanceID:     instance,
			LicenseState:   "valid",
			DaysToExpiry:   212,
		},
		Version: telemetry.Version{Husonym: "v0.3.0"},
		Sources: telemetry.Sources{Count: 3},
	}
}

func (b *bench) document(report *telemetry.Report) []byte {
	b.t.Helper()
	document, err := report.Marshal()
	require.NoError(b.t, err)
	return document
}

func (b *bench) seal(document []byte) string {
	b.t.Helper()
	seal, err := telemetry.Seal(b.entry.Encoded, document)
	require.NoError(b.t, err)
	return seal
}

// post hands a document to the intake, sealed as the instance would.
func (b *bench) post(document []byte) intake.Outcome {
	b.t.Helper()
	return b.receive(document, b.seal(document), b.fingerprint)
}

func (b *bench) receive(document []byte, seal, fingerprint string) intake.Outcome {
	b.t.Helper()
	outcome, err := b.intake.Receive(b.t.Context(), document, seal, fingerprint)
	require.NoError(b.t, err)
	return outcome
}

func (b *bench) count(table string) int {
	b.t.Helper()
	var n int
	require.NoError(b.t, b.pool.QueryRow(b.t.Context(), `SELECT count(*) FROM controlplane.`+table).Scan(&n))
	return n
}

// withDiagnostics adds to a report the least diagnostics the schema accepts.
func withDiagnostics(report *telemetry.Report, kind string) *telemetry.Report {
	report.Diagnostics = &telemetry.Diagnostics{
		Installation:  telemetry.Installation{Kind: kind, OS: "linux", Arch: "amd64"},
		Configuration: telemetry.Configuration{RunLogs: "loki"},
		Runs:          telemetry.Runs{RowsRead: "lt_10m", RowsDiscarded: "lt_1k"},
	}
	return report
}

func Test_Receive_KnownLicense_Stored(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBenchWithLicense(t)
	document := b.document(withDiagnostics(b.report(instanceA, "2026-10-06"), "helm"))
	seal := b.seal(document)

	require.Equal(t, intake.Stored, b.receive(document, seal, b.fingerprint))

	var gotDocument, gotSeal, licenseID string
	var receivedAt time.Time
	var conflicts int
	require.NoError(t, b.pool.QueryRow(ctx,
		`SELECT document, seal, license_id, received_at, conflicts FROM controlplane.usage_reports
		 WHERE instance_id = $1 AND day = '2026-10-06'`, instanceA,
	).Scan(&gotDocument, &gotSeal, &licenseID, &receivedAt, &conflicts))
	require.Equal(t, string(document), gotDocument) //nolint:testifylint // the exact bytes received, not their meaning
	require.Equal(t, seal, gotSeal)
	require.Equal(t, "lic-1", licenseID)
	require.True(t, received.Equal(receivedAt))
	require.Zero(t, conflicts)

	var firstSeen, lastSeen time.Time
	var lastDay, lastLicense, version string
	var kind *string
	require.NoError(t, b.pool.QueryRow(ctx,
		`SELECT first_seen_at, last_seen_at, last_report_day::text, license_id, husonym_version, install_kind
		 FROM controlplane.instances WHERE instance_id = $1`, instanceA,
	).Scan(&firstSeen, &lastSeen, &lastDay, &lastLicense, &version, &kind))
	require.True(t, received.Equal(firstSeen))
	require.True(t, received.Equal(lastSeen))
	require.Equal(t, "2026-10-06", lastDay)
	require.Equal(t, "lic-1", lastLicense)
	require.Equal(t, "v0.3.0", version)
	require.NotNil(t, kind)
	require.Equal(t, "helm", *kind)
	require.Zero(t, b.count("pending_reports"))
}

func Test_Receive_WithoutDiagnostics_TheInstallationKindIsUnknown(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)

	require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-10-06"))))

	var kindIsNull bool
	require.NoError(t, b.pool.QueryRow(t.Context(),
		`SELECT install_kind IS NULL FROM controlplane.instances WHERE instance_id = $1`, instanceA).Scan(&kindIsNull))
	require.True(t, kindIsNull)
}

func Test_Receive_UnknownLicense_Pending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	document := b.document(b.report(instanceA, "2026-10-06"))
	seal := b.seal(document)

	require.Equal(t, intake.Pending, b.receive(document, seal, b.fingerprint))

	pending, err := b.store.PendingReports(t.Context(), b.fingerprint)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, instanceA, pending[0].InstanceID)
	require.Equal(t, "2026-10-06", pending[0].Day.Format(time.DateOnly))
	require.Equal(t, document, pending[0].Document)
	require.Equal(t, seal, pending[0].Seal)
	require.True(t, received.Equal(pending[0].ReceivedAt))
	require.Zero(t, b.count("usage_reports"))
	require.Zero(t, b.count("instances"))
}

func Test_Receive_AlreadyPending_PendingAndNothingChanges(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	first := b.document(b.report(instanceA, "2026-10-06"))
	seal := b.seal(first)
	require.Equal(t, intake.Pending, b.receive(first, seal, b.fingerprint))

	// The same instance, day and seal again, whatever the body.
	other := b.report(instanceA, "2026-10-06")
	other.Sources.Count = 9
	b.now = received.Add(time.Hour)
	require.Equal(t, intake.Pending, b.receive(first, seal, b.fingerprint))
	require.Equal(t, intake.Pending, b.receive(b.document(other), seal, b.fingerprint))

	pending, err := b.store.PendingReports(t.Context(), b.fingerprint)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, first, pending[0].Document)
	require.True(t, received.Equal(pending[0].ReceivedAt))
}

// Someone who knows a fingerprint and an instance id, but holds no key, posts before the
// instance does: the report of the instance is kept all the same, and it is the one stored.
func Test_Receive_PendingCannotBeSquatted(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBench(t)
	garbage := b.report(instanceA, "2026-10-06")
	garbage.Sources.Count = 999
	forgedSeal := strings.Repeat("0", 64)
	require.Equal(t, intake.Pending, b.receive(b.document(garbage), forgedSeal, b.fingerprint))

	genuine := b.document(b.report(instanceA, "2026-10-06"))
	require.Equal(t, intake.Pending, b.post(genuine))
	require.Equal(t, intake.Pending, b.receive(b.document(garbage), forgedSeal, b.fingerprint))
	require.Equal(t, 2, b.count("pending_reports"))

	b.addLicense()
	stored, discarded, err := b.intake.PromotePending(ctx, b.fingerprint)
	require.NoError(t, err)
	require.Equal(t, 1, stored)
	require.Equal(t, 1, discarded)

	var document string
	var conflicts int
	require.NoError(t, b.pool.QueryRow(ctx,
		`SELECT document, conflicts FROM controlplane.usage_reports`).Scan(&document, &conflicts))
	require.Equal(t, string(genuine), document) //nolint:testifylint // the exact bytes received, not their meaning
	require.Zero(t, conflicts)
	require.Zero(t, b.count("pending_reports"))
	require.Equal(t, 1, b.count("seal_rejections"))
}

// A license sees nothing of what another one reports, even under the same instance id.
func Test_Receive_TwoLicenses_TheSameInstanceIdIsTwoInstances(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	a := newBenchWithLicense(t)
	b := a.anotherLicense("lic-2")

	reportA := withDiagnostics(a.report(instanceA, "2026-10-06"), "helm")
	reportB := withDiagnostics(b.report(instanceA, "2026-10-06"), "compose")
	reportB.Version.Husonym = "v0.9.0"
	documents := map[string][]byte{"lic-1": a.document(reportA), "lic-2": b.document(reportB)}
	require.Equal(t, intake.Stored, a.post(documents["lic-1"]))
	require.Equal(t, intake.Stored, b.post(documents["lic-2"]))
	// A later day under one license leaves the instance of the other as it was.
	laterB := b.report(instanceA, "2026-10-07")
	laterB.Version.Husonym = "v1.0.0"
	require.Equal(t, intake.Stored, b.post(b.document(laterB)))

	for licenseID, want := range map[string]struct{ version, kind, lastDay string }{
		"lic-1": {"v0.3.0", "helm", "2026-10-06"},
		"lic-2": {"v1.0.0", "compose", "2026-10-07"},
	} {
		var document, version, kind, lastDay string
		var conflicts int
		require.NoError(t, a.pool.QueryRow(ctx,
			`SELECT r.document, r.conflicts, i.husonym_version, i.install_kind, i.last_report_day::text
			 FROM controlplane.usage_reports r
			 JOIN controlplane.instances i USING (license_id, instance_id)
			 WHERE r.license_id = $1 AND r.instance_id = $2 AND r.day = '2026-10-06'`, licenseID, instanceA,
		).Scan(&document, &conflicts, &version, &kind, &lastDay))
		require.Equal(t, string(documents[licenseID]), document) //nolint:testifylint // the exact bytes received
		require.Zero(t, conflicts)
		require.Equal(t, want.version, version)
		require.Equal(t, want.kind, kind)
		require.Equal(t, want.lastDay, lastDay)
	}
	require.Equal(t, 2, a.count("instances"))
	require.Equal(t, 3, a.count("usage_reports"))
}

func Test_Receive_DayBounds(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	// The reception is on 2026-10-07, at any hour of that UTC day.
	for name, now := range map[string]time.Time{
		"first second of the day": time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC),
		"last second of the day":  time.Date(2026, 10, 7, 23, 59, 59, 0, time.UTC),
		"told in another zone":    time.Date(2026, 10, 8, 1, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60)),
	} {
		t.Run(name, func(t *testing.T) {
			b := newBenchWithLicense(t)
			b.now = now

			require.Equal(t, intake.Refused, b.post(b.document(b.report(instanceA, "2026-10-09"))), "two days ahead")
			require.Equal(t, intake.Refused, b.post(b.document(b.report(instanceA, "2026-08-07"))), "61 days back")
			require.Zero(t, b.count("usage_reports"))
			require.Zero(t, b.count("instances"))
			require.Zero(t, b.count("pending_reports"))
			require.Zero(t, b.count("seal_rejections"))

			require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-10-08"))), "one day ahead")
			require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-08-08"))), "60 days back")
		})
	}
}

func Test_Receive_InstancesOfALicenseAreCapped(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	_, err := b.pool.Exec(t.Context(),
		`INSERT INTO controlplane.instances
		   (license_id, instance_id, first_seen_at, last_seen_at, last_report_day, husonym_version)
		 SELECT 'lic-1', 'instance-' || n, now(), now(), '2026-10-01', 'v0.3.0' FROM generate_series(1, $1::int) n`,
		intake.InstanceCapPerLicense-1)
	require.NoError(t, err)

	require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-10-05"))), "the last one under the cap")
	require.Equal(t, intake.Refused, b.post(b.document(b.report(instanceB, "2026-10-05"))))
	require.Equal(t, intake.InstanceCapPerLicense, b.count("instances"))
	require.Equal(t, 1, b.count("usage_reports"))
	require.Zero(t, b.count("seal_rejections"))

	require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-10-06"))), "an instance already seen")
	other := b.anotherLicense("lic-2")
	require.Equal(t, intake.Stored, other.post(other.document(other.report(instanceB, "2026-10-05"))), "another license")
}

// The instance posted before its license was recorded, more instances than a license may have.
func Test_PromotePending_InstancesOfALicenseAreCapped(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceA, "2026-10-05"))))
	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceB, "2026-10-05"))))
	b.addLicense()
	_, err := b.pool.Exec(t.Context(),
		`INSERT INTO controlplane.instances
		   (license_id, instance_id, first_seen_at, last_seen_at, last_report_day, husonym_version)
		 SELECT 'lic-1', 'instance-' || n, now(), now(), '2026-10-01', 'v0.3.0' FROM generate_series(1, $1::int) n`,
		intake.InstanceCapPerLicense-1)
	require.NoError(t, err)

	stored, discarded, err := b.intake.PromotePending(t.Context(), b.fingerprint)
	require.NoError(t, err)
	require.Equal(t, 1, stored)
	require.Equal(t, 1, discarded)
	require.Zero(t, b.count("pending_reports"))
	require.Equal(t, 1, b.count("usage_reports"))
	require.Equal(t, intake.InstanceCapPerLicense, b.count("instances"))
	require.Zero(t, b.count("seal_rejections"))
}

// A repeat and a conflict tell nothing new of the instance.
func Test_Receive_RepeatAndConflict_LeaveTheInstanceAsItWas(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	document := b.document(withDiagnostics(b.report(instanceA, "2026-10-06"), "helm"))
	require.Equal(t, intake.Stored, b.post(document))
	instance := func() string {
		var row string
		require.NoError(t, b.pool.QueryRow(t.Context(),
			`SELECT row(i.*)::text FROM controlplane.instances i WHERE instance_id = $1`, instanceA).Scan(&row))
		return row
	}
	before := instance()

	b.now = received.Add(time.Hour)
	require.Equal(t, intake.Repeat, b.post(document))
	require.Equal(t, before, instance())

	other := withDiagnostics(b.report(instanceA, "2026-10-06"), "compose")
	other.Version.Husonym = "v0.9.0"
	b.now = received.Add(2 * time.Hour)
	require.Equal(t, intake.Conflict, b.post(b.document(other)))
	require.Equal(t, before, instance())
}

// The same document is the same bytes: one that only differs by a space is another one.
func Test_Receive_SameJSONInOtherBytes_Conflict(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	document := b.document(b.report(instanceA, "2026-10-06"))
	spaced := []byte(strings.Replace(string(document), `{`, `{ `, 1))
	require.JSONEq(t, string(document), string(spaced))
	require.NoError(t, telemetry.Validate(spaced))

	require.Equal(t, intake.Stored, b.post(document))
	require.Equal(t, intake.Conflict, b.post(spaced))

	var stored string
	require.NoError(t, b.pool.QueryRow(t.Context(), `SELECT document FROM controlplane.usage_reports`).Scan(&stored))
	require.Equal(t, string(document), stored) //nolint:testifylint // the exact bytes received, not their meaning
}

func Test_Receive_SameDocumentAgain_Repeat(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	document := b.document(b.report(instanceA, "2026-10-06"))
	require.Equal(t, intake.Stored, b.post(document))

	b.now = received.Add(time.Hour)
	require.Equal(t, intake.Repeat, b.post(document))

	var conflicts int
	var receivedAt time.Time
	require.NoError(t, b.pool.QueryRow(t.Context(),
		`SELECT conflicts, received_at FROM controlplane.usage_reports`).Scan(&conflicts, &receivedAt))
	require.Zero(t, conflicts)
	require.True(t, received.Equal(receivedAt))
}

// Review Focus 2.
func Test_Receive_DifferentDocumentForTheSameDay_ConflictAndTheFirstStays(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	first := b.document(b.report(instanceA, "2026-10-06"))
	require.Equal(t, intake.Stored, b.post(first))

	other := b.report(instanceA, "2026-10-06")
	other.Sources.Count = 9
	b.now = received.Add(time.Hour)
	require.Equal(t, intake.Conflict, b.post(b.document(other)))
	b.now = received.Add(2 * time.Hour)
	require.Equal(t, intake.Conflict, b.post(b.document(other)))

	var document string
	var conflicts int
	var lastConflictAt *time.Time
	require.NoError(t, b.pool.QueryRow(t.Context(),
		`SELECT document, conflicts, last_conflict_at FROM controlplane.usage_reports`,
	).Scan(&document, &conflicts, &lastConflictAt))
	require.Equal(t, string(first), document) //nolint:testifylint // the exact bytes received, not their meaning
	require.Equal(t, 2, conflicts)
	require.NotNil(t, lastConflictAt)
	require.True(t, b.now.Equal(*lastConflictAt))
	require.Equal(t, 1, b.count("usage_reports"))
}

func Test_Receive_Refused(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	for name, newBench := range map[string]func(*testing.T) *bench{
		"license known":   newBenchWithLicense,
		"license unknown": newBench,
	} {
		t.Run(name, func(t *testing.T) { refusals(t, newBench(t)) })
	}
}

// refusals hands the intake what a caller can get wrong, and checks that nothing is written.
func refusals(t *testing.T, b *bench) {
	t.Helper()
	good := b.document(b.report(instanceA, "2026-10-06"))
	goodSeal := b.seal(good)
	sealed := func(document []byte) (body []byte, seal, fingerprint string) {
		return document, b.seal(document), b.fingerprint
	}

	cases := map[string]func() (document []byte, seal, fingerprint string){
		// The first "day" is not UTF-8; a JSON reader keeps the second one, so the document
		// passes the schema.
		"a body that is not UTF-8": func() ([]byte, string, string) {
			document := []byte(strings.Replace(string(good), `{`, "{\"day\":\"\xff\",", 1))
			require.NoError(t, telemetry.Validate(document))
			return sealed(document)
		},
		"a day too far ahead": func() ([]byte, string, string) {
			return sealed(b.document(b.report(instanceA, "2026-10-09")))
		},
		"a day too far back": func() ([]byte, string, string) {
			return sealed(b.document(b.report(instanceA, "2026-08-07")))
		},
		"no seal":                  func() ([]byte, string, string) { return good, "", b.fingerprint },
		"seal too short":           func() ([]byte, string, string) { return good, goodSeal[:63], b.fingerprint },
		"seal in upper case":       func() ([]byte, string, string) { return good, strings.ToUpper(goodSeal), b.fingerprint },
		"no fingerprint":           func() ([]byte, string, string) { return good, goodSeal, "" },
		"fingerprint too long":     func() ([]byte, string, string) { return good, goodSeal, b.fingerprint + "0" },
		"fingerprint not hex":      func() ([]byte, string, string) { return good, goodSeal, strings.Repeat("g", 64) },
		"fingerprint in uppercase": func() ([]byte, string, string) { return good, goodSeal, strings.ToUpper(b.fingerprint) },
		"empty body":               func() ([]byte, string, string) { return sealed(nil) },
		"not JSON":                 func() ([]byte, string, string) { return sealed([]byte(`{"schema_version":1,`)) },
		"a field outside the schema": func() ([]byte, string, string) {
			return sealed([]byte(strings.Replace(string(good), `{`, `{"hostname":"db-1",`, 1)))
		},
		"a day that is not a date": func() ([]byte, string, string) {
			return sealed(b.document(b.report(instanceA, "2026-13-45")))
		},
		"the fingerprint of another key in the header": func() ([]byte, string, string) {
			return good, goodSeal, telemetry.KeyFingerprint("another key")
		},
		"the fingerprint of another key in the document": func() ([]byte, string, string) {
			report := b.report(instanceA, "2026-10-06")
			report.Identification.KeyFingerprint = telemetry.KeyFingerprint("another key")
			return sealed(b.document(report))
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			document, seal, fingerprint := build()
			outcome, err := b.intake.Receive(t.Context(), document, seal, fingerprint)
			require.NoError(t, err)
			require.Equal(t, intake.Refused, outcome)
		})
	}
	require.Zero(t, b.count("usage_reports"))
	require.Zero(t, b.count("instances"))
	require.Zero(t, b.count("pending_reports"))
	require.Zero(t, b.count("seal_rejections"))
}

func Test_Receive_WrongSeal_RefusedAndCounted(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBenchWithLicense(t)
	document := b.document(b.report(instanceA, "2026-10-06"))
	wrong := b.seal([]byte("another document"))

	// 23:30 then 00:30 UTC: two days of reception.
	b.now = time.Date(2026, 10, 7, 23, 30, 0, 0, time.UTC)
	require.Equal(t, intake.Refused, b.receive(document, wrong, b.fingerprint))
	require.Equal(t, intake.Refused, b.receive(document, wrong, b.fingerprint))
	b.now = time.Date(2026, 10, 8, 0, 30, 0, 0, time.UTC)
	require.Equal(t, intake.Refused, b.receive(document, wrong, b.fingerprint))

	rows, err := b.pool.Query(ctx,
		`SELECT license_id, day::text, count, last_at FROM controlplane.seal_rejections ORDER BY day`)
	require.NoError(t, err)
	defer rows.Close()
	type rejection struct {
		license, day string
		count        int
	}
	var got []rejection
	var lastAt time.Time
	for rows.Next() {
		var r rejection
		require.NoError(t, rows.Scan(&r.license, &r.day, &r.count, &lastAt))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []rejection{{"lic-1", "2026-10-07", 2}, {"lic-1", "2026-10-08", 1}}, got)
	require.True(t, b.now.Equal(lastAt))
	require.Zero(t, b.count("usage_reports"))
	require.Zero(t, b.count("instances"))
	require.Zero(t, b.count("pending_reports"))
}

func Test_Receive_PendingCapOfAFingerprint_Full(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	already := b.document(b.report(instanceA, "2026-10-06"))
	require.Equal(t, intake.Pending, b.post(already))
	_, err := b.pool.Exec(t.Context(),
		`INSERT INTO controlplane.pending_reports (key_fingerprint, instance_id, day, document, seal, received_at)
		 SELECT $1, 'instance-' || n, '2026-10-06', '{}', 'seal', now() FROM generate_series(1, $2::int) n`,
		b.fingerprint, intake.PendingCapPerFingerprint-2)
	require.NoError(t, err)

	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceB, "2026-10-06"))), "the last one under the cap")
	require.Equal(t, intake.Full, b.post(b.document(b.report(instanceC, "2026-10-06"))))
	require.Equal(t, intake.Pending, b.post(already), "a report already pending is not refused")
	require.Equal(t, intake.PendingCapPerFingerprint, b.count("pending_reports"))
}

func Test_Receive_GlobalPendingCap_Full(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	already := b.document(b.report(instanceA, "2026-10-06"))
	require.Equal(t, intake.Pending, b.post(already))
	_, err := b.pool.Exec(t.Context(),
		`INSERT INTO controlplane.pending_reports (key_fingerprint, instance_id, day, document, seal, received_at)
		 SELECT 'fingerprint-' || n, 'instance', '2026-10-06', '{}', 'seal', now() FROM generate_series(1, $1::int) n`,
		intake.PendingCapTotal-2)
	require.NoError(t, err)

	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceB, "2026-10-06"))), "the last one under the cap")
	require.Equal(t, intake.Full, b.post(b.document(b.report(instanceC, "2026-10-06"))))
	require.Equal(t, intake.Pending, b.post(already), "a report already pending is not refused")
	require.Equal(t, intake.PendingCapTotal, b.count("pending_reports"))
}

func Test_Receive_KnownLicense_IsNotHeldByTheCaps(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	_, err := b.pool.Exec(t.Context(),
		`INSERT INTO controlplane.pending_reports (key_fingerprint, instance_id, day, document, seal, received_at)
		 SELECT 'fingerprint-' || n, 'instance', '2026-10-06', '{}', 'seal', now() FROM generate_series(1, $1::int) n`,
		intake.PendingCapTotal)
	require.NoError(t, err)

	require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-10-06"))))
}

func Test_Receive_AnEarlierDayAfterALaterOne_KeepsTheLastReportDay(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	require.Equal(t, intake.Stored, b.post(b.document(b.report(instanceA, "2026-10-06"))))

	earlier := b.report(instanceA, "2026-10-05")
	earlier.Version.Husonym = "v0.2.0"
	b.now = received.Add(time.Hour)
	require.Equal(t, intake.Stored, b.post(b.document(earlier)))

	var firstSeen, lastSeen time.Time
	var lastDay, version string
	require.NoError(t, b.pool.QueryRow(t.Context(),
		`SELECT first_seen_at, last_seen_at, last_report_day::text, husonym_version
		 FROM controlplane.instances WHERE instance_id = $1`, instanceA,
	).Scan(&firstSeen, &lastSeen, &lastDay, &version))
	require.Equal(t, "2026-10-06", lastDay)
	require.Equal(t, "v0.3.0", version)
	require.True(t, received.Equal(firstSeen))
	require.True(t, received.Equal(lastSeen))
	require.Equal(t, 2, b.count("usage_reports"))
}

func Test_Receive_DatabaseFailure_IsAnErrorOfOurs(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBenchWithLicense(t)
	document := b.document(b.report(instanceA, "2026-10-06"))
	b.pool.Close()

	_, err := b.intake.Receive(t.Context(), document, b.seal(document), b.fingerprint)
	require.Error(t, err)
}

// Review Focus 1: the report came before its license was recorded. The fingerprint then shows
// among the ones now known, and the promotion stores what was pending.
func Test_PromotePending_StoresWhatVerifies(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBench(t)
	documents := map[string][]byte{
		instanceA: b.document(withDiagnostics(b.report(instanceA, "2026-10-05"), "compose")),
		instanceB: b.document(b.report(instanceB, "2026-10-06")),
	}
	for _, document := range documents {
		require.Equal(t, intake.Pending, b.post(document))
	}
	known, err := b.store.PendingFingerprintsNowKnown(ctx)
	require.NoError(t, err)
	require.Empty(t, known)

	b.addLicense()
	known, err = b.store.PendingFingerprintsNowKnown(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{b.fingerprint}, known)

	b.now = received.Add(time.Hour)
	stored, discarded, err := b.intake.PromotePending(ctx, b.fingerprint)
	require.NoError(t, err)
	require.Equal(t, 2, stored)
	require.Zero(t, discarded)

	require.Zero(t, b.count("pending_reports"))
	rows, err := b.pool.Query(ctx,
		`SELECT instance_id, document, license_id, received_at FROM controlplane.usage_reports`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string][]byte{}
	for rows.Next() {
		var instance, document, licenseID string
		var receivedAt time.Time
		require.NoError(t, rows.Scan(&instance, &document, &licenseID, &receivedAt))
		got[instance] = []byte(document)
		require.Equal(t, "lic-1", licenseID)
		require.True(t, received.Equal(receivedAt), "a promoted report keeps the moment it was received")
	}
	require.NoError(t, rows.Err())
	require.Equal(t, documents, got)

	var kind string
	var firstSeen time.Time
	require.NoError(t, b.pool.QueryRow(ctx,
		`SELECT install_kind, first_seen_at FROM controlplane.instances WHERE instance_id = $1`, instanceA,
	).Scan(&kind, &firstSeen))
	require.Equal(t, "compose", kind)
	require.True(t, received.Equal(firstSeen))

	known, err = b.store.PendingFingerprintsNowKnown(ctx)
	require.NoError(t, err)
	require.Empty(t, known)
}

// Review Focus 3.
func Test_PromotePending_AWrongSealIsDiscardedAndDoesNotBlockTheOthers(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBench(t)
	// The forged one sorts first: it is met before the two that verify.
	forged := b.document(b.report(instanceA, "2026-10-04"))
	require.Equal(t, intake.Pending, b.receive(forged, strings.Repeat("0", 64), b.fingerprint))
	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceB, "2026-10-05"))))
	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceC, "2026-10-06"))))
	b.addLicense()

	b.now = time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)
	stored, discarded, err := b.intake.PromotePending(ctx, b.fingerprint)
	require.NoError(t, err)
	require.Equal(t, 2, stored)
	require.Equal(t, 1, discarded)

	require.Zero(t, b.count("pending_reports"))
	var instances []string
	rows, err := b.pool.Query(ctx, `SELECT instance_id FROM controlplane.usage_reports ORDER BY instance_id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var instance string
		require.NoError(t, rows.Scan(&instance))
		instances = append(instances, instance)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{instanceB, instanceC}, instances)
	require.Equal(t, 2, b.count("instances"))

	var rejectionDay string
	var rejections int
	require.NoError(t, b.pool.QueryRow(ctx,
		`SELECT day::text, count FROM controlplane.seal_rejections WHERE license_id = 'lic-1'`,
	).Scan(&rejectionDay, &rejections))
	require.Equal(t, "2026-10-09", rejectionDay)
	require.Equal(t, 1, rejections)
}

func Test_PromotePending_AReportAlreadyStoredIsNotStoredTwice(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBench(t)
	document := b.document(b.report(instanceA, "2026-10-06"))
	require.Equal(t, intake.Pending, b.post(document))
	b.addLicense()
	// The instance tries again before the promotion runs.
	require.Equal(t, intake.Stored, b.post(document))

	stored, discarded, err := b.intake.PromotePending(ctx, b.fingerprint)
	require.NoError(t, err)
	require.Equal(t, 1, stored)
	require.Zero(t, discarded)
	require.Zero(t, b.count("pending_reports"))
	require.Equal(t, 1, b.count("usage_reports"))

	var conflicts int
	require.NoError(t, b.pool.QueryRow(ctx, `SELECT conflicts FROM controlplane.usage_reports`).Scan(&conflicts))
	require.Zero(t, conflicts)
}

func Test_PromotePending_UnknownFingerprint_LeavesWhatIsPending(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	b := newBench(t)
	require.Equal(t, intake.Pending, b.post(b.document(b.report(instanceA, "2026-10-06"))))

	stored, discarded, err := b.intake.PromotePending(t.Context(), b.fingerprint)
	require.NoError(t, err)
	require.Zero(t, stored)
	require.Zero(t, discarded)
	require.Equal(t, 1, b.count("pending_reports"))
}

func Test_PurgePending_45Days(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	b := newBench(t)
	require.Equal(t, 45*24*time.Hour, intake.PendingKept)

	for i, age := range []time.Duration{46 * 24 * time.Hour, 45*24*time.Hour + time.Second, 45 * 24 * time.Hour, 44 * 24 * time.Hour} {
		b.now = received.Add(-age)
		instance := fmt.Sprintf("%d23e4567-e89b-12d3-a456-426614174000", i)
		require.Equal(t, intake.Pending, b.post(b.document(b.report(instance, "2026-08-01"))))
	}

	purged, err := b.store.PurgePending(ctx, received.Add(-intake.PendingKept))
	require.NoError(t, err)
	require.EqualValues(t, 2, purged)

	pending, err := b.store.PendingReports(ctx, b.fingerprint)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	for i := range pending {
		require.False(t, pending[i].ReceivedAt.Before(received.Add(-intake.PendingKept)))
	}
}
