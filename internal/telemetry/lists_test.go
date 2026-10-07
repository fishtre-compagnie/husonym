package telemetry

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

var update = flag.Bool("update", false, "rewrite the schema file from the closed lists")

const schemaPath = "schema/usage-report.v1.schema.json"

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

func readSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))
	return schema
}

// The enums of the schema come from the closed lists, so that the transformers, the gates and the
// features are kept in one place. When a list grows, run `go test ./internal/telemetry -update`.
func Test_Schema_IsUpToDateWithTheLists(t *testing.T) {
	schema := readSchema(t)
	defs := schema["$defs"].(map[string]any)
	for name, list := range schemaEnums() {
		def, ok := defs[name].(map[string]any)
		require.True(t, ok, "the schema has no definition %q", name)
		def["enum"] = list
	}
	want, err := json.MarshalIndent(schema, "", "  ")
	require.NoError(t, err)
	want = append(want, '\n')

	if *update {
		require.NoError(t, os.WriteFile(schemaPath, want, 0o644))
	}
	got, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	require.True(t, bytes.Equal(want, got), "the schema file is out of date: run go test ./internal/telemetry -update")
}

// Every list is the enum of the schema and the schema holds no enum without a list, both ways.
func Test_Lists_AreTheEnumsOfTheSchema(t *testing.T) {
	defs := readSchema(t)["$defs"].(map[string]any)
	lists := schemaEnums()
	for name, def := range defs {
		if name == "count" {
			continue
		}
		list, ok := lists[name]
		require.True(t, ok, "the schema enum %q has no list", name)
		var enum []string
		for _, v := range def.(map[string]any)["enum"].([]any) {
			enum = append(enum, v.(string))
		}
		require.Equal(t, sortedCopy(list), sortedCopy(enum), name)
	}
	for name := range lists {
		require.Contains(t, defs, name)
	}
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

func Test_TransformerName(t *testing.T) {
	for source, want := range map[string]string{
		"generate_email":                    "generate_email",
		"TRANSFORMER_SOURCE_GENERATE_EMAIL": "generate_email",
		"TRANSFORMER_SOURCE_PASSTHROUGH":    "passthrough",
		"TRANSFORMER_SOURCE_USER_DEFINED":   "other",
		"TRANSFORMER_SOURCE_UNSPECIFIED":    "other",
		"my_secret_transformer":             "other",
		"":                                  "other",
	} {
		require.Equal(t, want, TransformerName(source), source)
	}
	for number, name := range mgmtv1alpha1.TransformerSource_name {
		source := mgmtv1alpha1.TransformerSource(number)
		if source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED ||
			source == mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_USER_DEFINED {
			continue
		}
		require.Contains(t, TransformerNames, TransformerName(name))
		require.NotEqual(t, "other", TransformerName(name), name)
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
		"INTEGER": "integer", "customer_status": "other", "": "other",
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
