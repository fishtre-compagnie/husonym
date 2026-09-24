package piidetect

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Reconcile decides between what a column's name says and what its content showed. It is
// the one place that rule lives: the UI, the CLI and an agent all read its verdict, so the
// same column is never described two ways.
//
// column carries the name-based detection, as Enrich leaves it; content is what the scan
// found in the column, or nil when it found nothing or did not run.
//
//   - A name that establishes the nature of the data wins, unless the content leaves the
//     format in doubt: "it is a birth date" does not tell dd/mm from mm/dd, and rewriting it
//     in the wrong one corrupts the target.
//   - Otherwise the content decides.
//   - Otherwise the column is not personal data.
func Reconcile(
	column *mgmtv1alpha1.DatabaseColumn,
	content *mgmtv1alpha1.ColumnPiiDetection,
) *mgmtv1alpha1.ColumnPiiVerdict {
	verdict := &mgmtv1alpha1.ColumnPiiVerdict{
		Schema: column.GetSchema(),
		Table:  column.GetTable(),
		Column: column.GetColumn(),
	}
	switch {
	case column.GetIsSensitive():
		verdict.IsSensitive = true
		verdict.DataCategory = column.GetDataCategory()
		verdict.SuggestedTransformerSource = column.GetSuggestedTransformerSource()
		verdict.PiiConfidence = column.GetPiiConfidence()
		verdict.PiiDetectionMethod = column.GetPiiDetectionMethod()
		verdict.PiiEvidence = column.GetPiiEvidence()
		if leavesFormatInDoubt(content) {
			verdict.PiiConfidence = content.GetPiiConfidence()
			verdict.PiiDetectionMethod = content.GetPiiDetectionMethod()
			verdict.PiiEvidence = content.GetPiiEvidence()
		}
	case content != nil:
		verdict.IsSensitive = content.GetIsSensitive()
		verdict.DataCategory = content.GetDataCategory()
		verdict.SuggestedTransformerSource = content.GetSuggestedTransformerSource()
		verdict.PiiConfidence = content.GetPiiConfidence()
		verdict.PiiDetectionMethod = content.GetPiiDetectionMethod()
		verdict.PiiEvidence = content.GetPiiEvidence()
	}
	return verdict
}

func leavesFormatInDoubt(content *mgmtv1alpha1.ColumnPiiDetection) bool {
	return content.GetPiiConfidence() == mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW &&
		content.GetPiiDetectionMethod() == mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_FORMAT
}
