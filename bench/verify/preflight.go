package verify

import (
	"fmt"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/cases"
)

// PreflightChanges tells how the pre-flight report of a run differs from what the case
// expects: an expected finding missing, or a blocking finding or a warning about the plan
// the case does not expect. The report is nil when the run kept none. container is where
// the tables of the case are, the schema a finding names them in.
func PreflightChanges(
	expected []cases.ExpectedFinding,
	container string,
	report *mgmtv1alpha1.PreflightReport,
) []string {
	if report == nil {
		if len(expected) == 0 {
			return nil
		}
		return []string{"pré-vol : le run n'a gardé aucun rapport"}
	}
	var changes []string
	matched := make([]bool, len(report.GetFindings()))
	for _, want := range expected {
		table := ""
		if want.Table != "" {
			table = container + "." + want.Table
		}
		found := false
		for i, f := range report.GetFindings() {
			if f.GetKind() == want.Kind && f.GetLevel() == want.Level && f.GetTable() == table {
				matched[i] = true
				found = true
			}
		}
		if !found {
			changes = append(changes, fmt.Sprintf("pré-vol : constat attendu absent : %s %s sur %q",
				levelName(want.Level), kindName(want.Kind), table))
		}
	}
	for i, f := range report.GetFindings() {
		if matched[i] || !checked(f) {
			continue
		}
		changes = append(changes, fmt.Sprintf("pré-vol : constat inattendu : %s %s sur %q : %s",
			levelName(f.GetLevel()), kindName(f.GetKind()), f.GetTable(), f.GetMessage()))
	}
	slices.Sort(changes)
	return changes
}

// checked reports whether a finding must be expected to be reported: a blocking finding or
// a warning about the plan. What a connection lacks is what the rights cases are about,
// through the message the run stops with.
func checked(f *mgmtv1alpha1.PreflightFinding) bool {
	if f.GetLevel() != mgmtv1alpha1.PreflightFinding_LEVEL_BLOCKING &&
		f.GetLevel() != mgmtv1alpha1.PreflightFinding_LEVEL_WARNING {
		return false
	}
	return f.GetKind() >= mgmtv1alpha1.PreflightFinding_KIND_ENGINE_UNSUPPORTED
}

func kindName(kind mgmtv1alpha1.PreflightFinding_Kind) string {
	return strings.ToLower(strings.TrimPrefix(kind.String(), "KIND_"))
}

func levelName(level mgmtv1alpha1.PreflightFinding_Level) string {
	return strings.ToLower(strings.TrimPrefix(level.String(), "LEVEL_"))
}
