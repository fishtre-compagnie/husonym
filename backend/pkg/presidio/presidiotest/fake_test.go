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
	fake.OnAnonymize(func(context.Context, *presidio.AnonymizeRequest) (*presidio.AnonymizeResult, error) {
		return &presidio.AnonymizeResult{Text: "<PERSON>"}, nil
	})
	fake.OnSupportedEntities(func(_ context.Context, language string) ([]string, error) {
		return []string{"PERSON", language}, nil
	})

	findings, err := fake.Analyze(context.Background(), &presidio.AnalyzeRequest{Text: "Jane"})
	require.NoError(t, err)
	require.Equal(t, []presidio.Finding{{EntityType: "PERSON", End: 4}}, findings)
	result, err := fake.Anonymize(context.Background(), &presidio.AnonymizeRequest{Text: "Jane"})
	require.NoError(t, err)
	require.Equal(t, "<PERSON>", result.Text)
	entities, err := fake.SupportedEntities(context.Background(), "fr")
	require.NoError(t, err)
	require.Equal(t, []string{"PERSON", "fr"}, entities)

	require.Equal(t, Calls{Analyze: 1, Anonymize: 1, SupportedEntities: 1}, fake.Calls())
}

func TestFake_FailsTheTestOnACallItWasNotToldAbout(t *testing.T) {
	recorder := &failed{}
	fake := New(recorder)

	_, err := fake.Analyze(context.Background(), &presidio.AnalyzeRequest{})
	require.Error(t, err)
	_, err = fake.Anonymize(context.Background(), &presidio.AnonymizeRequest{})
	require.Error(t, err)
	_, err = fake.SupportedEntities(context.Background(), "en")
	require.Error(t, err)

	require.Equal(t, 3, recorder.messages)
}
