package usagereport

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the reference report (testdata/report-full.json)")

const fullReportPath = "testdata/report-full.json"

const (
	testKid      = "test"
	testInstance = "0b9d6c1e-5a44-4f0e-9d3b-2f6a8c7e1d10"
	testLicense  = "8f2a41c09b7e63d5"
	jobGone      = "00000000-0000-0000-0000-0000000000a9"
)

var (
	// reportDay is the day the reports of the tests are about, and reportNow when they are made.
	reportDay = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	reportNow = time.Date(2026, 10, 7, 0, 5, 12, 0, time.UTC)
	// testExpiry is 212 days and some hours after reportNow.
	testExpiry = time.Date(2027, 5, 7, 12, 0, 0, 0, time.UTC)
)

// mintKey signs a key with a pair made from a fixed seed, the way the issuer lays it out: the
// same content always gives the same value. It gives the ring that verifies it too.
func mintKey(t testing.TB, key license.Key) (string, license.Keyring) { //nolint:gocritic // hugeParam: a value a test writes in place
	t.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	pub, ok := priv.Public().(ed25519.PublicKey)
	require.True(t, ok)
	content, err := json.Marshal(key)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{
		"license":   base64.StdEncoding.EncodeToString(content),
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(priv, content)),
		"kid":       testKid,
	})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw), license.Keyring{testKid: pub}
}

// keyExpiring gives a key of the license of the tests that expires at the given time. What a
// person wrote in it holds the marker.
func keyExpiring(expiresAt time.Time) license.Key {
	return license.Key{
		Version:    "2",
		Id:         testLicense,
		IssuedTo:   leak + " corp",
		CustomerId: leak + "-customer",
		IssuedAt:   time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		ExpiresAt:  expiresAt,
		Plan:       leak + "-plan",
	}
}

type fakeLicense struct {
	license.EEInterface
	inForce bool
}

func (f *fakeLicense) IsValid() bool { return f.inForce }

type fakeKeys struct {
	value string
	err   error
	calls int
}

func (f *fakeKeys) Current(context.Context) (string, error) {
	f.calls++
	return f.value, f.err
}

type fakeCounters struct {
	instanceId string
	runs       *usagestore.DayRuns
	versions   []usagestore.SourceEngineRuns
	refusals   []usagestore.GateCount
	err        error
	calls      int
	days       []time.Time
}

func (f *fakeCounters) InstanceId(context.Context) (string, error) {
	f.calls++
	return f.instanceId, f.err
}

func (f *fakeCounters) RunsOfDay(_ context.Context, day time.Time) (*usagestore.DayRuns, error) {
	f.calls++
	f.days = append(f.days, day)
	return f.runs, f.err
}

func (f *fakeCounters) SourceVersionsOfDay(_ context.Context, day time.Time) ([]usagestore.SourceEngineRuns, error) {
	f.calls++
	f.days = append(f.days, day)
	return f.versions, f.err
}

func (f *fakeCounters) RefusalsOfDay(_ context.Context, day time.Time) ([]usagestore.GateCount, error) {
	f.calls++
	f.days = append(f.days, day)
	return f.refusals, f.err
}

type fakeInventory struct {
	inventory *Inventory
	err       error
	calls     int
	now       time.Time
}

func (f *fakeInventory) Read(_ context.Context, now time.Time) (*Inventory, error) {
	f.calls++
	f.now = now
	return f.inventory, f.err
}

type fakeInstance struct {
	sources         int
	postgresMajor   int
	temporalVersion string
	workers         int

	sourcesErr, postgresErr, temporalErr, workersErr error
	calls                                            int
}

func (f *fakeInstance) SourcesCount(context.Context) (int, error) {
	f.calls++
	return f.sources, f.sourcesErr
}

func (f *fakeInstance) PostgresMajor(context.Context) (int, error) {
	f.calls++
	return f.postgresMajor, f.postgresErr
}

func (f *fakeInstance) TemporalVersion(context.Context) (string, error) {
	f.calls++
	return f.temporalVersion, f.temporalErr
}

func (f *fakeInstance) Workers(context.Context) (int, error) {
	f.calls++
	return f.workers, f.workersErr
}

// fixture is an instance with a license in force and a day of activity, behind fakes.
type fixture struct {
	license   *fakeLicense
	keys      *fakeKeys
	ring      license.Keyring
	counters  *fakeCounters
	inventory *fakeInventory
	instance  *fakeInstance
	facts     Facts
}

func (f *fixture) builder() *Builder {
	return NewBuilder(f.counters, f.inventory, f.instance, f.license, f.keys, f.ring, f.facts)
}

func int64p(value int64) *int64 { return &value }

func intp(value int) *int { return &value }

func newFixture(t testing.TB) *fixture {
	t.Helper()
	key, ring := mintKey(t, keyExpiring(testExpiry))
	return &fixture{
		license: &fakeLicense{inForce: true},
		keys:    &fakeKeys{value: key},
		ring:    ring,
		counters: &fakeCounters{
			instanceId: testInstance,
			runs: &usagestore.DayRuns{
				ByStatus: []usagestore.RunCount{
					{Kind: usagestore.JobKindSync, Status: usagestore.StatusCompleted, Count: 12},
					{Kind: usagestore.JobKindSync, Status: usagestore.StatusFailed, Count: 2},
					{Kind: usagestore.JobKindGenerate, Status: usagestore.StatusCompleted, Count: 1},
				},
				DurationMedian: int64p(180), DurationP95: int64p(640),
				RowsRead: 2_500_000, RowsDiscarded: 12, Retries: 3, WithUncountedRows: 1,
			},
			// Two jobs read PostgreSQL 16, one MySQL 8.0, and one is gone since its run.
			versions: []usagestore.SourceEngineRuns{
				{JobId: jobOne, VersionMajor: "16", Runs: 10},
				{JobId: jobTwo, VersionMajor: "16", Runs: 4},
				{JobId: jobThree, VersionMajor: "8.0", Runs: 2},
				{JobId: jobGone, VersionMajor: "16", Runs: 1},
			},
			refusals: []usagestore.GateCount{
				{Gate: license.Gate("subsetting"), Count: 2},
				{Gate: license.Gate("source_cap"), Count: 1},
			},
		},
		inventory: &fakeInventory{inventory: &Inventory{
			Connections: []telemetry.ConnectionCount{
				{Type: "mysql", Role: "destination", Count: 1},
				{Type: "postgres", Role: "source", Count: 2},
			},
			Jobs: telemetry.Jobs{
				ByKind: []telemetry.JobKindCount{
					{Kind: "generate", Scheduled: false, Count: 2},
					{Kind: "sync", Scheduled: false, Count: 1},
					{Kind: "sync", Scheduled: true, Count: 4},
				},
				Tables: 120, Columns: 940, WithSubset: 1,
			},
			Transformers: telemetry.Transformers{
				System: []telemetry.TransformerColumns{
					{Name: "generate_email", Columns: 120},
					{Name: "passthrough", Columns: 5},
				},
				UserDefined: 3, UserDefinedColumns: 17,
			},
			ColumnTypes: []telemetry.ColumnTypeCount{
				{Family: "integer", Columns: 300},
				{Family: "text", Columns: 500},
			},
			Features: []telemetry.FeatureUse{
				{Name: "api_keys", InUse: false},
				{Name: "job_hooks", InUse: true},
			},
			Users: telemetry.Users{
				Accounts: 1, Users: 10, Active30d: intp(6),
				ByRole: []telemetry.RoleCount{{Role: "admin", Count: 2}, {Role: "job_viewer", Count: 8}},
			},
			AccountOidcProviders: 1,
			SourceTypeOfJob:      map[string]string{jobOne: "postgres", jobTwo: "postgres", jobThree: "mysql"},
		}},
		instance: &fakeInstance{sources: 3, postgresMajor: 16, temporalVersion: "1.25.2", workers: 2},
		facts: Facts{
			Version: "v0.3.0", InstallKind: "helm", OS: "linux", Arch: "amd64",
			AuthEnabled: true, AuthProvider: "keycloak", Presidio: true, RunLogs: "loki",
			Diagnostics: true,
		},
	}
}

// tree reads a document back as the tree of its JSON.
func tree(t testing.TB, document []byte) map[string]any {
	t.Helper()
	var read map[string]any
	require.NoError(t, json.Unmarshal(document, &read))
	return read
}

// block gives an object of the tree by its path.
func block(t testing.TB, read map[string]any, path ...string) map[string]any {
	t.Helper()
	for _, name := range path {
		next, ok := read[name].(map[string]any)
		require.True(t, ok, "the document has no object %q", name)
		read = next
	}
	return read
}

// The whole report of a day, byte for byte: the reference file is the document itself.
func Test_Build_TheFullReportIsTheReferenceFile(t *testing.T) {
	f := newFixture(t)
	ctx, output := logged(t)

	sealed, err := f.builder().Build(ctx, reportDay, reportNow)
	require.NoError(t, err)

	if *update {
		require.NoError(t, os.WriteFile(fullReportPath, sealed.Document, 0o600))
	}
	want, err := os.ReadFile(fullReportPath)
	require.NoError(t, err)
	require.Equal(t, string(want), string(sealed.Document),
		"the report differs from its reference: when that is meant, run go test ./backend/internal/usagereport -update")

	require.NoError(t, telemetry.Validate(sealed.Document))
	require.Equal(t, telemetry.KeyFingerprint(f.keys.value), sealed.KeyFingerprint)
	require.Equal(t, sealed.KeyFingerprint, block(t, tree(t, sealed.Document), "identification")["key_fingerprint"])

	// The counters are the ones of the day asked, and the state is read at the moment given.
	require.Equal(t, []time.Time{reportDay, reportDay, reportDay}, f.counters.days)
	require.Equal(t, reportNow, f.inventory.now)

	// Neither the key nor what a person wrote in it is in the document or in the logs.
	require.NotContains(t, string(sealed.Document), leak)
	require.NotContains(t, string(sealed.Document), f.keys.value)
	require.NotContains(t, output.String(), f.keys.value)
	require.Empty(t, output.String(), "a report that reads everything logs nothing")
}

// Whoever holds the key checks the seal, and nobody else: the seal is of these bytes and of
// this key.
func Test_Build_TheSealIsCheckedWithTheKey(t *testing.T) {
	f := newFixture(t)
	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.NoError(t, err)

	require.NoError(t, telemetry.Verify(f.keys.value, sealed.Document, sealed.Seal))

	altered := append(bytes.Clone(sealed.Document), ' ')
	require.Error(t, telemetry.Verify(f.keys.value, altered, sealed.Seal))

	renewal := keyExpiring(testExpiry.AddDate(1, 0, 0))
	otherKey, _ := mintKey(t, renewal)
	require.NotEqual(t, f.keys.value, otherKey)
	require.Error(t, telemetry.Verify(otherKey, sealed.Document, sealed.Seal))
}

// The diagnostic switched off: the three first blocks, and nothing of the rest is even read.
func Test_Build_WithoutDiagnostics(t *testing.T) {
	f := newFixture(t)
	f.facts.Diagnostics = false

	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.Validate(sealed.Document))
	require.NoError(t, telemetry.Verify(f.keys.value, sealed.Document, sealed.Seal))

	read := tree(t, sealed.Document)
	require.NotContains(t, read, "diagnostics")
	require.Equal(t, map[string]any{
		"key_fingerprint": telemetry.KeyFingerprint(f.keys.value),
		"license_id":      testLicense,
		"instance_id":     testInstance,
		"license_state":   "valid",
		"days_to_expiry":  float64(212),
	}, read["identification"])
	require.Equal(t, map[string]any{"husonym": "v0.3.0"}, read["version"])
	require.Equal(t, map[string]any{"count": float64(3)}, read["sources"])
	require.Equal(t, "2026-10-06", read["day"])
	require.Equal(t, "2026-10-07T00:05:12Z", read["generated_at"])

	require.Zero(t, f.inventory.calls)
	require.Equal(t, 1, f.counters.calls, "only the id of the instance is read")
	require.Equal(t, 1, f.instance.calls, "only the sources are counted")
}

// Without a license in force there is no report, and nothing of the instance is read.
func Test_Build_NoLicenseInForce(t *testing.T) {
	t.Run("the process holds no license in force", func(t *testing.T) {
		f := newFixture(t)
		f.license.inForce = false

		sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.ErrorIs(t, err, ErrNoLicenseInForce)
		require.Nil(t, sealed)
		require.Zero(t, f.keys.calls)
		require.Zero(t, f.counters.calls)
		require.Zero(t, f.inventory.calls)
		require.Zero(t, f.instance.calls)
	})

	t.Run("the instance holds no key", func(t *testing.T) {
		f := newFixture(t)
		f.keys.value = ""

		_, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.ErrorIs(t, err, ErrNoLicenseInForce)
		require.Zero(t, f.counters.calls+f.inventory.calls+f.instance.calls)
	})

	t.Run("the key of the instance is past its grace period", func(t *testing.T) {
		f := newFixture(t)
		f.keys.value, f.ring = mintKey(t, keyExpiring(reportNow.AddDate(0, 0, -license.DefaultGraceDays-1)))

		_, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.ErrorIs(t, err, ErrNoLicenseInForce)
		require.Zero(t, f.counters.calls+f.inventory.calls+f.instance.calls)
	})
}

// A key that cannot be read or verified is an error, which never cites it.
func Test_Build_AKeyThatCannotBeUsed(t *testing.T) {
	t.Run("the store does not answer", func(t *testing.T) {
		f := newFixture(t)
		f.keys.err = errors.New("connection refused")

		sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.ErrorContains(t, err, "connection refused")
		require.NotErrorIs(t, err, ErrNoLicenseInForce)
		require.Nil(t, sealed)
	})

	t.Run("the key is signed by nobody the instance knows", func(t *testing.T) {
		f := newFixture(t)
		f.ring = license.Keyring{}

		sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNoLicenseInForce)
		require.NotContains(t, err.Error(), f.keys.value)
		require.Nil(t, sealed)
		require.Zero(t, f.counters.calls+f.inventory.calls+f.instance.calls)
	})
}

// The state and the days left are the ones of the key at the moment given.
func Test_Build_LicenseStateAndDaysToExpiry(t *testing.T) {
	for name, tc := range map[string]struct {
		expiresAt time.Time
		state     string
		days      int
	}{
		"far from its expiry":     {reportNow.Add(212*24*time.Hour + 12*time.Hour), "valid", 212},
		"a day and a half left":   {reportNow.Add(36 * time.Hour), "expiring", 1},
		"some hours left":         {reportNow.Add(5 * time.Hour), "expiring", 0},
		"expired some hours ago":  {reportNow.Add(-5 * time.Hour), "grace", -1},
		"expired three days ago":  {reportNow.Add(-72 * time.Hour), "grace", -3},
		"expired three days more": {reportNow.Add(-73 * time.Hour), "grace", -4},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.facts.Diagnostics = false
			f.keys.value, f.ring = mintKey(t, keyExpiring(tc.expiresAt))

			sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
			require.NoError(t, err)
			identification := block(t, tree(t, sealed.Document), "identification")
			require.Equal(t, tc.state, identification["license_state"])
			require.Equal(t, float64(tc.days), identification["days_to_expiry"])
		})
	}
}

// What the instance could not tell of where it runs is left out, said in the log, and the
// report is made all the same.
func Test_Build_AnOptionalReadingThatFails(t *testing.T) {
	failure := errors.New("unavailable")
	for name, tc := range map[string]struct {
		fail    func(*fakeInstance)
		missing string
	}{
		"the version of the database":     {func(i *fakeInstance) { i.postgresErr = failure }, "postgres_major"},
		"the version of the orchestrator": {func(i *fakeInstance) { i.temporalErr = failure }, "temporal_version"},
		"the workers":                     {func(i *fakeInstance) { i.workersErr = failure }, "workers"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.fail(f.instance)
			ctx, output := logged(t)

			sealed, err := f.builder().Build(ctx, reportDay, reportNow)
			require.NoError(t, err)
			require.NoError(t, telemetry.Validate(sealed.Document))
			require.NoError(t, telemetry.Verify(f.keys.value, sealed.Document, sealed.Seal))

			installation := block(t, tree(t, sealed.Document), "diagnostics", "installation")
			for _, field := range []string{"postgres_major", "temporal_version", "workers"} {
				if field == tc.missing {
					require.NotContains(t, installation, field)
				} else {
					require.Contains(t, installation, field)
				}
			}
			require.Contains(t, output.String(), `"level":"WARN"`)
			require.Contains(t, output.String(), "unavailable")
		})
	}

	t.Run("all three", func(t *testing.T) {
		f := newFixture(t)
		f.instance.postgresErr, f.instance.temporalErr, f.instance.workersErr = failure, failure, failure
		ctx, _ := logged(t)

		sealed, err := f.builder().Build(ctx, reportDay, reportNow)
		require.NoError(t, err)
		require.NoError(t, telemetry.Validate(sealed.Document))
		require.Equal(t,
			map[string]any{"kind": "helm", "os": "linux", "arch": "amd64"},
			block(t, tree(t, sealed.Document), "diagnostics", "installation"))
	})
}

// A reading the report cannot do without fails it: nothing is sealed.
func Test_Build_ARequiredReadingThatFails(t *testing.T) {
	failure := errors.New("unavailable")
	for name, fail := range map[string]func(*fixture){
		"the usage store": func(f *fixture) { f.counters.err = failure },
		"the sources":     func(f *fixture) { f.instance.sourcesErr = failure },
		"the inventory":   func(f *fixture) { f.inventory.err = failure },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			fail(f)

			sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
			require.ErrorIs(t, err, failure)
			require.Nil(t, sealed)
		})
	}
}

// A document the schema refuses is an error: it is never sealed.
func Test_Build_ADocumentTheSchemaRefuses(t *testing.T) {
	f := newFixture(t)
	f.counters.instanceId = "not-an-instance-id"

	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.ErrorContains(t, err, "does not match its schema")
	require.Nil(t, sealed)
}

// Whatever is not of a closed list becomes other, or is left out where the list has no other.
func Test_Build_ValuesOutsideTheirLists(t *testing.T) {
	f := newFixture(t)
	handKey := keyExpiring(testExpiry)
	handKey.Id = leak + "-contract-42"
	f.keys.value, f.ring = mintKey(t, handKey)
	f.facts = Facts{
		Version: leak + " build", InstallKind: "my-cluster", OS: "plan9", Arch: "riscv64",
		AuthEnabled: true, AuthProvider: leak + "-idp", RunLogs: leak + "-sink", Diagnostics: true,
	}
	f.instance.temporalVersion = "1.25.2-" + leak
	f.counters.runs.ByStatus = []usagestore.RunCount{
		{Kind: usagestore.JobKindSync, Status: usagestore.StatusCompleted, Count: 12},
		{Kind: usagestore.JobKind(leak + "-kind"), Status: usagestore.Status(leak + "-status"), Count: 2},
		{Kind: usagestore.JobKind(leak + "-another-kind"), Status: usagestore.Status(leak + "-another"), Count: 3},
	}
	f.counters.versions = []usagestore.SourceEngineRuns{
		{JobId: jobOne, VersionMajor: "16", Runs: 10},
		{JobId: jobOne, VersionMajor: leak + "-16", Runs: 5},
		{JobId: jobTwo, VersionMajor: "16", Runs: 4},
	}
	f.inventory.inventory.SourceTypeOfJob = map[string]string{jobOne: "postgres", jobTwo: leak + "-engine"}
	f.counters.refusals = []usagestore.GateCount{
		{Gate: license.Gate("subsetting"), Count: 2},
		{Gate: license.Gate(leak + "-gate"), Count: 7},
	}

	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.Validate(sealed.Document))
	require.NotContains(t, string(sealed.Document), leak)

	read := tree(t, sealed.Document)
	require.Equal(t, "other", block(t, read, "identification")["license_id"])
	require.Equal(t, "other", block(t, read, "version")["husonym"])
	require.Equal(t,
		map[string]any{"kind": "other", "os": "other", "arch": "other", "postgres_major": float64(16), "workers": float64(2)},
		block(t, read, "diagnostics", "installation"))
	configuration := block(t, read, "diagnostics", "configuration")
	require.Equal(t, "other", configuration["auth_provider"])
	require.Equal(t, "other", configuration["run_logs"])

	diagnostics := block(t, read, "diagnostics")
	// The two kinds nobody knows are one row.
	require.Equal(t, []any{
		map[string]any{"kind": "other", "status": "other", "count": float64(5)},
		map[string]any{"kind": "sync", "status": "completed", "count": float64(12)},
	}, block(t, diagnostics, "runs")["by_status"])
	// A version that is not one is not counted; a type that is not one is other.
	require.Equal(t, []any{
		map[string]any{"type": "other", "major": "16", "runs": float64(4)},
		map[string]any{"type": "postgres", "major": "16", "runs": float64(10)},
	}, diagnostics["source_engines"])
	require.Equal(t, []any{map[string]any{"gate": "subsetting", "count": float64(2)}}, diagnostics["refusals"])
}

// A day without a run: the counters are there and empty, the durations are not.
func Test_Build_ADayWithoutRuns(t *testing.T) {
	f := newFixture(t)
	f.counters.runs = &usagestore.DayRuns{}
	f.counters.versions = nil
	f.counters.refusals = nil
	f.facts.AuthEnabled, f.facts.AuthProvider = false, "keycloak"
	f.inventory.inventory.Users.Active30d = nil

	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.Validate(sealed.Document))

	diagnostics := block(t, tree(t, sealed.Document), "diagnostics")
	require.Equal(t, map[string]any{
		"by_status": []any{}, "rows_read": "lt_1k", "rows_discarded": "lt_1k",
		"retries": float64(0), "with_uncounted_rows": float64(0),
	}, diagnostics["runs"])
	require.Equal(t, []any{}, diagnostics["source_engines"])
	require.Equal(t, []any{}, diagnostics["refusals"])
	require.Equal(t, []any{}, diagnostics["errors"])
	// Without authentication no provider is named, whatever the variable holds.
	require.NotContains(t, block(t, diagnostics, "configuration"), "auth_provider")
	require.Equal(t, false, block(t, diagnostics, "configuration")["auth_enabled"])
}

// setEnvironment sets the two variables the facts read, for the time of a test.
func setEnvironment(t testing.TB, installKind, diagnostics any) {
	t.Helper()
	viper.Set(installKindVariable, installKind)
	viper.Set(diagnosticsVariable, diagnostics)
	t.Cleanup(func() {
		viper.Set(installKindVariable, nil)
		viper.Set(diagnosticsVariable, nil)
	})
}

func Test_FactsFromEnvironment_AuthenticationAndRunLogs(t *testing.T) {
	type logs struct {
		enabled bool
		sink    string
	}
	for _, auth := range []struct {
		enabled  bool
		provider string
		want     string
	}{
		{true, "", "auth0"},
		{true, "auth0", "auth0"},
		{true, "keycloak", "keycloak"},
		{true, "Keycloak", "keycloak"},
		{true, leak + "-idp", "other"},
		{false, "", ""},
		{false, "auth0", ""},
		{false, "keycloak", ""},
		{false, leak + "-idp", ""},
	} {
		for runLogs, want := range map[logs]string{
			{false, ""}:               "none",
			{false, "loki"}:           "none",
			{false, "k8s-pods"}:       "none",
			{true, "loki"}:            "loki",
			{true, "k8s-pods"}:        "k8s_pods",
			{true, leak + "-sink"}:    "other",
			{false, leak + "-sink"}:   "none",
			{true, ""}:                "none",
			{true, " Loki "}:          "loki",
			{true, "k8s_pods"}:        "k8s_pods",
			{false, "k8s_pods"}:       "none",
			{true, leak + " k8s-pod"}: "other",
		} {
			setEnvironment(t, nil, nil)
			facts := FactsFromEnvironment("v0.3.0", auth.enabled, auth.provider, true, runLogs.enabled, runLogs.sink)
			require.Equal(t, auth.enabled, facts.AuthEnabled)
			require.Equal(t, auth.want, facts.AuthProvider, "authentication %v by %q", auth.enabled, auth.provider)
			require.Equal(t, want, facts.RunLogs, "run logs %v to %q", runLogs.enabled, runLogs.sink)
			require.True(t, facts.Presidio)
		}
	}
}

func Test_FactsFromEnvironment_InstallKind(t *testing.T) {
	for raw, want := range map[string]string{
		"helm": "helm", "compose": "compose", " Helm ": "helm", "": "other", "my-cluster": "other",
		leak + ".corp.internal": "other",
	} {
		setEnvironment(t, raw, nil)
		require.Equal(t, want, FactsFromEnvironment("v0.3.0", false, "", false, false, "").InstallKind, "kind %q", raw)
	}
}

// Only the value false switches the diagnostic off: a variable that is absent, empty or
// mistyped leaves it on.
func Test_FactsFromEnvironment_Diagnostics(t *testing.T) {
	for raw, want := range map[string]bool{
		"": true, "true": true, "false": false, " False ": false, "FALSE": false, "0": true, "no": true, "off": true,
	} {
		setEnvironment(t, nil, raw)
		require.Equal(t, want, FactsFromEnvironment("v0.3.0", false, "", false, false, "").Diagnostics, "value %q", raw)
	}
	setEnvironment(t, nil, nil)
	require.True(t, FactsFromEnvironment("v0.3.0", false, "", false, false, "").Diagnostics)
}

// The process tells its own version and platform, each through its list.
func Test_FactsFromEnvironment_VersionAndPlatform(t *testing.T) {
	setEnvironment(t, "compose", nil)
	facts := FactsFromEnvironment("v0.0.0-main", false, "", false, false, "")
	require.Equal(t, "v0.0.0-main", facts.Version)
	require.Contains(t, telemetry.OperatingSystems, facts.OS)
	require.Contains(t, telemetry.Architectures, facts.Arch)
	require.False(t, facts.Presidio)

	require.Equal(t, "other", FactsFromEnvironment(leak+" build", false, "", false, false, "").Version)
}

// An environment nobody expected reaches the report as other.
func Test_Build_AnUnexpectedEnvironment(t *testing.T) {
	setEnvironment(t, "my-cluster", nil)
	f := newFixture(t)
	f.facts = FactsFromEnvironment("v0.3.0", true, leak+"-idp", true, true, "loki")

	sealed, err := f.builder().Build(t.Context(), reportDay, reportNow)
	require.NoError(t, err)
	require.NoError(t, telemetry.Validate(sealed.Document))
	diagnostics := block(t, tree(t, sealed.Document), "diagnostics")
	require.Equal(t, "other", block(t, diagnostics, "installation")["kind"])
	require.Equal(t, "other", block(t, diagnostics, "configuration")["auth_provider"])
	require.NotContains(t, string(sealed.Document), "my-cluster")
	require.NotContains(t, string(sealed.Document), leak)
}
