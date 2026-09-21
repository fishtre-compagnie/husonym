package job

import (
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
)

// LooksSensitive reports whether the name and type of a column read as personal data, and under
// which category.
//
// It is the shared verdict of the two places that talk about an unmapped column — the validator,
// which warns while the job is being configured, and the builder, which logs while the run
// copies it. They must agree: a column the form calls personal and the run does not would leave
// nobody able to say which is right. Calling one function is how they stay that way.
//
// The detection reads the column's name and type, never its data, so it costs nothing and is
// deterministic, which is what lets a Temporal activity call it during a replay.
//
// What it cannot do is clear a column. `false` means the heuristic recognised nothing — a column
// named `champ_libre`, `c_42` or `data` returns false and may well hold a person's address.
// Absence of detection is not evidence of innocuousness, so a caller may use this to say which
// column to look at first, never to decide that the others need no looking at.
func LooksSensitive(columnName, dataType string) (category string, sensitive bool) {
	classification, ok := piidetect.Classify(columnName, dataType)
	if !ok || !classification.Sensitive {
		return "", false
	}
	return classification.Category, true
}

// AcceptedPassthrough is a decision somebody made about one column: its passthrough is fine.
//
// It carries the column as it stood when the decision was made, because that is what was
// decided about. A decision is not a property of a name — a free-text field renamed, retyped or
// repurposed into something that holds addresses is a different column wearing the same label,
// and the most ordinary way for a job to start leaking after having been signed off.
type AcceptedPassthrough struct {
	DataType    string
	PiiCategory string
}

// StillHoldsFor reports whether the decision was made about the column as it is now.
func (a AcceptedPassthrough) StillHoldsFor(columnName, dataType string) bool {
	if a.DataType != dataType {
		return false
	}
	category, _ := LooksSensitive(columnName, dataType)
	return a.PiiCategory == category
}
