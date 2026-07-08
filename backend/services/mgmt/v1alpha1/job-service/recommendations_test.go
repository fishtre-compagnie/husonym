package v1alpha1_jobservice

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	v1alpha1_transformersservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/transformers-service"
	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	piidetect_table_activities "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/piidetect/workflows/table/activities"
	"github.com/stretchr/testify/require"
)

func Test_deriveCategoryAndEvidence(t *testing.T) {
	t.Parallel()

	t.Run("regex only", func(t *testing.T) {
		t.Parallel()
		category, confidence, evidence := deriveCategoryAndEvidence(&piidetect_table_activities.ColumnReport{
			ColumnName: "email",
			Report: piidetect_table_activities.CombinedPiiDetectReport{
				Regex: &piidetect_table_activities.RegexPiiDetectReport{
					Category: piidetect_table_activities.PiiCategoryContact,
				},
			},
		})
		require.Equal(t, piidetect_table_activities.PiiCategoryContact, category)
		require.Equal(t, regexDetectionConfidence, confidence)
		require.Len(t, evidence, 1)
		require.Equal(t, mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_REGEX, evidence[0].GetKind())
	})

	t.Run("llm only", func(t *testing.T) {
		t.Parallel()
		category, confidence, evidence := deriveCategoryAndEvidence(&piidetect_table_activities.ColumnReport{
			ColumnName: "notes",
			Report: piidetect_table_activities.CombinedPiiDetectReport{
				LLM: &piidetect_table_activities.LLMPiiDetectReport{
					Category:   piidetect_table_activities.PiiCategoryPersonal,
					Confidence: 0.7,
				},
			},
		})
		require.Equal(t, piidetect_table_activities.PiiCategoryPersonal, category)
		require.Equal(t, float32(0.7), confidence)
		require.Len(t, evidence, 1)
		require.Equal(t, mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_LLM, evidence[0].GetKind())
	})

	t.Run("dictionary only", func(t *testing.T) {
		t.Parallel()
		category, confidence, evidence := deriveCategoryAndEvidence(&piidetect_table_activities.ColumnReport{
			ColumnName: "geburtsdatum",
			Report: piidetect_table_activities.CombinedPiiDetectReport{
				Dictionary: &piidetect_table_activities.DictionaryPiiDetectReport{
					Category:   piidetect_table_activities.PiiCategoryPersonal,
					Token:      "geburtsdatum",
					Confidence: piidetect_table_activities.DictionaryMatchConfidence,
				},
			},
		})
		require.Equal(t, piidetect_table_activities.PiiCategoryPersonal, category)
		require.Equal(t, piidetect_table_activities.DictionaryMatchConfidence, confidence)
		require.Len(t, evidence, 1)
		require.Equal(t, mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_DICTIONARY, evidence[0].GetKind())
	})

	t.Run("highest confidence reporter wins", func(t *testing.T) {
		t.Parallel()
		category, confidence, evidence := deriveCategoryAndEvidence(&piidetect_table_activities.ColumnReport{
			ColumnName: "user_ref",
			Report: piidetect_table_activities.CombinedPiiDetectReport{
				Regex: &piidetect_table_activities.RegexPiiDetectReport{
					Category: piidetect_table_activities.PiiCategoryContact,
				},
				LLM: &piidetect_table_activities.LLMPiiDetectReport{
					Category:   piidetect_table_activities.PiiCategoryNationalId,
					Confidence: 0.95,
				},
			},
		})
		require.Equal(t, piidetect_table_activities.PiiCategoryNationalId, category)
		require.Equal(t, float32(0.95), confidence)
		require.Len(t, evidence, 2)
	})

	t.Run("llm does not override stronger regex", func(t *testing.T) {
		t.Parallel()
		category, confidence, _ := deriveCategoryAndEvidence(&piidetect_table_activities.ColumnReport{
			ColumnName: "email",
			Report: piidetect_table_activities.CombinedPiiDetectReport{
				Regex: &piidetect_table_activities.RegexPiiDetectReport{
					Category: piidetect_table_activities.PiiCategoryContact,
				},
				LLM: &piidetect_table_activities.LLMPiiDetectReport{
					Category:   piidetect_table_activities.PiiCategoryPersonal,
					Confidence: 0.5,
				},
			},
		})
		require.Equal(t, piidetect_table_activities.PiiCategoryContact, category)
		require.Equal(t, regexDetectionConfidence, confidence)
	})
}

func Test_buildColumnRecommendation(t *testing.T) {
	t.Parallel()
	catalog := v1alpha1_transformersservice.GetSystemTransformerCatalog(true)

	t.Run("contact email string column gets TransformEmail", func(t *testing.T) {
		t.Parallel()
		rec, _, _, _ := buildColumnRecommendation(
			"public", "users",
			&piidetect_table_activities.ColumnReport{
				ColumnName: "email",
				Report: piidetect_table_activities.CombinedPiiDetectReport{
					Regex: &piidetect_table_activities.RegexPiiDetectReport{
						Category: piidetect_table_activities.PiiCategoryContact,
					},
				},
			},
			&sqlmanager_shared.DatabaseSchemaRow{DataType: "character varying"},
			catalog,
		)
		require.Equal(t, "public", rec.GetSchema())
		require.Equal(t, "users", rec.GetTable())
		require.Equal(t, "email", rec.GetColumn())
		require.Equal(t, string(piidetect_table_activities.PiiCategoryContact), rec.GetCategory())
		require.NotNil(t, rec.GetRecommendedConfig().GetTransformEmailConfig())
		require.Equal(t, regexDetectionConfidence, rec.GetConfidence())
		require.Nil(t, rec.GetProposal())
		// regex evidence + mapping rationale evidence
		require.Len(t, rec.GetEvidence(), 2)
		require.Equal(t, mgmtv1alpha1.DetectionEvidence_DETECTOR_KIND_DICTIONARY, rec.GetEvidence()[1].GetKind())
		require.NotEmpty(t, rec.GetEvidence()[1].GetDetail())
	})

	t.Run("incompatible data type falls back to passthrough", func(t *testing.T) {
		t.Parallel()
		rec, _, _, _ := buildColumnRecommendation(
			"public", "users",
			&piidetect_table_activities.ColumnReport{
				ColumnName: "email",
				Report: piidetect_table_activities.CombinedPiiDetectReport{
					Regex: &piidetect_table_activities.RegexPiiDetectReport{
						Category: piidetect_table_activities.PiiCategoryContact,
					},
				},
			},
			&sqlmanager_shared.DatabaseSchemaRow{DataType: "bigint"},
			catalog,
		)
		require.NotNil(t, rec.GetRecommendedConfig().GetPassthroughConfig())
	})

	t.Run("generated column forces GenerateDefault", func(t *testing.T) {
		t.Parallel()
		rec, _, _, _ := buildColumnRecommendation(
			"public", "users",
			&piidetect_table_activities.ColumnReport{
				ColumnName: "email",
				Report: piidetect_table_activities.CombinedPiiDetectReport{
					Regex: &piidetect_table_activities.RegexPiiDetectReport{
						Category: piidetect_table_activities.PiiCategoryContact,
					},
				},
			},
			&sqlmanager_shared.DatabaseSchemaRow{
				DataType:      "text",
				GeneratedType: gotypeutil.ToPtr("s"),
			},
			catalog,
		)
		require.NotNil(t, rec.GetRecommendedConfig().GetGenerateDefaultConfig())
	})

	t.Run("identity column forces Passthrough", func(t *testing.T) {
		t.Parallel()
		rec, _, _, _ := buildColumnRecommendation(
			"public", "users",
			&piidetect_table_activities.ColumnReport{
				ColumnName: "id",
				Report: piidetect_table_activities.CombinedPiiDetectReport{
					LLM: &piidetect_table_activities.LLMPiiDetectReport{
						Category:   piidetect_table_activities.PiiCategoryNationalId,
						Confidence: 0.8,
					},
				},
			},
			&sqlmanager_shared.DatabaseSchemaRow{
				DataType:           "bigint",
				IdentityGeneration: gotypeutil.ToPtr("a"),
			},
			catalog,
		)
		require.NotNil(t, rec.GetRecommendedConfig().GetPassthroughConfig())
	})

	t.Run("unknown column info still yields a recommendation", func(t *testing.T) {
		t.Parallel()
		rec, _, _, _ := buildColumnRecommendation(
			"public", "users",
			&piidetect_table_activities.ColumnReport{
				ColumnName: "prenom",
				Report: piidetect_table_activities.CombinedPiiDetectReport{
					LLM: &piidetect_table_activities.LLMPiiDetectReport{
						Category:   piidetect_table_activities.PiiCategoryPersonal,
						Confidence: 0.85,
					},
				},
			},
			nil, // no schema info available (e.g. non-SQL connection)
			catalog,
		)
		// GenerateFirstName only supports string columns; with an unspecified
		// data type the validity filter falls back to passthrough rather than
		// risking a config that would fail ValidateJobMappings.
		require.NotNil(t, rec.GetRecommendedConfig().GetPassthroughConfig())
		require.Equal(t, float32(0.85), rec.GetConfidence())
	})
}
