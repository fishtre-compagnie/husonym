package presidiotest

import (
	"context"
	"testing"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

// failed records whether a test was failed, in place of the test that runs.
type failed struct {
	testing.TB
	messages int
}

func (f *failed) Helper()               {}
func (f *failed) Errorf(string, ...any) { f.messages++ }

func TestFake_AnswersWhatItIsTold(t *testing.T) {
	fake := New(t)
	fake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		return []presidio.Finding{{EntityType: "PERSON", End: len(req.Text)}}, nil
	})
	fake.OnSupportedEntities(func(_ context.Context, language string) ([]string, error) {
		return []string{"PERSON", language}, nil
	})

	findings, err := fake.Analyze(context.Background(), &presidio.AnalyzeRequest{Text: "Jane"})
	require.NoError(t, err)
	require.Equal(t, []presidio.Finding{{EntityType: "PERSON", End: 4}}, findings)
	entities, err := fake.SupportedEntities(context.Background(), "fr")
	require.NoError(t, err)
	require.Equal(t, []string{"PERSON", "fr"}, entities)

	require.Equal(t, Calls{Analyze: 1, SupportedEntities: 1}, fake.Calls())
}

func TestFake_FailsTheTestOnACallItWasNotToldAbout(t *testing.T) {
	recorder := &failed{}
	fake := New(recorder)

	_, err := fake.Analyze(context.Background(), &presidio.AnalyzeRequest{})
	require.Error(t, err)
	_, err = fake.SupportedEntities(context.Background(), "en")
	require.Error(t, err)

	require.Equal(t, 2, recorder.messages)
}

func TestFinding_CountsCharacters(t *testing.T) {
	fake := Finding(t, "PERSON", "Zoé")

	findings, err := fake.Analyze(context.Background(), &presidio.AnalyzeRequest{Text: "Très chère Zoé, bonjour"})
	require.NoError(t, err)
	require.Equal(t, []presidio.Finding{{EntityType: "PERSON", Start: 11, End: 14, Score: 0.85}}, findings)

	findings, err = fake.Analyze(context.Background(), &presidio.AnalyzeRequest{Text: "nobody here"})
	require.NoError(t, err)
	require.Empty(t, findings)
}
