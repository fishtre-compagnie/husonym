package telemetry

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

var update = flag.Bool("update", false, "rewrite the generated files (the schema from the closed lists)")

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

func Test_HusonymVersion(t *testing.T) {
	for raw, want := range map[string]string{
		"v0.3.0": "v0.3.0", "0.3.0": "0.3.0", "v0.3.0-rc.1": "v0.3.0-rc.1", "v0.0.0-main": "v0.0.0-main",
		"main": "other", "feat/usage": "other", "1.2.3-db.prod.customer.example.com": "other", "": "other",
	} {
		require.Equal(t, want, HusonymVersion(raw), raw)
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

// Every object of the schema is closed and every string is constrained, so that nobody can add
// an unconstrained string without a test failing.
func Test_Schema_ClosesEveryObjectAndConstrainsEveryString(t *testing.T) {
	walkSchema(readSchema(t), "", func(path string, node map[string]any) {
		switch node["type"] {
		case "object":
			require.Equal(t, false, node["additionalProperties"], "%s is an open object", path)
		case "string":
			_, enum := node["enum"]
			_, constant := node["const"]
			pattern, _ := node["pattern"].(string)
			if !enum && !constant {
				require.True(t, strings.HasPrefix(pattern, "^") && strings.HasSuffix(pattern, "$"),
					"%s is a string without enum, const or anchored pattern", path)
			}
		}
	})
}
