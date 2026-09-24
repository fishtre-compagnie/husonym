package piidetect

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestReconcile(t *testing.T) {
	named := func(column, dataType string) *mgmtv1alpha1.DatabaseColumn {
		c := &mgmtv1alpha1.DatabaseColumn{Schema: "public", Table: "users", Column: column, DataType: dataType}
		Enrich([]*mgmtv1alpha1.DatabaseColumn{c})
		return c
	}
	formatDoubt := &mgmtv1alpha1.ColumnPiiDetection{
		Column:             "date_naissance",
		IsSensitive:        true,
		DataCategory:       "date",
		PiiConfidence:      mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
		PiiDetectionMethod: mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_FORMAT,
		PiiEvidence:        "format ambigu sur 14 valeurs : jj/mm/aaaa ou mm/jj/aaaa",
	}
	byChecksum := &mgmtv1alpha1.ColumnPiiDetection{
		Column:                     "reference",
		IsSensitive:                true,
		DataCategory:               "nir",
		SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN,
		PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
		PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CHECKSUM,
		PiiEvidence:                "NIR vérifié sur 18/18 valeurs",
	}

	cases := []struct {
		name    string
		column  *mgmtv1alpha1.DatabaseColumn
		content *mgmtv1alpha1.ColumnPiiDetection
		want    *mgmtv1alpha1.ColumnPiiVerdict
	}{
		{
			name:   "the name alone, when no scan ran",
			column: named("email", "text"),
			want: &mgmtv1alpha1.ColumnPiiVerdict{
				Schema: "public", Table: "users", Column: "email",
				IsSensitive:                true,
				DataCategory:               "email",
				SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL,
				PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
				PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME,
				PiiEvidence:                "reconnu par le nom de colonne « email »",
			},
		},
		{
			name:    "the name wins over the content",
			column:  named("email", "text"),
			content: byChecksum,
			want: &mgmtv1alpha1.ColumnPiiVerdict{
				Schema: "public", Table: "users", Column: "email",
				IsSensitive:                true,
				DataCategory:               "email",
				SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL,
				PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
				PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME,
				PiiEvidence:                "reconnu par le nom de colonne « email »",
			},
		},
		{
			name:    "unless the content leaves the format in doubt",
			column:  named("date_naissance", "text"),
			content: formatDoubt,
			want: func() *mgmtv1alpha1.ColumnPiiVerdict {
				c := named("date_naissance", "text")
				return &mgmtv1alpha1.ColumnPiiVerdict{
					Schema: "public", Table: "users", Column: "date_naissance",
					IsSensitive:                true,
					DataCategory:               c.GetDataCategory(),
					SuggestedTransformerSource: c.GetSuggestedTransformerSource(),
					PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
					PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_FORMAT,
					PiiEvidence:                "format ambigu sur 14 valeurs : jj/mm/aaaa ou mm/jj/aaaa",
				}
			}(),
		},
		{
			name:    "the content decides when the name says nothing",
			column:  named("reference", "text"),
			content: byChecksum,
			want: &mgmtv1alpha1.ColumnPiiVerdict{
				Schema: "public", Table: "users", Column: "reference",
				IsSensitive:                true,
				DataCategory:               "nir",
				SuggestedTransformerSource: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN,
				PiiConfidence:              mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
				PiiDetectionMethod:         mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CHECKSUM,
				PiiEvidence:                "NIR vérifié sur 18/18 valeurs",
			},
		},
		{
			name:   "neither: not personal data",
			column: named("quantity", "integer"),
			want:   &mgmtv1alpha1.ColumnPiiVerdict{Schema: "public", Table: "users", Column: "quantity"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Reconcile(tc.column, tc.content)
			require.True(t, proto.Equal(tc.want, got), "want %v\ngot  %v", tc.want, got)
		})
	}
}
