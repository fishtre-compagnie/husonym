package v1alpha1_connectiondataservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/stretchr/testify/require"
)

// sentences returns n sentences, none of which holds anything personal.
func sentences(n int) []string {
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("Dossier %d suivi par le service client", i)
	}
	return values
}

// withFirst returns 50 sentences, the first ones replaced by the given texts.
func withFirst(texts ...string) []string {
	values := sentences(50)
	copy(values, texts)
	return values
}

// frenchAnalysis is a content analysis of texts in French, the language the rule of free
// text is measured in.
func frenchAnalysis(analyzer presidio.Analyzer) *contentAnalysis {
	content := newContentAnalysis(analyzer)
	content.language = "fr"
	return content
}

const (
	personA ="Rappeler Marie avant midi svp"
	personB = "Client Marie satisfait du service"
	placeA  = "Visite sur place a Lille hier"
	placeB  = "Livraison prevue a Lille demain"
)

// A column of free text is told personal data from the values that name a person, and from
// nothing else the analyzer finds in them.
func Test_contentAnalysis_FreeTextIsToldFromTheValuesThatNameAPerson(t *testing.T) {
	t.Run("two values that name a person make the verdict", func(t *testing.T) {
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", withFirst(personA, personB))

		require.True(t, ok)
		require.True(t, told.freeText)
		require.Equal(t, piidetect.PersonEntity, told.entity)
		require.Equal(t, 2, told.matchCount)
		require.InDelta(t, 0.8, told.avgScore, 0.001)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("a language the rule was not measured in keeps the rule of the third", func(t *testing.T) {
		for _, language := range []string{"en", "de", ""} {
			content := newContentAnalysis(entitiesIn(t, textMarkers))
			content.language = language

			_, ok := content.detect(t.Context(), "comment", withFirst(personA, personB))

			require.False(t, ok, language)
			require.Empty(t, content.notAnalyzed, language)
		}
	})

	t.Run("a locale of French is French", func(t *testing.T) {
		content := newContentAnalysis(entitiesIn(t, textMarkers))
		content.language = "fr-FR"

		_, ok := content.detect(t.Context(), "comment", withFirst(personA, personB))

		require.True(t, ok)
	})

	t.Run("one value is not enough", func(t *testing.T) {
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", withFirst(personA))

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("a value that holds a person twice counts once", func(t *testing.T) {
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", withFirst("Marie a rappele Marie hier soir"))

		require.False(t, ok)
	})

	t.Run("the places of the sentences do not make the verdict", func(t *testing.T) {
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", withFirst(placeA, placeB, placeA, placeB))

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("an email, a phone number or a link in the sentences do not make the verdict", func(t *testing.T) {
		content := frenchAnalysis(entitiesIn(t, map[string]found{
			"a@b.fr": {"EMAIL_ADDRESS", 0.7},
			"0612":   {"PHONE_NUMBER", 0.4},
			"http":   {"URL", 0.9},
		}))

		_, ok := content.detect(t.Context(), "comment", withFirst(
			"Ecrire a a@b.fr des que possible", "Ecrire a a@b.fr pour confirmer",
			"Appeler le 0612345678 apres midi", "Appeler le 0612345679 avant midi",
			"Voir http://exemple.fr pour le detail", "Voir http://autre.fr pour le suivi",
		))

		require.False(t, ok)
	})

	t.Run("the persons make the verdict whatever else the sentences hold", func(t *testing.T) {
		values := withFirst(personA, personB, placeA, placeB, "Voir Lille et Lille", "Aller a Lille", "Retour de Lille")
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.True(t, told.freeText)
		require.Equal(t, piidetect.PersonEntity, told.entity, "not the place, found in more values")
		require.Equal(t, 2, told.matchCount, "the values that name a person, and no other")
		require.InDelta(t, 0.8, told.avgScore, 0.001, "the score of the person, not of the place")
	})

	t.Run("a column that is not free text keeps the rule of the third", func(t *testing.T) {
		values := make([]string, 50)
		for i := range values {
			values[i] = fmt.Sprintf("Camille%d", i)
		}
		values[0], values[1] = "Marie", "Marie"
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "name", values)

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("a column of names found in a third of the values is told as before", func(t *testing.T) {
		values := make([]string, 6)
		for i := range values {
			values[i] = fmt.Sprintf("Camille%d", i)
		}
		values[0], values[1] = "Marie", "Marie"
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "name", values)

		require.True(t, ok)
		require.False(t, told.freeText)
		require.Equal(t, piidetect.PersonEntity, told.entity)
		require.Equal(t, 2, told.matchCount)
	})

	t.Run("the rule of the third is tried first on a column of free text", func(t *testing.T) {
		// Places in a third of the values: told as a place, as before the rule of free text,
		// though two values name a person.
		values := make([]string, 6)
		for i := range values {
			values[i] = fmt.Sprintf("Visite sur place a Lille hier %d", i)
		}
		values[0], values[1] = personA, personB
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.False(t, told.freeText)
		require.Equal(t, "LOCATION", told.entity)
	})

	t.Run("a column is free text by its values, not by what the analyzer is sent of them", func(t *testing.T) {
		// The first 200 runes of each value, which are what the analyzer reads, are one word:
		// the words are told on the whole value.
		long := strings.Repeat("a", maxValueRunes) + " b c d e"
		values := make([]string, 50)
		for i := range values {
			values[i] = long
		}
		values[0], values[1] = "Marie"+long, "Marie"+long
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.True(t, told.freeText)
		require.Equal(t, 2, told.matchCount)
	})

	t.Run("an analyzer that does not answer leaves the column not analyzed", func(t *testing.T) {
		fake := presidiotest.New(t)
		fake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			return nil, silence
		})
		content := frenchAnalysis(fake)

		_, ok := content.detect(t.Context(), "comment", sentences(50))

		require.False(t, ok)
		require.Equal(t, map[string]struct{}{"comment": {}}, content.notAnalyzed)
	})

	t.Run("a value the analyzer refuses leaves the column not analyzed when too few values count", func(t *testing.T) {
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", withFirst(personA, "Dossier REFUSE suivi par le service"))

		require.False(t, ok)
		require.Equal(t, map[string]struct{}{"comment": {}}, content.notAnalyzed)
	})

	t.Run("a value the analyzer refuses does not cost the verdict of the others", func(t *testing.T) {
		values := withFirst(personA, personB, "Dossier REFUSE suivi par le service")
		content := frenchAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, 2, told.matchCount)
		require.Empty(t, content.notAnalyzed)
	})
}

// A column the rule of the third tells of keeps what it was told as before the rule of free
// text: the multi-word values of addresses and of long names are not turned into free text.
func Test_contentAnalysis_detection_ThirdRuleStandsForMultiWordColumns(t *testing.T) {
	markers := map[string]found{"rue": {"LOCATION", 0.8}, "Jean": {"PERSON", 0.9}}

	t.Run("postal addresses are told as addresses", func(t *testing.T) {
		values := make([]string, 6)
		for i := range values {
			values[i] = fmt.Sprintf("%d rue Sainte-Catherine 33000 Bordeaux", i+1)
		}
		content := frenchAnalysis(entitiesIn(t, markers))

		got := content.detection(t.Context(), "public", "clients", "address", "text", values)

		require.NotNil(t, got)
		require.Equal(t, "LOCATION", got.GetEntityType())
		require.Equal(t, "street_address", got.GetDataCategory())
		require.Equal(t,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS, got.GetSuggestedTransformerSource())
		require.Equal(t, "LOCATION reconnu par analyse de contenu sur 6/6 valeurs (score moyen 0.80)", got.GetPiiEvidence())
	})

	t.Run("names of three words are told as full names", func(t *testing.T) {
		values := make([]string, 6)
		for i := range values {
			values[i] = fmt.Sprintf("Jean Pierre Dupont%d", i)
		}
		content := frenchAnalysis(entitiesIn(t, markers))

		got := content.detection(t.Context(), "public", "clients", "name", "text", values)

		require.NotNil(t, got)
		require.Equal(t, "PERSON", got.GetEntityType())
		require.Equal(t, "person_full_name", got.GetDataCategory())
		require.Equal(t,
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME, got.GetSuggestedTransformerSource())
		require.Equal(t, "PERSON reconnu par analyse de contenu sur 6/6 valeurs (score moyen 0.90)", got.GetPiiEvidence())
	})
}

func Test_contentAnalysis_detection_FreeText(t *testing.T) {
	content := frenchAnalysis(entitiesIn(t, textMarkers))

	got := content.detection(t.Context(), "public", "tickets", "comment", "text", withFirst(personA, personB))

	require.NotNil(t, got)
	require.Equal(t, "public", got.GetSchema())
	require.Equal(t, "tickets", got.GetTable())
	require.Equal(t, "comment", got.GetColumn())
	require.Equal(t, "PERSON", got.GetEntityType())
	require.InDelta(t, 0.8, got.GetScore(), 0.001)
	require.Equal(t, uint32(2), got.GetMatchCount())
	require.Equal(t, uint32(50), got.GetSampledCount())
	require.True(t, got.GetIsSensitive())
	require.Equal(t, piidetect.FreeTextCategory, got.GetDataCategory())
	require.Equal(t,
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT, got.GetSuggestedTransformerSource())
	require.Equal(t, mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW, got.GetPiiConfidence())
	require.Equal(t, mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT, got.GetPiiDetectionMethod())
	require.Equal(t, "texte libre : 2/50 valeurs désignent une personne", got.GetPiiEvidence())
}

// The free-text verdict is kept for a column of any type; the transformer that writes text is
// suggested only for a column that takes one.
func Test_contentAnalysis_detection_FreeTextTransformerFollowsTheColumnType(t *testing.T) {
	values := withFirst(personA, personB)
	piiText := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT
	none := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED
	cases := map[string]mgmtv1alpha1.TransformerSource{
		"text":              piiText,
		"character varying": piiText,
		"":                  piiText,
		"jsonb":             none,
		"JSON":              none,
		"bytea":             none,
		"text[]":            none,
		"integer":           none,
	}
	for dataType, want := range cases {
		t.Run("type "+dataType, func(t *testing.T) {
			content := frenchAnalysis(entitiesIn(t, textMarkers))

			got := content.detection(t.Context(), "public", "tickets", "comment", dataType, values)

			require.NotNil(t, got)
			require.Equal(t, piidetect.FreeTextCategory, got.GetDataCategory())
			require.Equal(t, uint32(2), got.GetMatchCount())
			require.Equal(t, mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW, got.GetPiiConfidence())
			require.Equal(t, want, got.GetSuggestedTransformerSource())
		})
	}
}

// The scan of a table names the columns it was asked about, and tells free text of them.
func Test_DetectPiiInConnectionData_FreeText(t *testing.T) {
	resp, err := scanOf(t, "status", withFirst(personA, personB)[:20], []string{"status"})

	require.NoError(t, err)
	require.Len(t, resp.GetDetections(), 1)
	detection := resp.GetDetections()[0]
	require.Equal(t, "status", detection.GetColumn())
	require.Equal(t, piidetect.FreeTextCategory, detection.GetDataCategory())
	require.Equal(t, "texte libre : 2/20 valeurs désignent une personne", detection.GetPiiEvidence())
	require.Len(t, resp.GetVerdicts(), 1, "only the named column is told of")
}

// A free-text column in which places are named, and no person, is not told: the scan of the
// table does not answer a rule that the sentences of a business table would all satisfy.
func Test_DetectPiiInConnectionData_FreeTextWithPlacesOnly(t *testing.T) {
	resp, err := scanOf(t, "status", withFirst(placeA, placeB, placeA)[:20], []string{"status"})

	require.NoError(t, err)
	require.Empty(t, resp.GetDetections())
	require.Len(t, resp.GetVerdicts(), 1)
	require.False(t, resp.GetVerdicts()[0].GetContentNotAnalyzed())
}
