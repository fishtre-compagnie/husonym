package v1alpha1_connectiondataservice

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

// failingAnalyzer finds a person in each value, and fails from its call failsAt on.
type failingAnalyzer struct {
	calls   int
	failsAt int
}

func (a *failingAnalyzer) Analyze(context.Context, presidio.AnalyzeRequest) ([]presidio.AnalyzeResult, error) {
	a.calls++
	if a.failsAt > 0 && a.calls >= a.failsAt {
		return nil, errors.New(`presidio analyze request failed: Post "http://presidio:3000/analyze": context deadline exceeded`)
	}
	return []presidio.AnalyzeResult{{EntityType: "PERSON", Score: 0.9}}, nil
}

var names = []string{"Ada Lovelace", "Alan Turing", "Grace Hopper", "Edsger Dijkstra"}

// A column the analyzer cannot read is not told empty of personal data: the scan fails, at the
// first value the analyzer fails on.
func Test_analyzeColumn_FailsWhenTheAnalyzerDoes(t *testing.T) {
	analyzer := &failingAnalyzer{failsAt: 2}
	service := &Service{analyze: analyzer}

	_, _, _, ok, err := service.analyzeColumn(context.Background(), names, 0.5, "en")

	require.Error(t, err)
	require.False(t, ok)
	require.Equal(t, 2, analyzer.calls, "the analyzer was asked again after it failed")
}

func Test_analyzeColumn_TellsWhatTheAnalyzerFinds(t *testing.T) {
	analyzer := &failingAnalyzer{}
	service := &Service{analyze: analyzer}

	entity, score, matches, ok, err := service.analyzeColumn(context.Background(), names, 0.5, "en")

	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "PERSON", entity)
	require.InDelta(t, 0.9, score, 0.001)
	require.Equal(t, len(names), matches)
}

// The failure is told as the analysis being unavailable, in words that name the table.
func Test_analysisUnavailable(t *testing.T) {
	err := analysisUnavailable("public", "users")

	require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
	require.ErrorContains(t, err, "public.users")
}
