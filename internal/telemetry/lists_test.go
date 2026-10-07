package telemetry

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

var update = flag.Bool("update", false, "rewrite the generated files (the schema from the closed lists)")

const (
	schemaPath       = "schema/usage-report.v1.schema.json"
	periodSchemaPath = "schema/usage-period-report.v1.schema.json"
)

// schemaEnums are the closed lists the schema holds as an enum under $defs, by definition name.
func schemaEnums() map[string][]string {
	return map[string][]string{
		"license_state":      LicenseStates,
		"install_kind":       InstallKinds,
		"operating_system":   OperatingSystems,
		"architecture":       Architectures,
		"auth_provider":      AuthProviders,
		"run_logs":           RunLogSinks,
		"connection_type":    ConnectionTypes,
		"connection_role":    ConnectionRoles,
		"job_kind":           JobKinds,
		"transformer_name":   TransformerNames,
		"column_type_family": ColumnTypeFamilies,
		"feature":            Features(),
		"gate":               Gates(),
		"run_status":         RunStatuses,
		"rows_bucket":        RowsBuckets,
		"error_category":     ErrorCategories,
		"error_step":         ErrorSteps,
		"role":               Roles,
	}
}

func readSchema(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	return schema
}

// requireSchemaFile checks that the file is the schema, in its canonical form; with -update it
// writes it first.
func requireSchemaFile(t *testing.T, path string, schema map[string]any) {
	t.Helper()
	want, err := json.MarshalIndent(schema, "", "  ")
	require.NoError(t, err)
	want = append(want, '\n')

	if *update {
		require.NoError(t, os.WriteFile(path, want, 0o644))
	}
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(want, got), "%s is out of date: run go test ./internal/telemetry -update", path)
}

// dailySchema is the schema of the report of a day, its enums taken from the closed lists.
func dailySchema(t *testing.T) map[string]any {
	t.Helper()
	schema := readSchema(t, schemaPath)
	defs := schema["$defs"].(map[string]any)
	for name, list := range schemaEnums() {
		def, ok := defs[name].(map[string]any)
		require.True(t, ok, "the schema has no definition %q", name)
		def["enum"] = list
	}
	return schema
}

// monthStateBlocks are the names of the blocks of MonthState, in the order of its fields.
func monthStateBlocks() []string {
	return jsonFields(reflect.TypeFor[MonthState]())
}

// jsonFields are the names the fields of a struct are written under.
func jsonFields(of reflect.Type) []string {
	names := make([]string, 0, of.NumField())
	for i := range of.NumField() {
		name, _, _ := strings.Cut(of.Field(i).Tag.Get("json"), ",")
		names = append(names, name)
	}
	return names
}

// periodSchema is the schema of the report for a period. Its file says of its own only how a
// period and a month are laid out: every block the two documents share, and every definition
// those blocks name, is the one of the schema of the day.
func periodSchema(t *testing.T, daily map[string]any) map[string]any {
	t.Helper()
	schema := readSchema(t, periodSchemaPath)
	dailyProperties := daily["properties"].(map[string]any)
	diagnostics := dailyProperties["diagnostics"].(map[string]any)["properties"].(map[string]any)

	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"schema_version", "generated_at", "identification"} {
		properties[name] = dailyProperties[name]
	}
	month := properties["months"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	month["version"], month["sources"] = dailyProperties["version"], dailyProperties["sources"]
	month["runs"], month["refusals"] = diagnostics["runs"], diagnostics["refusals"]
	state := map[string]any{}
	for _, block := range monthStateBlocks() {
		require.Contains(t, diagnostics, block)
		state[block] = diagnostics[block]
	}
	month["state"] = map[string]any{
		"additionalProperties": false, "properties": state, "required": monthStateBlocks(), "type": "object",
	}

	delete(schema, "$defs")
	dailyDefs, defs := daily["$defs"].(map[string]any), map[string]any{}
	walkSchema(schema, "", func(path string, node map[string]any) {
		ref, ok := node["$ref"].(string)
		if !ok {
			return
		}
		name := strings.TrimPrefix(ref, "#/$defs/")
		require.Contains(t, dailyDefs, name, "%s names a definition the schema of the day has not", path)
		defs[name] = dailyDefs[name]
	})
	schema["$defs"] = defs
	return schema
}

// The enums of the schemas come from the closed lists, so that the transformers, the gates and
// the features are kept in one place, and what the report for a period shares with the report of
// a day comes from the schema of the day. When a list grows, or the schema of the day changes,
// run `go test ./internal/telemetry -update`.
func Test_Schema_IsUpToDateWithTheLists(t *testing.T) {
	daily := dailySchema(t)
	requireSchemaFile(t, schemaPath, daily)
	requireSchemaFile(t, periodSchemaPath, periodSchema(t, daily))
}

// enumOf reads the enum of a definition of a schema.
func enumOf(def any) []string {
	var enum []string
	for _, v := range def.(map[string]any)["enum"].([]any) {
		enum = append(enum, v.(string))
	}
	return enum
}

// Every list is the enum of the schema and the schema holds no enum without a list, both ways.
// The schema of a period holds the lists its blocks name, and no enum without a list either.
func Test_Lists_AreTheEnumsOfTheSchema(t *testing.T) {
	lists := schemaEnums()
	for _, path := range []string{schemaPath, periodSchemaPath} {
		for name, def := range readSchema(t, path)["$defs"].(map[string]any) {
			if name == "count" {
				continue
			}
			list, ok := lists[name]
			require.True(t, ok, "the enum %q of %s has no list", name, path)
			require.Equal(t, sortedCopy(list), sortedCopy(enumOf(def)), "%s of %s", name, path)
		}
	}
	defs := readSchema(t, schemaPath)["$defs"].(map[string]any)
	for name := range lists {
		require.Contains(t, defs, name)
	}
}

// The state of a month is the diagnostic without what is counted over a day: a block added to
// one has to be placed in, or kept out of, the other.
func Test_MonthState_HoldsTheBlocksOfTheDiagnosticThatTellAState(t *testing.T) {
	require.ElementsMatch(t,
		append(monthStateBlocks(), "runs", "refusals", "source_engines", "errors"),
		jsonFields(reflect.TypeFor[Diagnostics]()))
}

func Test_Lists_HaveNoDuplicate(t *testing.T) {
	for name, list := range schemaEnums() {
		require.Len(t, slices.Compact(sortedCopy(list)), len(list), name)
		require.NotEmpty(t, list, name)
	}
}

func Test_Roles_AreTheAccountRolesOfTheProto(t *testing.T) {
	var want []string
	for number, name := range mgmtv1alpha1.AccountRole_name {
		if number != int32(mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED) {
			want = append(want, name)
		}
	}
	require.Len(t, Roles, len(want)+2)
	require.Contains(t, Roles, "admin")
	require.Contains(t, Roles, "job_developer")
	require.Equal(t, "none", Roles[0])
	require.Equal(t, "other", Roles[len(Roles)-1])
}

func Test_Role(t *testing.T) {
	for role, want := range map[mgmtv1alpha1.AccountRole]string{
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN:         "admin",
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER: "job_developer",
		mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED:   "none",
		mgmtv1alpha1.AccountRole(9999):                      "other",
	} {
		require.Equal(t, want, Role(role), role)
	}
	for number := range mgmtv1alpha1.AccountRole_name {
		role := mgmtv1alpha1.AccountRole(number)
		require.Contains(t, Roles, Role(role))
		require.NotEqual(t, "other", Role(role), role)
	}
}

func Test_TransformerName(t *testing.T) {
	for source, want := range map[mgmtv1alpha1.TransformerSource]string{
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL: "generate_email",
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH:    "passthrough",
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_USER_DEFINED:   "other",
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED:    "other",
		mgmtv1alpha1.TransformerSource(9999):                             "other",
	} {
		require.Equal(t, want, TransformerName(source), source)
	}
	for number := range mgmtv1alpha1.TransformerSource_name {
		source := mgmtv1alpha1.TransformerSource(number)
		if source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED ||
			source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_USER_DEFINED {
			continue
		}
		require.Contains(t, TransformerNames, TransformerName(source))
		require.NotEqual(t, "other", TransformerName(source), source)
	}
}

func Test_RowsBucket(t *testing.T) {
	for n, want := range map[int64]string{
		-1: "lt_1k", 0: "lt_1k", 999: "lt_1k", 1_000: "lt_10k", 9_999: "lt_10k", 10_000: "lt_100k",
		99_999: "lt_100k", 100_000: "lt_1m", 999_999: "lt_1m", 1_000_000: "lt_10m",
		9_999_999: "lt_10m", 10_000_000: "lt_100m", 99_999_999: "lt_100m",
		100_000_000: "gte_100m", 1 << 60: "gte_100m",
	} {
		require.Equal(t, want, RowsBucket(n), n)
	}
}

func Test_ColumnTypeFamily(t *testing.T) {
	for raw, want := range map[string]string{
		"varchar(255)": "text", "character varying": "text", "int4": "integer",
		"bigint unsigned": "integer", "numeric(10,2)": "decimal", "timestamptz": "timestamp",
		"timestamp(3) with time zone": "timestamp", "jsonb": "json", "uuid": "uuid",
		"text[]": "array", "_int4": "array", "tinyint(1)": "integer", "nvarchar(max)": "text",
		"INTEGER": "integer", "ARRAY": "array", "USER-DEFINED": "other", "customer_status": "other", "": "other",
	} {
		require.Equal(t, want, ColumnTypeFamily(raw), raw)
	}
}

func Test_Buckets_OfFreeValues(t *testing.T) {
	require.Equal(t, "helm", InstallKind("Helm"))
	require.Equal(t, "other", InstallKind(""))
	require.Equal(t, "other", InstallKind("k3s"))
	require.Equal(t, "keycloak", AuthProvider("Keycloak"))
	require.Equal(t, "other", AuthProvider("my-idp.example.com"))
	require.Equal(t, "k8s_pods", RunLogs("k8s-pods"))
	require.Equal(t, "loki", RunLogs("loki"))
	require.Equal(t, "none", RunLogs(""))
	require.Equal(t, "other", RunLogs("elastic"))
	require.Equal(t, "aws-s3", ConnectionType("aws-s3"))
	require.Equal(t, "other", ConnectionType("unknown"))
	require.Equal(t, "other", ConnectionType("prod-db-1"))
	require.Equal(t, "source", ConnectionRole("source"))
	require.Equal(t, "", ConnectionRole("whatever"))
	require.Equal(t, "linux", OperatingSystem("linux"))
	require.Equal(t, "other", OperatingSystem("plan9"))
	require.Equal(t, "arm64", Architecture("arm64"))
	require.Equal(t, "other", Architecture("riscv64"))
	require.Equal(t, "1.25.2", TemporalVersion("1.25.2"))
	require.Equal(t, "", TemporalVersion("1.25.2-rc1"))
	require.Equal(t, "", TemporalVersion("temporal.internal.corp"))
	require.Equal(t, "", TemporalVersion(""))
}

func Test_SourceMajor(t *testing.T) {
	require.Equal(t, "16", SourceMajor("16"))
	require.Equal(t, "8.0", SourceMajor("8.0"))
	require.Equal(t, "", SourceMajor("8.0.36"))
	require.Equal(t, "", SourceMajor("prod-1"))
	require.Equal(t, "", SourceMajor("16\n"))
	require.Equal(t, "", SourceMajor(""))
}

// sortedCopy is the list sorted, to compare lists as sets.
func sortedCopy(list []string) []string {
	c := slices.Clone(list)
	slices.Sort(c)
	return c
}

func Test_ColumnTypeFamilies_AreAllInTheList(t *testing.T) {
	for name, family := range columnTypeFamilies {
		require.Contains(t, ColumnTypeFamilies, family, name)
	}
}

func Test_ColumnTypeTable_ListsNoTypeUnderTwoFamilies(t *testing.T) {
	seen := map[string]string{}
	for family, names := range typeNamesByFamily {
		for _, name := range names {
			previous, twice := seen[name]
			require.False(t, twice, "%q is under %q and %q", name, previous, family)
			seen[name] = family
		}
	}
}

func Test_LicenseStates_AreTheOnesOfTheLicense(t *testing.T) {
	require.ElementsMatch(t, []string{
		string(license.StateNone), string(license.StateValid), string(license.StateExpiring),
		string(license.StateGrace), string(license.StateFrozen), "other",
	}, LicenseStates)
}

func Test_ListMappings_SendWhatIsUnknownToOther(t *testing.T) {
	require.Equal(t, "sync", JobKind("sync"))
	require.Equal(t, "other", JobKind("my job"))
	require.Equal(t, "timed_out", RunStatus("timed_out"))
	require.Equal(t, "other", RunStatus("exploded"))
	require.Equal(t, "frozen", LicenseState("frozen"))
	require.Equal(t, "other", LicenseState("lifetime"))
}

// A release, or a build stamped with a short suffix of two parts at most: a longer tail could
// be the name of a host.
func Test_HusonymVersion(t *testing.T) {
	for raw, want := range map[string]string{
		"v0.3.0": "v0.3.0", "0.3.0": "0.3.0", "v0.3.0-rc.1": "v0.3.0-rc.1", "v0.0.0-main": "v0.0.0-main",
		"v1.2.3-0123456789abcdef.0123456789abcdef": "v1.2.3-0123456789abcdef.0123456789abcdef",
		"main": "other", "feat/usage": "other", "1.2.3-db.prod.customer.example.com": "other", "": "other",
		"v1.2.3-db01.corp.example.com": "other", "v1.2.3-db01.corp.example": "other",
		"v1.2.3-0123456789abcdefg": "other", "v1.2.3-rc.": "other", "v1.2.3-rc_1": "other",
	} {
		require.Equal(t, want, HusonymVersion(raw), raw)
	}
}

// The schema holds the same shape as HusonymVersion: what one refuses, the other does.
func Test_Schema_TakesTheVersionsHusonymVersionTakes(t *testing.T) {
	for _, raw := range []string{
		"v0.3.0", "0.3.0", "v0.3.0-rc.1", "v0.0.0-main", "v1.2.3-0123456789abcdef.0123456789abcdef", "other",
		"main", "v1.2.3-db01.corp.example.com", "v1.2.3-db01.corp.example", "v1.2.3-0123456789abcdefg", "v1.2.3-rc.",
	} {
		report := fullReport()
		report.Version.Husonym = raw
		document, err := report.Marshal()
		require.NoError(t, err)
		if raw == "other" || HusonymVersion(raw) == raw {
			require.NoError(t, Validate(document), raw)
		} else {
			require.Error(t, Validate(document), raw)
		}
	}
}

// The license tool generates an id as 8 random bytes in lowercase hex (randomId in issue.go).
func Test_LicenseId(t *testing.T) {
	for raw, want := range map[string]string{
		"0123456789abcdef": "0123456789abcdef", "contract-42": "other", "0123456789abcdef.corp.example.com": "other",
		"0123456789abcdef0": "other", "0123456789ABCDEF": "other", "": "other",
	} {
		require.Equal(t, want, LicenseId(raw), raw)
	}
}

// walkSchema calls visit on every schema node of the tree, with its path.
func walkSchema(node any, path string, visit func(path string, node map[string]any)) {
	switch n := node.(type) {
	case map[string]any:
		visit(path, n)
		for key, child := range n {
			walkSchema(child, path+"/"+key, visit)
		}
	case []any:
		for i, child := range n {
			walkSchema(child, fmt.Sprintf("%s/%d", path, i), visit)
		}
	}
}

// Every object of the schemas is closed and every string is constrained, so that nobody can add
// an unconstrained string without a test failing.
func Test_Schema_ClosesEveryObjectAndConstrainsEveryString(t *testing.T) {
	for _, file := range []string{schemaPath, periodSchemaPath} {
		objects, constrained := 0, 0
		walkSchema(readSchema(t, file), "", func(path string, node map[string]any) {
			switch node["type"] {
			case "object":
				objects++
				require.Equal(t, false, node["additionalProperties"], "%s%s is an open object", file, path)
			case "string":
				_, enum := node["enum"]
				_, constant := node["const"]
				pattern, _ := node["pattern"].(string)
				if !enum && !constant {
					constrained++
					require.True(t, strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$"),
						"%s%s is a string without enum, const or anchored pattern", file, path)
				}
			}
		})
		require.Greater(t, objects, 10, file)
		require.Greater(t, constrained, 3, file)
	}
}
