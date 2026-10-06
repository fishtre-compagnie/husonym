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

// entitiesIn is an analyzer that finds, in a text, the entity of each marker it holds.
func entitiesIn(t *testing.T, markers map[string]string) *presidiotest.Fake {
	t.Helper()
	fake := presidiotest.New(t)
	fake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		findings := []presidio.Finding{}
		for marker, entity := range markers {
			if strings.Contains(req.Text, marker) {
				findings = append(findings, presidio.Finding{EntityType: entity, Score: 0.8})
			}
		}
		return findings, nil
	})
	return fake
}

func Test_contentAnalysis_FreeTextIsToldFromTwoValues(t *testing.T) {
	markers := map[string]string{
		"Marie":  "PERSON",
		"Lille":  "LOCATION",
		"a@b.fr": "EMAIL_ADDRESS",
		"http":   "URL",
	}
	with := func(texts ...string) []string {
		values := sentences(50)
		copy(values, texts)
		return values
	}

	t.Run("two values holding a person make the verdict", func(t *testing.T) {
		values := with("Rappeler Marie avant midi svp", "Client Marie satisfait du service")
		content := newContentAnalysis(entitiesIn(t, markers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.True(t, told.freeText)
		require.Equal(t, "PERSON", told.entity)
		require.Equal(t, 2, told.matchCount)
		require.InDelta(t, 0.8, told.avgScore, 0.001)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("the dominant entity is the sensitive one found in the most values", func(t *testing.T) {
		values := with(
			"Rappeler Marie avant midi svp", "Client Marie satisfait du service",
			"Client Marie venu en agence", "Visite sur place a Lille hier",
		)
		content := newContentAnalysis(entitiesIn(t, markers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, "PERSON", told.entity)
		require.Equal(t, 4, told.matchCount, "the values holding a sensitive entity, whichever it is")
		require.Equal(t, []string{"LOCATION", "PERSON"}, told.names)
	})

	t.Run("a person and an email in two values count as two", func(t *testing.T) {
		values := with("Rappeler Marie avant midi svp", "Ecrire a a@b.fr des que possible")
		content := newContentAnalysis(entitiesIn(t, markers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, 2, told.matchCount)
		require.Equal(t, []string{"EMAIL_ADDRESS", "PERSON"}, told.names)
	})

	t.Run("one value is not enough", func(t *testing.T) {
		content := newContentAnalysis(entitiesIn(t, markers))

		_, ok := content.detect(t.Context(), "comment", with("Rappeler Marie avant midi svp"))

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("an entity with no suggestion does not count", func(t *testing.T) {
		values := with("Voir http://exemple.fr pour le detail", "Voir http://autre.fr pour le suivi")
		content := newContentAnalysis(entitiesIn(t, markers))

		_, ok := content.detect(t.Context(), "comment", values)

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("a column of names keeps the rule of the third", func(t *testing.T) {
		values := make([]string, 50)
		for i := range values {
			values[i] = fmt.Sprintf("Camille%d", i)
		}
		values[0], values[1] = "Marie", "Marie"
		content := newContentAnalysis(entitiesIn(t, markers))

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
		content := newContentAnalysis(entitiesIn(t, markers))

		told, ok := content.detect(t.Context(), "name", values)

		require.True(t, ok)
		require.False(t, told.freeText)
		require.Equal(t, "PERSON", told.entity)
		require.Equal(t, 2, told.matchCount)
	})

	t.Run("a single filled value is not analyzed as missing", func(t *testing.T) {
		content := newContentAnalysis(entitiesIn(t, markers))

		_, ok := content.detect(t.Context(), "comment", []string{"Rappeler Marie avant midi svp"})

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("an analyzer that does not answer leaves the column not analyzed", func(t *testing.T) {
		fake := presidiotest.New(t)
		fake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			return nil, silence
		})
		content := newContentAnalysis(fake)

		_, ok := content.detect(t.Context(), "comment", sentences(50))

		require.False(t, ok)
		require.Equal(t, map[string]struct{}{"comment": {}}, content.notAnalyzed)
	})
}

func Test_freeTextDetection(t *testing.T) {
	told := columnVerdict{entity: "PERSON", avgScore: 0.75, matchCount: 3, names: []string{"LOCATION", "PERSON"}, freeText: true}

	got := freeTextDetection("public", "tickets", "comment", sentences(50), told)

	require.Equal(t, "public", got.GetSchema())
	require.Equal(t, "tickets", got.GetTable())
	require.Equal(t, "comment", got.GetColumn())
	require.Equal(t, "PERSON", got.GetEntityType())
	require.InDelta(t, 0.75, got.GetScore(), 0.001)
	require.Equal(t, uint32(3), got.GetMatchCount())
	require.Equal(t, uint32(50), got.GetSampledCount())
	require.True(t, got.GetIsSensitive())
	require.Equal(t, piidetect.FreeTextCategory, got.GetDataCategory())
	require.Equal(t, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_PII_TEXT, got.GetSuggestedTransformerSource())
	require.Equal(t, mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW, got.GetPiiConfidence())
	require.Equal(t, mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT, got.GetPiiDetectionMethod())
	require.Equal(t, "texte libre : 3/50 valeurs contiennent LOCATION, PERSON", got.GetPiiEvidence())
}
