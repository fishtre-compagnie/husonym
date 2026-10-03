package ee_transformer_fns

import (
	"context"
	"errors"
	"net/http"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

func Test_TransformPiiText(t *testing.T) {
	ctx := context.Background()
	t.Run("empty", func(t *testing.T) {
		actual, err := TransformPiiText(ctx, nil, nil, nil, nil, "", testutil.GetTestLogger(t))
		require.NoError(t, err)
		require.Equal(t, "", actual)
	})

	t.Run("ok", func(t *testing.T) {
		mockText := "bar"
		presidioFake := presidiotest.Rewriting(t, []presidio.Finding{{}}, mockText)
		mockhusonym := NewMockHusonymOperatorApi(t)

		config := &mgmtv1alpha1.TransformPiiText{}

		actual, err := TransformPiiText(
			ctx,
			presidioFake,
			presidioFake,
			mockhusonym,
			config,
			"foo",
			testutil.GetTestLogger(t),
		)
		require.NoError(t, err)
		require.Equal(t, mockText, actual)
		require.Equal(t, presidiotest.Calls{Analyze: 1, Anonymize: 1}, presidioFake.Calls())
	})

	t.Run("what the configuration sets is what the analyzer and the anonymizer are asked", func(t *testing.T) {
		var analyzed presidio.AnalyzeRequest
		var anonymized presidio.AnonymizeRequest
		presidioFake := presidiotest.New(t)
		presidioFake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			analyzed = *req
			return []presidio.Finding{
				{EntityType: "PERSON", Start: 11, End: 24, Score: 0.85},
				{EntityType: "TITLE", Start: 0, End: 2, Score: 1},
			}, nil
		})
		presidioFake.OnAnonymize(func(_ context.Context, req *presidio.AnonymizeRequest) (*presidio.AnonymizeResult, error) {
			anonymized = *req
			return &presidio.AnonymizeResult{Text: "My name is <PERSON> prepare to die"}, nil
		})

		language := "en"
		threshold := float32(0.5)
		text := "My name is Inigo Montoya prepare to die"
		actual, err := TransformPiiText(
			ctx,
			presidioFake,
			presidioFake,
			NewMockHusonymOperatorApi(t),
			&mgmtv1alpha1.TransformPiiText{
				Language:        &language,
				ScoreThreshold:  threshold,
				AllowedEntities: []string{"PERSON", "TITLE"},
				AllowedPhrases:  []string{"My"},
				DenyRecognizers: []*mgmtv1alpha1.PiiDenyRecognizer{{Name: "TITLE", DenyWords: []string{"My"}}},
				DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
					Config: &mgmtv1alpha1.PiiAnonymizer_Redact_{Redact: &mgmtv1alpha1.PiiAnonymizer_Redact{}},
				},
			},
			text,
			testutil.GetTestLogger(t),
		)
		require.NoError(t, err)
		require.Equal(t, "My name is <PERSON> prepare to die", actual)

		sent := float64(threshold)
		require.Equal(t, presidio.AnalyzeRequest{
			Text:           text,
			Language:       "en",
			ScoreThreshold: &sent,
			Entities:       []string{"PERSON", "TITLE"},
			AdHocRecognizers: []presidio.AdHocRecognizer{{
				Name: "TITLE", SupportedEntity: "TITLE", SupportedLanguage: "en", DenyList: []string{"My"},
			}},
		}, analyzed)
		// The finding of an allowed phrase is left out; the others are sent as they were found.
		require.Equal(t, presidio.AnonymizeRequest{
			Text:      text,
			Findings:  []presidio.Finding{{EntityType: "PERSON", Start: 11, End: 24, Score: 0.85}},
			Operators: map[string]presidio.Operator{presidio.DefaultOperatorKey: presidio.Redact()},
		}, anonymized)
	})

	t.Run("an analyzer that fails is the error, and the anonymizer is not called", func(t *testing.T) {
		presidioFake := presidiotest.New(t)
		presidioFake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			return nil, errors.Join(presidio.ErrNoAnswer, errors.New("connection refused"))
		})

		_, err := TransformPiiText(
			ctx, presidioFake, presidioFake, NewMockHusonymOperatorApi(t),
			&mgmtv1alpha1.TransformPiiText{}, "foo", testutil.GetTestLogger(t),
		)
		require.ErrorIs(t, err, presidio.ErrNoAnswer)
		require.ErrorContains(t, err, "unable to analyze input")
	})

	t.Run("an anonymizer that refuses is the error, with its words", func(t *testing.T) {
		presidioFake := presidiotest.New(t)
		presidioFake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
			return []presidio.Finding{}, nil
		})
		presidioFake.OnAnonymize(func(context.Context, *presidio.AnonymizeRequest) (*presidio.AnonymizeResult, error) {
			return nil, &presidio.RefusedError{
				Operation: "anonymize", StatusCode: http.StatusUnprocessableEntity, Message: "422 err",
			}
		})

		_, err := TransformPiiText(
			ctx, presidioFake, presidioFake, NewMockHusonymOperatorApi(t),
			&mgmtv1alpha1.TransformPiiText{}, "foo", testutil.GetTestLogger(t),
		)
		var refused *presidio.RefusedError
		require.ErrorAs(t, err, &refused)
		require.Equal(t, http.StatusUnprocessableEntity, refused.StatusCode)
		require.ErrorContains(t, err, "unable to anonymize input")
		require.ErrorContains(t, err, "422 err")
	})
}

func Test_removeAllowedPhrases(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		actual := removeAllowedPhrases(
			[]presidio.Finding{
				{
					Start:      11,
					End:        24,
					Score:      0.85,
					EntityType: "person",
				},
			},
			"My name is Inigo Montoya prepare to die",
			[]string{"Inigo Montoya"},
		)
		require.Empty(t, actual)
	})

	t.Run("invalid_range_skip", func(t *testing.T) {
		actual := removeAllowedPhrases(
			[]presidio.Finding{
				{
					Start:      500,
					End:        600,
					Score:      0.85,
					EntityType: "person",
				},
			},
			"My name is Inigo Montoya prepare to die",
			[]string{"Inigo Montoya"},
		)
		require.Empty(t, actual)
	})

	t.Run("not_found", func(t *testing.T) {
		input := []presidio.Finding{
			{
				Start:      11,
				End:        24,
				Score:      0.85,
				EntityType: "person",
			},
		}
		actual := removeAllowedPhrases(
			input,
			"My name is Inigo Montoya prepare to die",
			[]string{"Inigo"},
		)
		require.Equal(t, input, actual)
	})
}

func Test_buildAnonymizers(t *testing.T) {
	t.Run("entities", func(t *testing.T) {
		replaceVal := "newval"
		hashAlgo := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256
		output := buildAnonymizers(&mgmtv1alpha1.TransformPiiText{
			DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
				Config: &mgmtv1alpha1.PiiAnonymizer_Replace_{
					Replace: &mgmtv1alpha1.PiiAnonymizer_Replace{
						Value: &replaceVal,
					},
				},
			},
			EntityAnonymizers: map[string]*mgmtv1alpha1.PiiAnonymizer{
				"PERSON": {
					Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{
						Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{
							Algo: &hashAlgo,
						},
					},
				},
				"DATE_TIME": {
					Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
						Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{
							Config: &mgmtv1alpha1.TransformerConfig{
								Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{},
							},
						},
					},
				},
			},
		})
		require.Equal(t, map[string]presidio.Operator{
			"DEFAULT":           presidio.Replace(replaceVal),
			"PERSON":            presidio.Hash(presidio.HashSHA256),
			"HUSONYM_DATE_TIME": presidio.Replace("{{HUSONYM_DATE_TIME}}"),
		}, output)
	})

	t.Run("no anonymizer sets no operator", func(t *testing.T) {
		require.Empty(t, buildAnonymizers(&mgmtv1alpha1.TransformPiiText{}))
	})
}

func Test_toPresidioAnonymizerConfig(t *testing.T) {
	t.Run("redact", func(t *testing.T) {
		actual, ok := toPresidioAnonymizerConfig("", &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Redact_{
				Redact: &mgmtv1alpha1.PiiAnonymizer_Redact{},
			},
		})
		require.True(t, ok)
		require.Equal(t, presidio.Redact(), actual)
	})

	t.Run("replace", func(t *testing.T) {
		newval := "newval"
		actual, ok := toPresidioAnonymizerConfig("", &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Replace_{
				Replace: &mgmtv1alpha1.PiiAnonymizer_Replace{
					Value: &newval,
				},
			},
		})
		require.True(t, ok)
		require.Equal(t, presidio.Replace(newval), actual)
	})

	t.Run("hash", func(t *testing.T) {
		sha512 := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA512
		actual, ok := toPresidioAnonymizerConfig("", &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{
				Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{
					Algo: &sha512,
				},
			},
		})
		require.True(t, ok)
		require.Equal(t, presidio.Hash(presidio.HashSHA512), actual)
	})

	t.Run("mask", func(t *testing.T) {
		maskingChar := "*"
		charsTomask := int32(5)
		fromend := false
		actual, ok := toPresidioAnonymizerConfig("", &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Mask_{
				Mask: &mgmtv1alpha1.PiiAnonymizer_Mask{
					MaskingChar: &maskingChar,
					CharsToMask: &charsTomask,
					FromEnd:     &fromend,
				},
			},
		})
		require.True(t, ok)
		require.Equal(t, presidio.Mask("*", 5, false), actual)
	})

	t.Run("default", func(t *testing.T) {
		_, ok := toPresidioAnonymizerConfig("", nil)
		require.False(t, ok)
	})

	t.Run("transform", func(t *testing.T) {
		actual, ok := toPresidioAnonymizerConfig("PERSON", &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
				Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{},
			},
		})
		require.True(t, ok)
		require.Equal(t, presidio.Replace("{{HUSONYM_PERSON}}"), actual)
	})
}

func Test_toPresidioHashType(t *testing.T) {
	t.Run("md5", func(t *testing.T) {
		actual := toPresidioHashType(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_MD5)
		require.Equal(t, presidio.HashMD5, actual)
	})

	t.Run("sha256", func(t *testing.T) {
		actual := toPresidioHashType(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256)
		require.Equal(t, presidio.HashSHA256, actual)
	})

	t.Run("sha512", func(t *testing.T) {
		actual := toPresidioHashType(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA512)
		require.Equal(t, presidio.HashSHA512, actual)
	})

	t.Run("default", func(t *testing.T) {
		actual := toPresidioHashType(mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_UNSPECIFIED)
		require.Equal(t, presidio.HashMD5, actual)
	})
}
