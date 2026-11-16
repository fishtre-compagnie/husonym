package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

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

func (a *scriptedAnalyzer) Analyze(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
	a.asked = append(a.asked, req.Text)
	if a.failures[req.Text] > 0 {
		a.failures[req.Text]--
		return nil, a.failure
	}
	return []presidio.Finding{{EntityType: "PERSON", Score: 0.9}}, nil
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

	told, ok := content.detect(context.Background(), "full_name", names)

	require.True(t, ok)
	require.Equal(t, "PERSON", told.entity)
	require.InDelta(t, 0.9, told.avgScore, 0.001)
	require.Equal(t, len(names), told.matchCount)
	require.Empty(t, content.notAnalyzed)
}

// A value the analyzer refuses once is asked again: a refusal that passes does not cost the
// column its analysis.
func Test_contentAnalysis_AsksAgainAValueThatIsRefused(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Alan Turing": 1}, failure: refused}
	content := newContentAnalysis(analyzer)

	told, ok := content.detect(context.Background(), "full_name", names)

	require.True(t, ok)
	require.Equal(t, len(names), told.matchCount)
	require.Empty(t, content.notAnalyzed)
	require.Len(t, analyzer.asked, len(names)+1)
}

// A column one value of which the analyzer keeps refusing is told by the others, when they
// are enough to find an entity: what they show stands.
func Test_contentAnalysis_AValueItRefusesDoesNotCostTheColumn(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Alan Turing": 2}, failure: refused}
	content := newContentAnalysis(analyzer)

	told, ok := content.detect(context.Background(), "full_name", names)

	require.True(t, ok)
	require.Equal(t, "PERSON", told.entity)
	require.Equal(t, len(names)-1, told.matchCount)
	require.Empty(t, content.notAnalyzed)
}

// A column the values taken of which show no entity, some of them refused, is told not
// analyzed, and not empty of personal data: the values refused may be the ones that hold some.
// It is given up at its second value refused, and the other columns are analyzed all the same.
func Test_contentAnalysis_AColumnItCannotAnalyzeDoesNotCostTheOthers(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Ada Lovelace": 2, "Alan Turing": 2}, failure: refused}
	content := newContentAnalysis(analyzer)

	_, ok := content.detect(context.Background(), "full_name", names)
	require.False(t, ok)
	require.Equal(t, map[string]struct{}{"full_name": {}}, content.notAnalyzed)
	require.Equal(t, []string{"Ada Lovelace", "Ada Lovelace", "Alan Turing", "Alan Turing"}, analyzer.asked,
		"the column was analyzed past the values it was given up on")

	told, ok := content.detect(context.Background(), "city", cities)
	require.True(t, ok)
	require.Equal(t, len(cities), told.matchCount)
	require.Equal(t, map[string]struct{}{"full_name": {}}, content.notAnalyzed)
}

// A column in which the analyzer finds nothing, none of its values refused, is analyzed.
func Test_contentAnalysis_AColumnWithoutEntityIsAnalyzed(t *testing.T) {
	content := newContentAnalysis(nothingFound{})

	_, ok := content.detect(context.Background(), "label", names)

	require.False(t, ok)
	require.Empty(t, content.notAnalyzed)
}

// An entity that too few of the values carry is not told of the column, which is analyzed
// all the same: a name in a column of labels does not make it a column of names.
func Test_contentAnalysis_AnEntityOfTooFewValuesIsNotTold(t *testing.T) {
	content := newContentAnalysis(findsIn{"Ada Lovelace"})

	_, ok := content.detect(context.Background(), "label", names)

	require.False(t, ok)
	require.Empty(t, content.notAnalyzed)
}

// findsIn finds a person in the texts it holds, and nothing in the others.
type findsIn []string

func (f findsIn) Analyze(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
	for _, text := range f {
		if text == req.Text {
			return []presidio.Finding{{EntityType: "PERSON", Score: 0.9}}, nil
		}
	}
	return nil, nil
}

type nothingFound struct{}

func (nothingFound) Analyze(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
	return nil, nil
}

// limited tells how long each call it receives may last, and what it was asked.
type limited struct {
	limits   []time.Duration
	requests []presidio.AnalyzeRequest
}

func (l *limited) Analyze(ctx context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("the call has no limit")
	}
	l.limits = append(l.limits, time.Until(deadline))
	l.requests = append(l.requests, *req)
	return nil, nil
}

// A sampled value is short: the analyzer is waited for less long than for a text of any length.
func Test_analyzeColumn_WaitsLessThanTheClientDoes(t *testing.T) {
	analyzer := &limited{}

	_, refused, err := analyzeColumn(context.Background(), analyzer, names, 0.5, "fr")

	require.NoError(t, err)
	require.NoError(t, refused)
	require.Len(t, analyzer.limits, len(names))
	for _, limit := range analyzer.limits {
		require.LessOrEqual(t, limit, analyzeTimeout)
		require.Greater(t, limit, analyzeTimeout-5*time.Second)
	}
	require.Less(t, analyzeTimeout, presidio.Timeout)

	threshold := 0.5
	require.Equal(t, presidio.AnalyzeRequest{Text: names[0], Language: "fr", ScoreThreshold: &threshold}, analyzer.requests[0])
}

// A threshold that is not set is left to the analyzer.
func Test_analyzeColumn_SendsNoThresholdWhenNoneIsSet(t *testing.T) {
	analyzer := &limited{}

	_, _, err := analyzeColumn(context.Background(), analyzer, names[:1], 0, "en")

	require.NoError(t, err)
	require.Nil(t, analyzer.requests[0].ScoreThreshold)
}

func Test_minMatches(t *testing.T) {
	require.Equal(t, 2, minMatches(4))
	require.Equal(t, 2, minMatches(6))
	require.Equal(t, 6, minMatches(20))
}

// An analyzer that does not answer is not asked again, for this value nor for the columns
// that follow, each of which it would be waited for: they are told not analyzed.
func Test_contentAnalysis_AnAnalyzerThatDoesNotAnswerIsNotAskedAgain(t *testing.T) {
	analyzer := &scriptedAnalyzer{failures: map[string]int{"Ada Lovelace": 10}, failure: silence}
	content := newContentAnalysis(analyzer)

	_, ok := content.detect(context.Background(), "full_name", names)
	require.False(t, ok)
	_, ok = content.detect(context.Background(), "city", cities)
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
