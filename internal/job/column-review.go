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
