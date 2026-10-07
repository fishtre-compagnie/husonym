package telemetry

import (
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// other is what any value outside of its closed list becomes.
const other = "other"

// The closed lists of the usage report. A value of the report is a number, a boolean, a date or a
// member of one of these lists, and every input that is not a number passes through one of the
// functions of this file before it reaches the report.
var (
	// JobKinds are the kinds of job a run can belong to.
	JobKinds = []string{"sync", "generate", "ai_generate", "pii_detect"}
	// RunStatuses are the statuses of a run.
	RunStatuses = []string{"running", "completed", "failed", "canceled", "terminated", "timed_out"}
	// LicenseStates are the states of the license lifecycle.
	LicenseStates = []string{"none", "valid", "expiring", "grace", "frozen"}
	// ConnectionRoles are the roles a connection plays in a job.
	ConnectionRoles = []string{"source", "destination"}
	// ConnectionTypes are the types of connection, and other.
	ConnectionTypes = []string{
		"postgres", "mysql", "mssql", "mongodb", "dynamodb", "aws-s3", "gcp-cloud-storage", "openai", other,
	}
	// ColumnTypeFamilies are the families a column type is counted under.
	ColumnTypeFamilies = []string{
		"integer", "decimal", "float", "boolean", "text", "binary", "date", "time", "timestamp",
		"interval", "uuid", "json", "xml", "array", "enum", "network", "geometric", other,
	}
	// ErrorCategories are the categories a failure is counted under.
	ErrorCategories = []string{
		"connection_refused", "authentication_refused", "timeout", "constraint_violated",
		"insufficient_privileges", "object_missing", "type_mismatch", "resources_exhausted",
		"canceled", "license", other,
	}
	// ErrorSteps are the steps of a run a failure is counted under.
	ErrorSteps = []string{"preflight", "schema_init", "table_sync", "hooks", "integrity_check", other}
	// RowsBuckets are the bands the number of rows comes out in, in ascending order.
	RowsBuckets = []string{"lt_1k", "lt_10k", "lt_100k", "lt_1m", "lt_10m", "lt_100m", "gte_100m"}
	// InstallKinds are the ways the instance is installed.
	InstallKinds = []string{"helm", "compose", other}
	// OperatingSystems are the operating systems the instance runs on.
	OperatingSystems = []string{"linux", "darwin", "windows", other}
	// Architectures are the processor architectures the instance runs on.
	Architectures = []string{"amd64", "arm64", other}
	// AuthProviders are the identity providers the instance authenticates with.
	AuthProviders = []string{"auth0", "keycloak", other}
	// RunLogSinks are the places the logs of a run are read from.
	RunLogSinks = []string{"none", "loki", "k8s_pods", other}

	// Roles are the roles of an account, plus none for a user who has no role and other.
	Roles = protoNames(mgmtv1alpha1.AccountRole_name, "ACCOUNT_ROLE_", []string{"none"}, []string{other})
	// TransformerNames are the names of the system transformers, plus other. A source that is
	// unspecified or defined by the user is no system transformer: it is counted apart.
	TransformerNames = protoNames(
		mgmtv1alpha1.TransformerSource_name, "TRANSFORMER_SOURCE_", nil, []string{other},
		"UNSPECIFIED", "USER_DEFINED",
	)
)

// protoNames is the lower-cased names of an enum without their prefix, in the order of their
// numbers, framed by the names before and after; skipped names (without prefix) are left out.
func protoNames(names map[int32]string, prefix string, before, after []string, skipped ...string) []string {
	numbers := make([]int32, 0, len(names))
	for number := range names {
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	list := slices.Clone(before)
	for _, number := range numbers {
		name := strings.TrimPrefix(names[number], prefix)
		if name == "UNSPECIFIED" || slices.Contains(skipped, name) {
			continue
		}
		list = append(list, strings.ToLower(name))
	}
	return append(list, after...)
}

// Gates are the gates a license refuses by, as license.AllGates names them.
func Gates() []string {
	gates := license.AllGates()
	list := make([]string, 0, len(gates))
	for _, gate := range gates {
		list = append(list, string(gate))
	}
	return list
}

// Features are the optional capabilities of a license, as license.AllFeatures names them.
func Features() []string {
	features := license.AllFeatures()
	list := make([]string, 0, len(features))
	for _, feature := range features {
		list = append(list, string(feature))
	}
	return list
}

// member is the lower-cased raw value when the list holds it, other otherwise.
func member(list []string, raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if slices.Contains(list, value) {
		return value
	}
	return other
}

// InstallKind is how the instance is installed: helm, compose or other.
func InstallKind(raw string) string { return member(InstallKinds, raw) }

// OperatingSystem is the operating system, as runtime.GOOS spells it, or other.
func OperatingSystem(raw string) string { return member(OperatingSystems, raw) }

// Architecture is the processor architecture, as runtime.GOARCH spells it, or other.
func Architecture(raw string) string { return member(Architectures, raw) }

// AuthProvider is the identity provider: auth0, keycloak or other.
func AuthProvider(raw string) string { return member(AuthProviders, raw) }

// RunLogs is where the logs of a run are read from: none, loki, k8s_pods or other. The empty
// value is none, and the chart's k8s-pods is k8s_pods.
func RunLogs(raw string) string {
	value := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "-", "_")
	if value == "" {
		return "none"
	}
	return member(RunLogSinks, value)
}

// ConnectionType is the type of a connection as the API names it, or other.
func ConnectionType(raw string) string { return member(ConnectionTypes, raw) }

// ConnectionRole is source or destination; anything else is not a role of the report, so the
// result is empty and the caller leaves the row out.
func ConnectionRole(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if slices.Contains(ConnectionRoles, value) {
		return value
	}
	return ""
}

// TransformerName is the lower-cased name of a system transformer source, given with or without
// its TRANSFORMER_SOURCE_ prefix, or other.
func TransformerName(source string) string {
	value := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(source)), "transformer_source_")
	return member(TransformerNames, value)
}

// RowsBucket is the band a number of rows falls in; a negative number is in the lowest band.
func RowsBucket(n int64) string {
	for i, limit := range []int64{1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000} {
		if n < limit {
			return RowsBuckets[i]
		}
	}
	return RowsBuckets[len(RowsBuckets)-1]
}
