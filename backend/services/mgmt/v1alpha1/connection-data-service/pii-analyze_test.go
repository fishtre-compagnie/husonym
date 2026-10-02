package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"fmt"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

// scriptedAnalyzer finds a person in each text, but for the texts it is told to fail on: it
// fails them with their error as many times as told, then finds a person in them too.
type scriptedAnalyzer struct {
	failures map[string]int
	failure  error
	asked    []string
}

func (a *scriptedAnalyzer) Analyze(_ context.Context, req presidio.AnalyzeRequest) ([]presidio.AnalyzeResult, error) {
	a.asked = append(a.asked, req.Text)
	if a.failures[req.Text] > 0 {
		a.failures[req.Text]--
		return nil, a.failure
	}
	return []presidio.AnalyzeResult{{EntityType: "PERSON", Score: 0.9}}, nil
}

type quiet struct{}

func (quiet) Warn(string, ...any) {}

var (
	names   = []string{"Ada Lovelace", "Alan Turing", "Grace Hopper", "Edsger Dijkstra"}
	cities  = []string{"London", "Paris", "Arlington", "Rotterdam"}
	refused = errors.New("presidio analyze returned status 500: recognizer failed")
	silence = fmt.Errorf("presidio analyze request failed: %w", presidio.ErrNoAnswer)
)

func newContentAnalysis(analyzer presidio.Analyzer) *contentAnalysis {
	return &contentAnalysis{
		analyze: analyzer, threshold: 0.5, language: "en", logger: quiet{}, notAnalyzed: map[string]struct{}{},
	}
}

func Test_contentAnalysis_TellsWhatTheAnalyzerFinds(t *testing.T) {
	content := newContentAnalysis(&scriptedAnalyzer{})

	entity, score, matches, ok := content.column(context.Background(), "full_name", names)

	require.True(t, ok)
	require.Equal(t, "PERSON", entity)
	require.InDelta(t, 0.9, score, 0.001)
	require.Equal(t, len(names), matches)
	require.Empty(t, content.notAnalyzed)
}

// A value the analyzer refuses once is asked again: a refusal that passes does not cost the
// column its analysis.
func Test_contentAnalysis_AsksAgainAValueThatIsRefused(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Alan Turing": 1}, failure: refused}
	content := newContentAnalysis(analyzer)

	_, _, matches, ok := content.column(context.Background(), "full_name", names)

	require.True(t, ok)
	require.Equal(t, len(names), matches)
	require.Empty(t, content.notAnalyzed)
	require.Len(t, analyzer.asked, len(names)+1)
}

// A column the analyzer keeps refusing a value of is told not analyzed, and not empty of
// personal data: a value left out would count as one without any. The other columns are
// analyzed all the same.
func Test_contentAnalysis_AColumnItCannotAnalyseDoesNotCostTheOthers(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Alan Turing": 2}, failure: refused}
	content := newContentAnalysis(analyzer)

	_, _, _, ok := content.column(context.Background(), "full_name", names)
	require.False(t, ok)
	require.Equal(t, map[string]struct{}{"full_name": {}}, content.notAnalyzed)
	require.Equal(t, []string{"Ada Lovelace", "Alan Turing", "Alan Turing"}, analyzer.asked,
		"the column was analyzed past the value it could not be")

	_, _, matches, ok := content.column(context.Background(), "city", cities)
	require.True(t, ok)
	require.Equal(t, len(cities), matches)
	require.Equal(t, map[string]struct{}{"full_name": {}}, content.notAnalyzed)
}

// An analyzer that does not answer is not asked again, for this value nor for the columns
// that follow, each of which it would be waited for: they are told not analyzed.
func Test_contentAnalysis_AnAnalyzerThatDoesNotAnswerIsNotAskedAgain(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Ada Lovelace": 10}, failure: silence}
	content := newContentAnalysis(analyzer)

	_, _, _, ok := content.column(context.Background(), "full_name", names)
	require.False(t, ok)
	_, _, _, ok = content.column(context.Background(), "city", cities)
	require.False(t, ok)

	require.Equal(t, map[string]struct{}{"full_name": {}, "city": {}}, content.notAnalyzed)
	require.Equal(t, []string{"Ada Lovelace"}, analyzer.asked)
}

// The verdict of a column tells that its content could not be analyzed, whatever its name
// says of it, and a column the schema does not list is told all the same.
func Test_verdicts_TellTheColumnsNotAnalysed(t *testing.T) {
	columns := []*mgmtv1alpha1.DatabaseColumn{
		{Schema: "public", Table: "users", Column: "email", DataType: "text"},
		{Schema: "public", Table: "users", Column: "notes", DataType: "text"},
		{Schema: "public", Table: "users", Column: "nickname", DataType: "text"},
	}
	notAnalyzed := map[string]struct{}{"email": {}, "notes": {}, "comment": {}}

	byColumn := map[string]*mgmtv1alpha1.ColumnPiiVerdict{}
	for _, verdict := range verdicts("public", "users", columns, nil, nil, notAnalyzed) {
		byColumn[verdict.GetColumn()] = verdict
	}

	require.Len(t, byColumn, 4)
	require.True(t, byColumn["email"].GetContentNotAnalyzed())
	require.True(t, byColumn["email"].GetIsSensitive(), "the name of the column still speaks")
	require.True(t, byColumn["notes"].GetContentNotAnalyzed())
	require.False(t, byColumn["notes"].GetIsSensitive())
	require.False(t, byColumn["nickname"].GetContentNotAnalyzed())
	require.True(t, byColumn["comment"].GetContentNotAnalyzed(), "a column the schema does not list is not told")
}
