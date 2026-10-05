package piitext

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// requested is the request the analyzer receives for a value under config, from an engine
// whose deployment language is defaultLanguage.
func requested(t *testing.T, config *mgmtv1alpha1.TransformPiiText, defaultLanguage string) *presidio.AnalyzeRequest {
	t.Helper()
	var got *presidio.AnalyzeRequest
	fake := presidiotest.New(t)
	fake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		got = req
		return nil, nil
	})
	engine, err := NewEngine(fake, defaultLanguage)
	require.NoError(t, err)
	transformer, err := engine.Transformer(config, Options{})
	require.NoError(t, err)
	_, err = transformer.Transform(context.Background(), "some text")
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func Test_Request(t *testing.T) {
	french, empty := "fr", ""

	t.Run("the text is sent as it is", func(t *testing.T) {
		require.Equal(t, "some text", requested(t, nil, "").Text)
	})

	t.Run("the language is the one of the configuration", func(t *testing.T) {
		require.Equal(t, "fr", requested(t, &mgmtv1alpha1.TransformPiiText{Language: &french}, "de").Language)
	})

	t.Run("a configuration without a language is analyzed in the language of the deployment", func(t *testing.T) {
		require.Equal(t, "de", requested(t, &mgmtv1alpha1.TransformPiiText{}, "de").Language)
		require.Equal(t, "de", requested(t, &mgmtv1alpha1.TransformPiiText{Language: &empty}, "de").Language)
		require.Equal(t, "de", requested(t, nil, "de").Language)
	})

	t.Run("without a language anywhere the text is analyzed as English", func(t *testing.T) {
		require.Equal(t, "en", requested(t, &mgmtv1alpha1.TransformPiiText{}, "").Language)
	})

	t.Run("the recognizers of deny lists are declared in the language of the request", func(t *testing.T) {
		config := &mgmtv1alpha1.TransformPiiText{
			Language: &french,
			DenyRecognizers: []*mgmtv1alpha1.PiiDenyRecognizer{
				{Name: "vip-list", DenyWords: []string{"falcon", "acme"}},
				{Name: "codes", DenyWords: []string{"x-1"}},
			},
		}
		require.Equal(t, []presidio.AdHocRecognizer{
			{Name: "vip-list", SupportedEntity: "vip-list", SupportedLanguage: "fr", DenyList: []string{"falcon", "acme"}},
			{Name: "codes", SupportedEntity: "codes", SupportedLanguage: "fr", DenyList: []string{"x-1"}},
		}, requested(t, config, "").AdHocRecognizers)
	})

	t.Run("without a restriction every entity is looked for", func(t *testing.T) {
		config := &mgmtv1alpha1.TransformPiiText{
			DenyRecognizers: []*mgmtv1alpha1.PiiDenyRecognizer{{Name: "vip-list", DenyWords: []string{"falcon"}}},
		}
		require.Empty(t, requested(t, config, "").Entities)
	})

	t.Run("the names of deny lists join a restricted list of entities", func(t *testing.T) {
		config := &mgmtv1alpha1.TransformPiiText{
			AllowedEntities: []string{"PERSON", "codes"},
			DenyRecognizers: []*mgmtv1alpha1.PiiDenyRecognizer{
				{Name: "vip-list", DenyWords: []string{"falcon"}},
				{Name: "codes", DenyWords: []string{"x-1"}},
			},
		}
		require.Equal(t, []string{"PERSON", "codes", "vip-list"}, requested(t, config, "").Entities)
	})

	t.Run("the threshold is sent as the decimal that was configured", func(t *testing.T) {
		for threshold, want := range map[float32]float64{0: 0, 0.35: 0.35, 0.5: 0.5, 0.6: 0.6, 0.85: 0.85, 1: 1} {
			got := requested(t, &mgmtv1alpha1.TransformPiiText{ScoreThreshold: threshold}, "").ScoreThreshold
			require.NotNil(t, got)
			require.Equal(t, want, *got) //nolint:testifylint // the exact decimal is the point
		}
	})

	t.Run("no configuration sends a threshold of zero", func(t *testing.T) {
		got := requested(t, nil, "").ScoreThreshold
		require.NotNil(t, got)
		require.Zero(t, *got)
	})

	t.Run("the configuration is left as it was given", func(t *testing.T) {
		config := &mgmtv1alpha1.TransformPiiText{
			AllowedEntities: []string{"PERSON"},
			DenyRecognizers: []*mgmtv1alpha1.PiiDenyRecognizer{{Name: "vip-list", DenyWords: []string{"falcon"}}},
		}
		before := proto.CloneOf(config)
		requested(t, config, "de")
		require.True(t, proto.Equal(before, config))
	})
}
