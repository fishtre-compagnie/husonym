package job

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/internal/transformers/catalog"
)

// LooksSensitive reports whether the name and type of a column read as personal data, and under
// which category.
//
// The detection reads the column's name and type, never its data, so it costs nothing and is
// deterministic, which is what lets a Temporal activity call it during a replay.
//
// What it cannot do is clear a column. `false` means the heuristic recognized nothing — a column
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

// SuggestedTransformer returns the transformer the PII detection suggests for a column, by its
// name and type, with the category it recognized — the suggestion the product makes wherever a
// column is to be mapped. False when it suggests nothing.
//
// A suggestion the column's type does not take is no suggestion. The detection reads the name
// first: `state`, `gender` or `city` name a string generator whatever the column holds, so a
// `state smallint` would be handed GenerateState and every batch would fail on "CA". Where a
// person picks from a list the UI has already filtered by type, this is the same filter.
func SuggestedTransformer(columnName, dataType string) (mgmtv1alpha1.TransformerSource, string, bool) {
	none := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED
	classification, ok := piidetect.Classify(columnName, dataType)
	if !ok || classification.Suggested == none {
		return none, "", false
	}
	// The whole catalogue, enterprise transformers included: what a license allows is the
	// caller's business, the types a transformer takes are not.
	transformer, ok := catalog.BySource(true)[classification.Suggested]
	if !ok || !acceptsDataType(transformer.GetDataTypes(), TransformerDataTypeOf(dataType)) {
		return none, "", false
	}
	return classification.Suggested, classification.Category, true
}
