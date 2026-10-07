package usagestore

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

func names[T ~string](values []T) []string {
	list := make([]string, 0, len(values))
	for _, v := range values {
		list = append(list, string(v))
	}
	return list
}

// The usage report names job kinds and run statuses by closed lists of its own, which cannot
// import this package; they must be the values the store lists, plus other.
func Test_TheUsageReportLists_AreTheKindsAndStatusesOfTheStore(t *testing.T) {
	require.ElementsMatch(t, append(names(JobKinds()), "other"), telemetry.JobKinds)
	require.ElementsMatch(t, append(names(Statuses()), "other"), telemetry.RunStatuses)
}

var (
	constantValue = regexp.MustCompile(`(?m)^\s*(?:JobKind|Status)\w+\s+(JobKind|Status)\s*=\s*"([a-z_]+)"`)
	checkValues   = regexp.MustCompile(`(?s)(job_kind|status) IN \(([^)]*)\)`)
	quoted        = regexp.MustCompile(`'([a-z_]+)'`)
)

// A constant that the functions forget, or a value the table does not allow, fails here: the
// constants of store.go and the CHECK constraints of the migration must be the functions' lists.
func Test_TheStoreLists_ListEveryConstantAndEveryValueOfTheTable(t *testing.T) {
	source, err := os.ReadFile("store.go")
	require.NoError(t, err)
	var kinds, statuses []string
	for _, m := range constantValue.FindAllStringSubmatch(string(source), -1) {
		if m[1] == "JobKind" {
			kinds = append(kinds, m[2])
		} else {
			statuses = append(statuses, m[2])
		}
	}
	require.NotEmpty(t, kinds)
	require.NotEmpty(t, statuses)
	require.ElementsMatch(t, kinds, names(JobKinds()), "a JobKind constant is missing from JobKinds()")
	require.ElementsMatch(t, statuses, names(Statuses()), "a Status constant is missing from Statuses()")

	migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "postgresql", "schema", "20261007100000_adds-usage.up.sql"))
	require.NoError(t, err)
	for _, m := range checkValues.FindAllStringSubmatch(string(migration), -1) {
		var values []string
		for _, q := range quoted.FindAllStringSubmatch(m[2], -1) {
			values = append(values, q[1])
		}
		if m[1] == "job_kind" {
			require.ElementsMatch(t, values, names(JobKinds()))
		} else {
			require.ElementsMatch(t, values, names(Statuses()))
		}
	}
}
