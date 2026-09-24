package mcp_server

import (
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// enumLabel turns a proto enum name into the word a model reads: PII_CONFIDENCE_NEEDS_REVIEW
// with its prefix gives needs_review. The unspecified value gives nothing.
func enumLabel(name, prefix string) string {
	label := strings.TrimPrefix(name, prefix)
	if label == "UNSPECIFIED" {
		return ""
	}
	return strings.ToLower(label)
}

func transformerLabel(source mgmtv1alpha1.TransformerSource) string {
	return enumLabel(source.String(), "TRANSFORMER_SOURCE_")
}

func confidenceLabel(confidence mgmtv1alpha1.PiiConfidence) string {
	return enumLabel(confidence.String(), "PII_CONFIDENCE_")
}

func methodLabel(method mgmtv1alpha1.PiiDetectionMethod) string {
	return enumLabel(method.String(), "PII_DETECTION_METHOD_")
}

// transformerSource reads back a transformer label, such as generate_email.
func transformerSource(label string) (mgmtv1alpha1.TransformerSource, bool) {
	value, ok := mgmtv1alpha1.TransformerSource_value["TRANSFORMER_SOURCE_"+strings.ToUpper(label)]
	if !ok || value == int32(mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED) {
		return mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED, false
	}
	return mgmtv1alpha1.TransformerSource(value), true
}
