package v1alpha1_connectiondataservice

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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

// numbered returns n values made of the prefix and the rank of the value.
func numbered(prefix string, n int) []string {
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("%s%d", prefix, i)
	}
	return values
}

func Test_contentAnalysis_detection_Addresses(t *testing.T) {
	values := make([]string, 6)
	for i := range values {
		values[i] = fmt.Sprintf("%d rue Sainte-Catherine 33000 Bordeaux", i+1)
	}
	content := newContentAnalysis(entitiesIn(t, map[string]found{"rue": {"LOCATION", 0.8}}))

	got := content.detection(t.Context(), "public", "clients", "address", "text", values)

	require.NotNil(t, got)
	require.Equal(t, "LOCATION", got.GetEntityType())
	require.Equal(t, "street_address", got.GetDataCategory())
	require.Equal(t,
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS, got.GetSuggestedTransformerSource())
	require.Equal(t, "LOCATION reconnu par analyse de contenu sur 6/6 valeurs (score moyen 0.80)", got.GetPiiEvidence())
}

func Test_contentAnalysis_detection_NamesOfThreeWords(t *testing.T) {
	values := make([]string, 6)
	for i := range values {
		values[i] = fmt.Sprintf("Jean Pierre Dupont%d", i)
	}
	content := newContentAnalysis(entitiesIn(t, map[string]found{"Jean": {"PERSON", 0.9}}))

	got := content.detection(t.Context(), "public", "clients", "name", "text", values)

	require.NotNil(t, got)
	require.Equal(t, "PERSON", got.GetEntityType())
	require.Equal(t, "person_full_name", got.GetDataCategory())
	require.Equal(t,
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME, got.GetSuggestedTransformerSource())
	require.Equal(t, "PERSON reconnu par analyse de contenu sur 6/6 valeurs (score moyen 0.90)", got.GetPiiEvidence())
}

// The data type of the column reaches the suggestion: phone numbers in an integer column are
// given the generator of integer phone numbers.
func Test_contentAnalysis_detection_PhoneNumbersInAnIntegerColumn(t *testing.T) {
	values := make([]string, 6)
	for i := range values {
		values[i] = fmt.Sprintf("06123456%02d", i)
	}
	content := newContentAnalysis(entitiesIn(t, map[string]found{"0612": {"PHONE_NUMBER", 0.9}}))

	got := content.detection(t.Context(), "public", "clients", "contact", "bigint", values)

	require.NotNil(t, got)
	require.Equal(t, "PHONE_NUMBER", got.GetEntityType())
	require.Equal(t,
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER, got.GetSuggestedTransformerSource())
}

func Test_contentAnalysis_detection_Fields(t *testing.T) {
	values := append([]string{"Marie Dupont", "Marie Martin"}, numbered("Camille Durand", 4)...)
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	got := content.detection(t.Context(), "public", "clients", "name", "text", values)

	require.NotNil(t, got)
	require.Equal(t, "public", got.GetSchema())
	require.Equal(t, "clients", got.GetTable())
	require.Equal(t, "name", got.GetColumn())
	require.Equal(t, "PERSON", got.GetEntityType())
	require.InDelta(t, 0.8, got.GetScore(), 0.001)
	require.Equal(t, uint32(2), got.GetMatchCount())
	require.Equal(t, uint32(6), got.GetSampledCount())
	require.True(t, got.GetIsSensitive())
	require.Equal(t, mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW, got.GetPiiConfidence())
	require.Equal(t, mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CONTENT, got.GetPiiDetectionMethod())
}

func Test_contentAnalysis_detection_NothingFound(t *testing.T) {
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	require.Nil(t, content.detection(t.Context(), "public", "tickets", "comment", "text", numbered("Dossier ", 50)))
	require.Empty(t, content.notAnalyzed)
}

func Test_contentAnalysis_detection_AnEntityWithNoSuggestionIsNotTold(t *testing.T) {
	values := numbered("Voir http://exemple.fr pour le dossier ", 6)
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	require.Nil(t, content.detection(t.Context(), "public", "tickets", "comment", "text", values))
	require.Empty(t, content.notAnalyzed)
}

// Entities found in as many values are told apart by name, and the score is the one of the
// entity told.
func Test_contentAnalysis_detection_TheDominantEntityOfATieIsTheFirstByName(t *testing.T) {
	values := append(numbered("Marie ", 3), numbered("Visite a Lille ", 3)...)
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	got := content.detection(t.Context(), "public", "clients", "place", "text", values)

	require.NotNil(t, got)
	require.Equal(t, "LOCATION", got.GetEntityType())
	require.InDelta(t, 0.6, got.GetScore(), 0.001, "the score of LOCATION, not of PERSON")
	require.Equal(t, uint32(3), got.GetMatchCount())
}

func Test_contentAnalysis_detection_TheMostFrequentEntityWins(t *testing.T) {
	values := append(numbered("Marie ", 4), numbered("Visite a Lille ", 2)...)
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	got := content.detection(t.Context(), "public", "clients", "place", "text", values)

	require.NotNil(t, got)
	require.Equal(t, "PERSON", got.GetEntityType())
	require.Equal(t, uint32(4), got.GetMatchCount())
}

func Test_contentAnalysis_detection_AnEntityOfTooFewValuesIsNotTold(t *testing.T) {
	values := append([]string{"Marie Dupont", "Marie Martin"}, numbered("Camille Durand", 48)...)
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	require.Nil(t, content.detection(t.Context(), "public", "clients", "name", "text", values))
	require.Empty(t, content.notAnalyzed)
}

func Test_contentAnalysis_detection_ASingleFilledValueTellsNothingAndIsAnalyzed(t *testing.T) {
	content := newContentAnalysis(entitiesIn(t, textMarkers))

	got := content.detection(t.Context(), "public", "tickets", "comment", "text", []string{"Rappeler Marie avant midi svp"})

	require.Nil(t, got)
	require.Empty(t, content.notAnalyzed)
}

func Test_contentAnalysis_detection_AnAnalyzerThatDoesNotAnswerLeavesTheColumnNotAnalyzed(t *testing.T) {
	fake := presidiotest.New(t)
	fake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		return nil, silence
	})
	content := newContentAnalysis(fake)

	got := content.detection(t.Context(), "public", "tickets", "comment", "text", numbered("Dossier ", 50))

	require.Nil(t, got)
	require.Equal(t, map[string]struct{}{"comment": {}}, content.notAnalyzed)
}

func Test_contentAnalysis_detection_ValuesTheAnalyzerRefuses(t *testing.T) {
	t.Run("a refused value leaves the column not analyzed when too few values count", func(t *testing.T) {
		values := append([]string{"Marie Dupont", "Dossier REFUSE"}, numbered("Camille Durand", 48)...)
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		got := content.detection(t.Context(), "public", "clients", "name", "text", values)

		require.Nil(t, got)
		require.Equal(t, map[string]struct{}{"name": {}}, content.notAnalyzed)
	})

	t.Run("a refused value does not cost the verdict of the others", func(t *testing.T) {
		values := append(numbered("Marie Dupont", 5), "Dossier REFUSE")
		content := newContentAnalysis(entitiesIn(t, textMarkers))

		got := content.detection(t.Context(), "public", "clients", "name", "text", values)

		require.NotNil(t, got)
		require.Equal(t, "PERSON", got.GetEntityType())
		require.Equal(t, uint32(5), got.GetMatchCount())
		require.Empty(t, content.notAnalyzed)
	})
}

// scanOf runs the scan of the columns of a table, the sampled column holding the given values,
// under a license that names no feature list: every feature is included.
func scanOf(
	t *testing.T,
	column string,
	values []string,
	columns []string,
) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
	t.Helper()
	return scanUnder(t, testutil.NewFakeEELicense(testutil.WithIsValid()), column, values, columns)
}

// scanUnder is scanOf under the given license.
func scanUnder(
	t *testing.T,
	eelicense license.EEInterface,
	column string,
	values []string,
	columns []string,
) (*mgmtv1alpha1.DetectPiiInConnectionDataResponse, error) {
	t.Helper()
	dataconn := connectiondata.NewMockConnectionDataService(t)
	dataconn.EXPECT().
		SampleColumn(mock.Anything, mock.Anything, "public", "clients", column, uint(20)).
		RunAndReturn(func(_ context.Context, stream connectiondata.SampleDataStream, _, _, _ string, _ uint) error {
			for _, value := range values {
				sendRows(t, stream, map[string]any{column: value})
			}
			return nil
		})
	dataconn.EXPECT().
		GetTableSchema(mock.Anything, "public", "clients").
		Return([]*mgmtv1alpha1.DatabaseColumn{
			{Schema: "public", Table: "clients", Column: "name", DataType: "text"},
			{Schema: "public", Table: "clients", Column: "status", DataType: "text"},
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
		transformers:          Transformers{License: eelicense},
	}

	resp, err := service.DetectPiiInConnectionData(t.Context(), connect.NewRequest(
		&mgmtv1alpha1.DetectPiiInConnectionDataRequest{
			ConnectionId: "c1", Schema: "public", Table: "clients", Columns: columns,
		},
	))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// The scan of a table names the columns it was asked about, and tells of them only.
func Test_DetectPiiInConnectionData_NamedColumn(t *testing.T) {
	values := numbered("Marie Dupont", 20)

	resp, err := scanOf(t, "name", values, []string{"name"})

	require.NoError(t, err)
	require.Len(t, resp.GetDetections(), 1)
	detection := resp.GetDetections()[0]
	require.Equal(t, "name", detection.GetColumn())
	require.Equal(t, "PERSON", detection.GetEntityType())
	require.Equal(t, "PERSON reconnu par analyse de contenu sur 20/20 valeurs (score moyen 0.80)", detection.GetPiiEvidence())
	require.Len(t, resp.GetVerdicts(), 1, "only the named column is told of")
	require.Equal(t, "name", resp.GetVerdicts()[0].GetColumn())
}

// A column that holds a single filled value naming a person is not told as personal data and is
// not left unanalyzed.
func Test_DetectPiiInConnectionData_ASingleFilledValue(t *testing.T) {
	resp, err := scanOf(t, "name", []string{"Marie Dupont"}, []string{"name"})

	require.NoError(t, err)
	require.Empty(t, resp.GetDetections())
	require.Len(t, resp.GetVerdicts(), 1)
	require.False(t, resp.GetVerdicts()[0].GetContentNotAnalyzed())
}

// The scan of the content is a feature of the license: a license without it refuses the call
// once the connection is known, and before the connection is opened or a value is sampled.
func Test_DetectPiiInConnectionData_NeedsThePiiDetectionFeature(t *testing.T) {
	for name, eelicense := range map[string]*testutil.FakeEELicense{
		"a license that includes every other feature": testutil.NewFakeEELicense(
			testutil.WithIsValid(), testutil.WithFeatures(license.FeaturePiiText, license.FeatureCustomTransformers),
		),
		"a license that is not in force": testutil.NewFakeEELicense(),
	} {
		t.Run("refused under "+name, func(t *testing.T) {
			// The builder expects no call: opening the connection would fail the test. So would an
			// analyzer call, since none is answered.
			connections := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
			connections.EXPECT().GetConnection(mock.Anything, mock.Anything).Return(
				connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{Connection: &mgmtv1alpha1.Connection{Id: "c1"}}), nil,
			)
			fake := presidiotest.New(t)
			service := &Service{
				cfg:                   &Config{IsPresidioEnabled: true},
				connectionService:     connections,
				connectiondatabuilder: connectiondata.NewMockConnectionDataBuilder(t),
				analyze:               fake,
				transformers:          Transformers{License: eelicense},
			}

			resp, err := service.DetectPiiInConnectionData(t.Context(), connect.NewRequest(
				&mgmtv1alpha1.DetectPiiInConnectionDataRequest{ConnectionId: "c1", Schema: "public", Table: "clients"},
			))

			require.Nil(t, resp)
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
			require.ErrorContains(t, err, "this license does not include pii_detection")
			require.Equal(t, presidiotest.Calls{}, fake.Calls())
		})
	}

	t.Run("served under a license that includes it and no other feature", func(t *testing.T) {
		eelicense := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeaturePiiDetection))

		resp, err := scanUnder(t, eelicense, "name", numbered("Marie Dupont", 20), []string{"name"})

		require.NoError(t, err)
		require.Len(t, resp.GetDetections(), 1)
	})
}
