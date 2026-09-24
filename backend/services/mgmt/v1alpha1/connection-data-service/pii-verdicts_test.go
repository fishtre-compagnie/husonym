package v1alpha1_connectiondataservice

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// verdicts rend un verdict par colonne de la table : une colonne où le scan n'a rien trouvé
// en a un aussi, tiré de son nom. C'est ce qui permet à l'UI de ne plus rien fusionner.
func Test_verdicts(t *testing.T) {
	column := func(name, dataType string) *mgmtv1alpha1.DatabaseColumn {
		return &mgmtv1alpha1.DatabaseColumn{Schema: "public", Table: "users", Column: name, DataType: dataType}
	}
	note := &mgmtv1alpha1.ColumnPiiDetection{
		Schema: "public", Table: "users", Column: "note",
		IsSensitive:        true,
		DataCategory:       "person_name",
		PiiConfidence:      mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
		PiiDetectionMethod: mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
	}
	summarize := func(vs []*mgmtv1alpha1.ColumnPiiVerdict) map[string]mgmtv1alpha1.PiiDetectionMethod {
		out := map[string]mgmtv1alpha1.PiiDetectionMethod{}
		for _, v := range vs {
			out[v.GetColumn()] = v.GetPiiDetectionMethod()
		}
		return out
	}

	t.Run("une colonne par colonne de la table, le nom et le contenu réconciliés", func(t *testing.T) {
		tableColumns := []*mgmtv1alpha1.DatabaseColumn{column("id", "uuid"), column("email", "text"), column("note", "text")}
		got := verdicts("public", "users", tableColumns, nil, []*mgmtv1alpha1.ColumnPiiDetection{note})

		require.Equal(t, map[string]mgmtv1alpha1.PiiDetectionMethod{
			"id":    mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_UNSPECIFIED,
			"email": mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME,
			"note":  mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
		}, summarize(got))
		// La détection par nom est écrite sur une copie : les colonnes de l'appelant restent intactes.
		require.False(t, tableColumns[1].GetIsSensitive())
	})

	t.Run("seulement les colonnes demandées", func(t *testing.T) {
		tableColumns := []*mgmtv1alpha1.DatabaseColumn{column("id", "uuid"), column("email", "text"), column("note", "text")}
		got := verdicts("public", "users", tableColumns, map[string]struct{}{"note": {}}, []*mgmtv1alpha1.ColumnPiiDetection{note})

		require.Equal(t, map[string]mgmtv1alpha1.PiiDetectionMethod{
			"note": mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
		}, summarize(got))
	})

	t.Run("sans le schéma de la table, les colonnes où le scan a trouvé quelque chose", func(t *testing.T) {
		email := &mgmtv1alpha1.ColumnPiiDetection{
			Schema: "public", Table: "users", Column: "email",
			IsSensitive:        true,
			PiiConfidence:      mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
			PiiDetectionMethod: mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
		}
		got := verdicts("public", "users", nil, nil, []*mgmtv1alpha1.ColumnPiiDetection{note, email})

		// Le nom se lit encore sans le type : email l'emporte par son nom.
		require.Equal(t, map[string]mgmtv1alpha1.PiiDetectionMethod{
			"email": mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME,
			"note":  mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT,
		}, summarize(got))
		require.Equal(t, "public", got[0].GetSchema())
		require.Equal(t, "users", got[0].GetTable())
	})
}
