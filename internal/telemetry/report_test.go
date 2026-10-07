package telemetry

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func intp(n int) *int { return &n }

func fullReport() *Report {
	return &Report{
		SchemaVersion: SchemaVersion,
		Day:           "2026-10-06",
		GeneratedAt:   "2026-10-07T00:05:12Z",
		Identification: Identification{
			KeyFingerprint: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			LicenseID:      "lic_abc123",
			InstanceID:     "123e4567-e89b-12d3-a456-426614174000",
			Plan:           "team",
			LicenseState:   "valid",
			DaysToExpiry:   intp(212),
		},
		Version: Version{Husonym: "v0.3.0"},
		Sources: Sources{Count: 3},
		Diagnostics: &Diagnostics{
			Installation: Installation{
				Kind: "helm", OS: "linux", Arch: "amd64",
				PostgresMajor: intp(16), TemporalVersion: "1.25.2", Workers: intp(2),
			},
			Configuration: Configuration{
				AuthEnabled: true, AuthProvider: "keycloak", AccountOIDCProviders: 1, Presidio: true, RunLogs: "loki",
			},
			Connections: []ConnectionCount{
				{Type: "postgres", Role: "source", Count: 2}, {Type: "mysql", Role: "destination", Count: 1},
			},
			SourceEngines: []SourceEngine{{Type: "postgres", Major: "16", Runs: 14}, {Type: "mysql", Major: "8.0", Runs: 2}},
			Jobs: Jobs{
				ByKind: []JobKindCount{
					{Kind: "sync", Scheduled: true, Count: 4}, {Kind: "sync", Scheduled: false, Count: 1},
					{Kind: "generate", Scheduled: false, Count: 2},
				},
				Tables: 120, Columns: 940, WithSubset: 1,
			},
			Transformers: Transformers{
				System:      []TransformerColumns{{Name: "generate_email", Columns: 120}, {Name: "passthrough", Columns: 5}},
				UserDefined: 3, UserDefinedColumns: 17,
			},
			ColumnTypes: []ColumnTypeCount{{Family: "text", Columns: 500}, {Family: "integer", Columns: 300}},
			Features:    []FeatureUse{{Name: "job_hooks", InUse: true}, {Name: "api_keys", InUse: false}},
			Refusals:    []GateCount{{Gate: "subsetting", Count: 2}, {Gate: "source_cap", Count: 1}},
			Runs: Runs{
				ByStatus: []RunCount{
					{Kind: "sync", Status: "completed", Count: 12}, {Kind: "sync", Status: "failed", Count: 2},
				},
				DurationSeconds: &Durations{Median: 180, P95: 640},
				RowsRead:        "lt_10m", RowsDiscarded: "lt_1k", Retries: 3, WithUncountedRows: 1,
			},
			Errors: []ErrorCount{},
			Users: Users{
				Accounts: 1, Users: 10, Active30d: intp(6),
				ByRole: []RoleCount{{Role: "admin", Count: 2}, {Role: "job_viewer", Count: 8}},
			},
		},
	}
}

func Test_Report_FullPassesValidate(t *testing.T) {
	document, err := fullReport().Marshal()
	require.NoError(t, err)
	require.NoError(t, Validate(document))
}

func Test_Report_ThePublishedExampleIsAccepted(t *testing.T) {
	require.NoError(t, Validate([]byte(`{
	  "schema_version": 1, "day": "2026-10-06", "generated_at": "2026-10-07T00:05:12Z",
	  "identification": {"instance_id": "123e4567-e89b-12d3-a456-426614174000", "license_state": "none"},
	  "version": {"husonym": "v0.3.0"}, "sources": {"count": 0}
	}`)))
}

func Test_Report_WithoutDiagnosticsOrOptionalFieldsOmitsThem(t *testing.T) {
	r := fullReport()
	r.Diagnostics.Installation.PostgresMajor = nil
	r.Diagnostics.Installation.TemporalVersion = ""
	r.Diagnostics.Installation.Workers = nil
	r.Diagnostics.Configuration.AuthEnabled = false
	r.Diagnostics.Configuration.AuthProvider = ""
	r.Diagnostics.Runs.DurationSeconds = nil
	r.Diagnostics.Users.Active30d = nil
	document, err := r.Marshal()
	require.NoError(t, err)
	require.NoError(t, Validate(document))

	var tree map[string]any
	require.NoError(t, json.Unmarshal(document, &tree))
	diagnostics := tree["diagnostics"].(map[string]any)
	for block, keys := range map[string][]string{
		"installation":  {"postgres_major", "temporal_version", "workers"},
		"configuration": {"auth_provider"},
		"runs":          {"duration_seconds"},
		"users":         {"active_30d"},
	} {
		for _, key := range keys {
			require.NotContains(t, diagnostics[block], key)
		}
	}

	r.Diagnostics = nil
	document, err = r.Marshal()
	require.NoError(t, err)
	require.NoError(t, Validate(document))
	require.NotContains(t, string(document), "diagnostics")
}

func Test_Report_ErrorsIsAnEmptyArrayNotNull(t *testing.T) {
	r := fullReport()
	r.Diagnostics.Errors = nil
	r.Diagnostics.Connections = nil
	document, err := r.Marshal()
	require.NoError(t, err)
	require.Contains(t, string(document), `"errors":[]`)
	require.Contains(t, string(document), `"connections":[]`)
	require.NoError(t, Validate(document))
}

func Test_Validate_RefusesWhatTheSchemaDoesNotKnow(t *testing.T) {
	document, err := fullReport().Marshal()
	require.NoError(t, err)
	var tree map[string]any
	require.NoError(t, json.Unmarshal(document, &tree))

	tree["job_name"] = "nightly"
	extra, _ := json.Marshal(tree)
	require.Error(t, Validate(extra))

	delete(tree, "job_name")
	tree["diagnostics"].(map[string]any)["jobs"].(map[string]any)["by_kind"].([]any)[0].(map[string]any)["name"] = "x"
	nested, _ := json.Marshal(tree)
	require.Error(t, Validate(nested))

	require.Error(t, Validate([]byte(`not json`)))
	require.Error(t, Validate([]byte(`{}`)))
}

func Test_Validate_RefusesAValueOutsideOfItsList(t *testing.T) {
	for name, mutate := range map[string]func(r *Report){
		"license_state":    func(r *Report) { r.Identification.LicenseState = "bogus" },
		"install kind":     func(r *Report) { r.Diagnostics.Installation.Kind = "bogus" },
		"os":               func(r *Report) { r.Diagnostics.Installation.OS = "bogus" },
		"arch":             func(r *Report) { r.Diagnostics.Installation.Arch = "bogus" },
		"auth provider":    func(r *Report) { r.Diagnostics.Configuration.AuthProvider = "bogus" },
		"run logs":         func(r *Report) { r.Diagnostics.Configuration.RunLogs = "bogus" },
		"connection type":  func(r *Report) { r.Diagnostics.Connections[0].Type = "bogus" },
		"connection role":  func(r *Report) { r.Diagnostics.Connections[0].Role = "bogus" },
		"engine type":      func(r *Report) { r.Diagnostics.SourceEngines[0].Type = "bogus" },
		"job kind":         func(r *Report) { r.Diagnostics.Jobs.ByKind[0].Kind = "bogus" },
		"transformer":      func(r *Report) { r.Diagnostics.Transformers.System[0].Name = "bogus" },
		"column family":    func(r *Report) { r.Diagnostics.ColumnTypes[0].Family = "bogus" },
		"feature":          func(r *Report) { r.Diagnostics.Features[0].Name = "bogus" },
		"gate":             func(r *Report) { r.Diagnostics.Refusals[0].Gate = "bogus" },
		"run kind":         func(r *Report) { r.Diagnostics.Runs.ByStatus[0].Kind = "bogus" },
		"run status":       func(r *Report) { r.Diagnostics.Runs.ByStatus[0].Status = "bogus" },
		"rows read":        func(r *Report) { r.Diagnostics.Runs.RowsRead = "12345" },
		"rows discarded":   func(r *Report) { r.Diagnostics.Runs.RowsDiscarded = "bogus" },
		"error category":   func(r *Report) { r.Diagnostics.Errors = []ErrorCount{{Category: "bogus", Step: "hooks", Count: 1}} },
		"error step":       func(r *Report) { r.Diagnostics.Errors = []ErrorCount{{Category: "timeout", Step: "bogus", Count: 1}} },
		"role":             func(r *Report) { r.Diagnostics.Users.ByRole[0].Role = "bogus" },
		"temporal version": func(r *Report) { r.Diagnostics.Installation.TemporalVersion = "temporal.corp.internal" },
		"engine major":     func(r *Report) { r.Diagnostics.SourceEngines[0].Major = "prod-1" },
		"husonym version":  func(r *Report) { r.Version.Husonym = "my build" },
		"day":              func(r *Report) { r.Day = "yesterday" },
		"generated_at":     func(r *Report) { r.GeneratedAt = "2026-10-07T00:05:12+02:00" },
		"fingerprint":      func(r *Report) { r.Identification.KeyFingerprint = "abc" },
		"instance id":      func(r *Report) { r.Identification.InstanceID = "my-host" },
		"negative count":   func(r *Report) { r.Sources.Count = -1 },
		"schema version":   func(r *Report) { r.SchemaVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			r := fullReport()
			mutate(r)
			document, err := r.Marshal()
			require.NoError(t, err)
			require.Error(t, Validate(document))
		})
	}
}

func Test_Marshal_GivesTheSameBytesWhateverTheOrder(t *testing.T) {
	want, err := fullReport().Marshal()
	require.NoError(t, err)

	shuffled := fullReport()
	d := shuffled.Diagnostics
	d.Errors = []ErrorCount{{Category: "timeout", Step: "hooks", Count: 1}, {Category: "canceled", Step: "table_sync", Count: 2}}
	reference := fullReport()
	reference.Diagnostics.Errors = slices.Clone(d.Errors)
	reference.Diagnostics.Errors[0], reference.Diagnostics.Errors[1] = reference.Diagnostics.Errors[1], reference.Diagnostics.Errors[0]
	wantErrors, err := reference.Marshal()
	require.NoError(t, err)

	rng := rand.New(rand.NewPCG(1, 2))
	for range 20 {
		rng.Shuffle(len(d.Connections), func(i, j int) { d.Connections[i], d.Connections[j] = d.Connections[j], d.Connections[i] })
		rng.Shuffle(
			len(d.SourceEngines),
			func(i, j int) { d.SourceEngines[i], d.SourceEngines[j] = d.SourceEngines[j], d.SourceEngines[i] },
		)
		rng.Shuffle(len(d.Jobs.ByKind), func(i, j int) { d.Jobs.ByKind[i], d.Jobs.ByKind[j] = d.Jobs.ByKind[j], d.Jobs.ByKind[i] })
		rng.Shuffle(len(d.Transformers.System), func(i, j int) {
			d.Transformers.System[i], d.Transformers.System[j] = d.Transformers.System[j], d.Transformers.System[i]
		})
		rng.Shuffle(len(d.ColumnTypes), func(i, j int) { d.ColumnTypes[i], d.ColumnTypes[j] = d.ColumnTypes[j], d.ColumnTypes[i] })
		rng.Shuffle(len(d.Features), func(i, j int) { d.Features[i], d.Features[j] = d.Features[j], d.Features[i] })
		rng.Shuffle(len(d.Refusals), func(i, j int) { d.Refusals[i], d.Refusals[j] = d.Refusals[j], d.Refusals[i] })
		rng.Shuffle(
			len(d.Runs.ByStatus),
			func(i, j int) { d.Runs.ByStatus[i], d.Runs.ByStatus[j] = d.Runs.ByStatus[j], d.Runs.ByStatus[i] },
		)
		rng.Shuffle(len(d.Errors), func(i, j int) { d.Errors[i], d.Errors[j] = d.Errors[j], d.Errors[i] })
		rng.Shuffle(len(d.Users.ByRole), func(i, j int) { d.Users.ByRole[i], d.Users.ByRole[j] = d.Users.ByRole[j], d.Users.ByRole[i] })

		got, err := shuffled.Marshal()
		require.NoError(t, err)
		// The errors differ from the full report's, so compare all but them to want, and them apart.
		gotErrors, err := reference.Marshal()
		require.NoError(t, err)
		require.Equal(t, wantErrors, gotErrors)
		var a, b map[string]any
		require.NoError(t, json.Unmarshal(got, &a))
		require.NoError(t, json.Unmarshal(want, &b))
		delete(a["diagnostics"].(map[string]any), "errors")
		delete(b["diagnostics"].(map[string]any), "errors")
		require.Equal(t, b, a)
	}
}

func Test_Marshal_SortsByKeysAndLeavesTheReportAlone(t *testing.T) {
	r := fullReport()
	before := slices.Clone(r.Diagnostics.Connections)
	document, err := r.Marshal()
	require.NoError(t, err)
	require.Equal(t, before, r.Diagnostics.Connections)

	var back Report
	require.NoError(t, json.Unmarshal(document, &back))
	require.Equal(t, "mysql", back.Diagnostics.Connections[0].Type)
	require.Equal(t, "postgres", back.Diagnostics.Connections[1].Type)
	require.Equal(t, "generate", back.Diagnostics.Jobs.ByKind[0].Kind)
	require.False(t, back.Diagnostics.Jobs.ByKind[1].Scheduled)
	require.NotEqual(t, byte('\n'), document[len(document)-1])
}
