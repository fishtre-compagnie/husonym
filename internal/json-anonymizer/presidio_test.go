package jsonanonymizer

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/stretchr/testify/require"
)

func piiTextMapping(config *mgmtv1alpha1.TransformPiiText) Option {
	return WithTransformerMappings([]*mgmtv1alpha1.TransformerMapping{{
		Expression: ".note",
		Transformer: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: config},
		},
	}})
}

func engineOf(t *testing.T, analyzer presidio.Analyzer) *piitext.Engine {
	t.Helper()
	engine, err := piitext.NewEngine(analyzer, "")
	require.NoError(t, err)
	return engine
}

// The caller of the anonymizer tells a Presidio that did not answer from a value that could not
// be transformed: the error of Presidio stays the cause of the one returned.
func Test_AnonymizeJSONObject_KeepsWhyPresidioFailed(t *testing.T) {
	presidioFake := presidiotest.New(t)
	presidioFake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		return nil, errors.Join(presidio.ErrNoAnswer, errors.New("connection refused"))
	})
	anonymizer, err := NewAnonymizer(
		context.Background(),
		piiTextMapping(&mgmtv1alpha1.TransformPiiText{}),
		WithPiiText(engineOf(t, presidioFake), true, nil),
	)
	require.NoError(t, err)

	_, err = anonymizer.AnonymizeJSONObject(`{"note":"call Jane"}`)

	require.ErrorIs(t, err, presidio.ErrNoAnswer)
	require.ErrorContains(t, err, "failed to anonymize JSON")
}

func Test_WithPiiText(t *testing.T) {
	t.Run("a text is rewritten where the analyzer finds personal data", func(t *testing.T) {
		anonymizer, err := NewAnonymizer(
			context.Background(),
			piiTextMapping(&mgmtv1alpha1.TransformPiiText{}),
			WithPiiText(engineOf(t, presidiotest.Finding(t, "PERSON", "Zoé")), true, nil),
		)
		require.NoError(t, err)

		out, err := anonymizer.AnonymizeJSONObject(`{"note":"appeler Zoé demain"}`)
		require.NoError(t, err)
		require.JSONEq(t, `{"note":"appeler <PERSON> demain"}`, out)
	})

	t.Run("without a license the transformer is not enabled, and Presidio is not called", func(t *testing.T) {
		_, err := NewAnonymizer(
			context.Background(),
			piiTextMapping(&mgmtv1alpha1.TransformPiiText{}),
			WithPiiText(engineOf(t, presidiotest.New(t)), false, nil),
		)
		require.ErrorContains(t, err, "TransformPiiText is not enabled")
	})

	t.Run("without an engine the transformer is not enabled", func(t *testing.T) {
		_, err := NewAnonymizer(
			context.Background(),
			piiTextMapping(&mgmtv1alpha1.TransformPiiText{}),
			WithPiiText(nil, true, nil),
		)
		require.ErrorContains(t, err, "TransformPiiText is not enabled")
	})

	t.Run("the hashes are computed under the key of the request", func(t *testing.T) {
		algo := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_MD5
		config := &mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{Algo: &algo}},
		}}
		hashUnder := func(key *piitext.HashKey) string {
			anonymizer, err := NewAnonymizer(
				context.Background(),
				piiTextMapping(config),
				WithPiiText(engineOf(t, presidiotest.Finding(t, "PERSON", "Zoé")), true, key),
			)
			require.NoError(t, err)
			out, err := anonymizer.AnonymizeJSONObject(`{"note":"Zoé"}`)
			require.NoError(t, err)
			return out
		}
		require.Equal(t, hashUnder(&piitext.HashKey{1}), hashUnder(&piitext.HashKey{1}))
		require.NotEqual(t, hashUnder(&piitext.HashKey{1}), hashUnder(&piitext.HashKey{2}))
		require.NotEqual(t, hashUnder(&piitext.HashKey{1}), hashUnder(nil))
	})
}
