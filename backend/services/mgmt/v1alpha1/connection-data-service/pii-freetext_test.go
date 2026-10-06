package v1alpha1_connectiondataservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/piidetect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/stretchr/testify/mock"
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

// found is what an analyzer finds in a text that holds a marker: an entity, with a score.
type found struct {
	entity string
	score  float64
}

// entitiesIn is an analyzer that finds, in a text, the entity of each marker it holds. A text
// that holds REFUSE is refused.
func entitiesIn(t *testing.T, markers map[string]found) *presidiotest.Fake {
	t.Helper()
	fake := presidiotest.New(t)
	fake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		if strings.Contains(req.Text, "REFUSE") {
			return nil, refused
		}
		findings := []presidio.Finding{}
		for marker, f := range markers {
			if strings.Contains(req.Text, marker) {
				findings = append(findings, presidio.Finding{EntityType: f.entity, Score: f.score})
			}
		}
		return findings, nil
	})
	return fake
}

var textMarkers = map[string]found{
	"Marie":  {"PERSON", 0.8},
	"Lille":  {"LOCATION", 0.6},
	"a@b.fr": {"EMAIL_ADDRESS", 0.7},
	"http":   {"URL", 0.9},
}

// withFirst returns 50 sentences, the first ones replaced by the given texts.
func withFirst(texts ...string) []string {
	values := sentences(50)
	copy(values, texts)
	return values
}

func Test_contentAnalysis_FreeTextIsToldFromTwoValues(t *testing.T) {
	t.Run("two values holding a person make the verdict", func(t *testing.T) {
		values := withFirst("Rappeler Marie avant midi svp", "Client Marie satisfait du service")
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.True(t, told.freeText)
		require.Equal(t, "PERSON", told.entity)
		require.Equal(t, 2, told.matchCount)
		require.InDelta(t, 0.8, told.avgScore, 0.001)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("the dominant entity is the sensitive one found in the most values", func(t *testing.T) {
		values := withFirst(
			"Rappeler Marie avant midi svp", "Client Marie satisfait du service",
			"Client Marie venu en agence", "Visite sur place a Lille hier",
		)
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, "PERSON", told.entity)
		require.InDelta(t, 0.8, told.avgScore, 0.001, "the mean score of the dominant entity")
		require.Equal(t, 4, told.matchCount, "the values holding a sensitive entity, whichever it is")
		require.Equal(t, []string{"LOCATION", "PERSON"}, told.names)
	})

	t.Run("entities found in as many values are told apart by name", func(t *testing.T) {
		values := withFirst("Rappeler Marie avant midi svp", "Visite sur place a Lille hier")
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, "LOCATION", told.entity)
		require.InDelta(t, 0.6, told.avgScore, 0.001, "the score of LOCATION, not of PERSON")
		require.Equal(t, 2, told.matchCount)
	})

	t.Run("a person and an email in two values count as two", func(t *testing.T) {
		values := withFirst("Rappeler Marie avant midi svp", "Ecrire a a@b.fr des que possible")
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, 2, told.matchCount)
		require.Equal(t, []string{"EMAIL_ADDRESS", "PERSON"}, told.names)
	})

	t.Run("a value holding two entities counts once", func(t *testing.T) {
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", withFirst("Marie est passee a Lille hier"))

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("one value is not enough", func(t *testing.T) {
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", withFirst("Rappeler Marie avant midi svp"))

		require.False(t, ok)
		require.Empty(t, content.notAnalyzed)
	})

	t.Run("an entity with no suggestion does not count", func(t *testing.T) {
		values := withFirst("Voir http://exemple.fr pour le detail", "Voir http://autre.fr pour le suivi")
		content := newContentAnalysis(entitiesIn(t, textMarkers))

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
		content := newContentAnalysis(entitiesIn(t, textMarkers))

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
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "name", values)

		require.True(t, ok)
		require.False(t, told.freeText)
		require.Equal(t, "PERSON", told.entity)
		require.Equal(t, 2, told.matchCount)
	})

	t.Run("a free-text column with a single filled value tells nothing and is analyzed", func(t *testing.T) {
		content := newContentAnalysis(entitiesIn(t, textMarkers))

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

	t.Run("a value the analyzer refuses leaves the column not analyzed when too few values count", func(t *testing.T) {
		values := withFirst("Rappeler Marie avant midi svp", "Dossier REFUSE suivi par le service")
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		_, ok := content.detect(t.Context(), "comment", values)

		require.False(t, ok)
		require.Equal(t, map[string]struct{}{"comment": {}}, content.notAnalyzed)
	})

	t.Run("a value the analyzer refuses does not cost the verdict of the others", func(t *testing.T) {
		values := withFirst(
			"Rappeler Marie avant midi svp", "Client Marie satisfait du service", "Dossier REFUSE suivi par le service",
		)
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.Equal(t, 2, told.matchCount)
		require.Empty(t, content.notAnalyzed)
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
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		told, ok := content.detect(t.Context(), "comment", values)

		require.True(t, ok)
		require.True(t, told.freeText)
		require.Equal(t, 2, told.matchCount)
	})
}

// A column the rule of the third tells of keeps what it was told as before the rule of free
// text: the free-text rule is tried on a column the rule of the third tells nothing of.
func Test_contentAnalysis_detection_ThirdRuleStandsForMultiWordColumns(t *testing.T) {
	markers := map[string]found{"rue": {"LOCATION", 0.8}, "Jean": {"PERSON", 0.9}}

	t.Run("postal addresses are told as addresses", func(t *testing.T) {
		values := make([]string, 6)
		for i := range values {
			values[i] = fmt.Sprintf("%d rue Sainte-Catherine 33000 Bordeaux", i+1)
		}
		content := newContentAnalysis(entitiesIn(t, markers))

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
		content := newContentAnalysis(entitiesIn(t, markers))

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
	values := withFirst("Rappeler Marie avant midi svp", "Client Marie satisfait du service")
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	got := content.detection(t.Context(), "public", "tickets", "comment", "text", values)

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
	require.Equal(t, "texte libre : 2/50 valeurs contiennent PERSON", got.GetPiiEvidence())
}

func Test_contentAnalysis_detection_NothingFound(t *testing.T) {
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	require.Nil(t, content.detection(t.Context(), "public", "tickets", "comment", "text", sentences(50)))
}

func Test_freeTextDetection(t *testing.T) {
	told := columnVerdict{
		entity: "PERSON", avgScore: 0.75, matchCount: 3, names: []string{"LOCATION", "PERSON"}, freeText: true,
	}

	got := freeTextDetection("public", "tickets", "comment", sentences(50), told)

	require.Equal(t, "texte libre : 3/50 valeurs contiennent LOCATION, PERSON", got.GetPiiEvidence())
	require.Equal(t, uint32(3), got.GetMatchCount())
}

// The scan of a table names the columns it was asked about, and tells free text of them.
func Test_DetectPiiInConnectionData_FreeText(t *testing.T) {
	dataconn := connectiondata.NewMockConnectionDataService(t)
	dataconn.EXPECT().
		SampleColumn(mock.Anything, mock.Anything, "public", "tickets", "comment", uint(20)).
		RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _, _ string, _ uint) error {
			for _, value := range withFirst("Rappeler Marie avant midi svp", "Client Marie satisfait du service")[:20] {
				sendRows(t, stream, map[string]any{"comment": value})
			}
			return nil
		})
	dataconn.EXPECT().
		GetTableSchema(mock.Anything, "public", "tickets").
		Return([]*mgmtv1alpha1.DatabaseColumn{
			{Schema: "public", Table: "tickets", Column: "comment", DataType: "text"},
			{Schema: "public", Table: "tickets", Column: "status", DataType: "text"},
		}, nil)
	builder := connectiondata.NewMockConnectionDataBuilder(t)
	builder.EXPECT().NewDataConnection(mock.Anything, mock.Anything).Return(dataconn, nil)
	connections := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
	connections.EXPECT().GetConnection(mock.Anything, mock.Anything).Return(
		connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{Connection: &mgmtv1alpha1.Connection{Id: "c1"}}), nil,
	)
	service := &Service{
		cfg:                   &Config{IsPresidioEnabled: true},
		connectionService:     connections,
		connectiondatabuilder: builder,
		analyze:               entitiesIn(t, textMarkers),
	}

	resp, err := service.DetectPiiInConnectionData(t.Context(), connect.NewRequest(
		&mgmtv1alpha1.DetectPiiInConnectionDataRequest{
			ConnectionId: "c1", Schema: "public", Table: "tickets", Columns: []string{"comment"},
		},
	))

	require.NoError(t, err)
	require.Len(t, resp.Msg.GetDetections(), 1)
	detection := resp.Msg.GetDetections()[0]
	require.Equal(t, "comment", detection.GetColumn())
	require.Equal(t, piidetect.FreeTextCategory, detection.GetDataCategory())
	require.Equal(t, "texte libre : 2/20 valeurs contiennent PERSON", detection.GetPiiEvidence())
	require.Len(t, resp.Msg.GetVerdicts(), 1, "only the named column is told of")
	require.Equal(t, "comment", resp.Msg.GetVerdicts()[0].GetColumn())
}
