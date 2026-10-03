package piitext

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/stretchr/testify/require"
)

// found is the finding an analyzer answers for the first occurrence of part in text: its
// positions count characters, as those of Presidio do.
func found(t testing.TB, text, part, entity string, score float64) presidio.Finding {
	t.Helper()
	at := strings.Index(text, part)
	require.GreaterOrEqual(t, at, 0, "%q is not in %q", part, text)
	start := utf8.RuneCountInString(text[:at])
	return presidio.Finding{
		EntityType: entity,
		Start:      start,
		End:        start + utf8.RuneCountInString(part),
		Score:      score,
	}
}

// finding answers an analyzer that finds the same findings in every text.
func finding(t testing.TB, findings ...presidio.Finding) *presidiotest.Fake {
	t.Helper()
	fake := presidiotest.New(t)
	fake.OnAnalyze(func(context.Context, *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		return findings, nil
	})
	return fake
}

func newEngine(t testing.TB, analyzer presidio.Analyzer) *Engine {
	t.Helper()
	engine, err := NewEngine(analyzer, "")
	require.NoError(t, err)
	return engine
}

// rewrite transforms value under config, with an analyzer that answers findings.
func rewrite(
	t testing.TB,
	config *mgmtv1alpha1.TransformPiiText,
	opts Options,
	value string,
	findings ...presidio.Finding,
) (string, error) {
	t.Helper()
	transformer, err := newEngine(t, finding(t, findings...)).Transformer(config, opts)
	require.NoError(t, err)
	return transformer.Transform(context.Background(), value)
}

func mustRewrite(
	t testing.TB,
	config *mgmtv1alpha1.TransformPiiText,
	opts Options,
	value string,
	findings ...presidio.Finding,
) string {
	t.Helper()
	out, err := rewrite(t, config, opts, value, findings...)
	require.NoError(t, err)
	return out
}

func replaceWith(value string) *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Replace_{
		Replace: &mgmtv1alpha1.PiiAnonymizer_Replace{Value: &value},
	}}
}

func replaceByEntity() *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Replace_{
		Replace: &mgmtv1alpha1.PiiAnonymizer_Replace{},
	}}
}

func redact() *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Redact_{
		Redact: &mgmtv1alpha1.PiiAnonymizer_Redact{},
	}}
}

func mask(char string, count int32, fromEnd bool) *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Mask_{
		Mask: &mgmtv1alpha1.PiiAnonymizer_Mask{MaskingChar: &char, CharsToMask: &count, FromEnd: &fromEnd},
	}}
}

func maskAll(char string) *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Mask_{
		Mask: &mgmtv1alpha1.PiiAnonymizer_Mask{MaskingChar: &char},
	}}
}

func hashOf(algo mgmtv1alpha1.PiiAnonymizer_Hash_HashType) *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{
		Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{Algo: &algo},
	}}
}

func transformWith(config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
		Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: config},
	}}
}

func withDefault(anonymizer *mgmtv1alpha1.PiiAnonymizer) *mgmtv1alpha1.TransformPiiText {
	return &mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: anonymizer}
}

// bracketing is a builder of snippet transformers that put what they receive between
// brackets, and records every snippet and every configuration it was built for.
type bracketing struct {
	snippets []string
	built    []*mgmtv1alpha1.TransformerConfig
}

func (b *bracketing) build(
	_ context.Context,
	config *mgmtv1alpha1.TransformerConfig,
) (SnippetTransformer, error) {
	b.built = append(b.built, config)
	return func(_ context.Context, snippet string) (string, error) {
		b.snippets = append(b.snippets, snippet)
		return "[" + snippet + "]", nil
	}, nil
}

var passthrough = &mgmtv1alpha1.TransformerConfig{
	Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
}
