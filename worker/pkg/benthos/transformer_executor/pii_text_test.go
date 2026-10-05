package transformer_executor

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio/presidiotest"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// engineFinding is an engine whose analyzer finds part, as an entity, in every text that
// carries it. It records the requests it receives in requests, when given.
func engineFinding(t testing.TB, part, entity string, requests *[]*presidio.AnalyzeRequest) *piitext.Engine {
	t.Helper()
	fake := presidiotest.New(t)
	fake.OnAnalyze(func(_ context.Context, req *presidio.AnalyzeRequest) ([]presidio.Finding, error) {
		if requests != nil {
			*requests = append(*requests, req)
		}
		at := strings.Index(req.Text, part)
		if at < 0 {
			return nil, nil
		}
		start := utf8.RuneCountInString(req.Text[:at])
		return []presidio.Finding{{
			EntityType: entity, Start: start, End: start + utf8.RuneCountInString(part), Score: 0.85,
		}}, nil
	})
	engine, err := piitext.NewEngine(fake, "fr")
	require.NoError(t, err)
	return engine
}

func findingJohnDoe(t testing.TB) *piitext.Engine {
	t.Helper()
	return engineFinding(t, "John Doe", "PERSON", nil)
}

func piiTextConfig(config *mgmtv1alpha1.TransformPiiText) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{TransformPiiTextConfig: config},
	}
}

func transformWith(config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.PiiAnonymizer {
	return &mgmtv1alpha1.PiiAnonymizer{Config: &mgmtv1alpha1.PiiAnonymizer_Transform_{
		Transform: &mgmtv1alpha1.PiiAnonymizer_Transform{Config: config},
	}}
}

func Test_TransformPiiText_Values(t *testing.T) {
	executor, err := InitializeTransformerByConfigType(
		context.Background(), piiTextConfig(&mgmtv1alpha1.TransformPiiText{}), WithPiiText(findingJohnDoe(t), nil),
	)
	require.NoError(t, err)

	t.Run("a null value stays null", func(t *testing.T) {
		result, err := executor.Mutate(nil, executor.Opts)
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("an empty value stays empty", func(t *testing.T) {
		result, err := executor.Mutate("", executor.Opts)
		require.NoError(t, err)
		require.Equal(t, "", result)
	})

	t.Run("a value that is no text is refused", func(t *testing.T) {
		_, err := executor.Mutate(42, executor.Opts)
		require.Error(t, err)
	})

	t.Run("a null value stays null over the network too", func(t *testing.T) {
		remote, err := InitializeTransformerByConfigType(
			context.Background(), piiTextConfig(nil), WithTransformPiiTextApi(fakePiiTextApi{out: "bar"}),
		)
		require.NoError(t, err)
		result, err := remote.Mutate(nil, remote.Opts)
		require.NoError(t, err)
		require.Nil(t, result)
	})
}

func Test_TransformPiiText_Configuration(t *testing.T) {
	t.Run("the configuration is left as it was given, and the engine chooses the language", func(t *testing.T) {
		var requests []*presidio.AnalyzeRequest
		config := piiTextConfig(&mgmtv1alpha1.TransformPiiText{})
		before := proto.CloneOf(config)
		executor, err := InitializeTransformerByConfigType(
			context.Background(), config, WithPiiText(engineFinding(t, "John Doe", "PERSON", &requests), nil),
		)
		require.NoError(t, err)
		_, err = executor.Mutate("Hello, John Doe!", executor.Opts)
		require.NoError(t, err)

		require.True(t, proto.Equal(before, config))
		require.Len(t, requests, 1)
		require.Equal(t, "fr", requests[0].Language)
	})

	t.Run("a configuration that cannot be applied builds no executor", func(t *testing.T) {
		char := "ab"
		_, err := InitializeTransformerByConfigType(
			context.Background(),
			piiTextConfig(&mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
				Config: &mgmtv1alpha1.PiiAnonymizer_Mask_{Mask: &mgmtv1alpha1.PiiAnonymizer_Mask{MaskingChar: &char}},
			}}),
			WithPiiText(findingJohnDoe(t), nil),
		)
		var configErr *piitext.ConfigError
		require.ErrorAs(t, err, &configErr)
	})

	t.Run("the hashes are computed under the key the executor is given", func(t *testing.T) {
		algo := mgmtv1alpha1.PiiAnonymizer_Hash_HASH_TYPE_SHA256
		config := piiTextConfig(&mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: &mgmtv1alpha1.PiiAnonymizer{
			Config: &mgmtv1alpha1.PiiAnonymizer_Hash_{Hash: &mgmtv1alpha1.PiiAnonymizer_Hash{Algo: &algo}},
		}})
		hashUnder := func(engine *piitext.Engine, key *piitext.HashKey) any {
			executor, err := InitializeTransformerByConfigType(context.Background(), config, WithPiiText(engine, key))
			require.NoError(t, err)
			result, err := executor.Mutate("Hello, John Doe!", executor.Opts)
			require.NoError(t, err)
			return result
		}
		one, other := &piitext.HashKey{1}, &piitext.HashKey{2}
		require.Equal(t, hashUnder(findingJohnDoe(t), one), hashUnder(findingJohnDoe(t), one),
			"two processes hash a text the same way under the key of one scope")
		require.NotEqual(t, hashUnder(findingJohnDoe(t), one), hashUnder(findingJohnDoe(t), other))
		require.NotEqual(t, hashUnder(findingJohnDoe(t), nil), hashUnder(findingJohnDoe(t), nil),
			"without a key each process hashes under its own")
	})
}

func Test_TransformPiiText_Transformers(t *testing.T) {
	text := "Hello, John Doe!"
	run := func(t *testing.T, anonymizer *mgmtv1alpha1.PiiAnonymizer, opts ...TransformerExecutorOption) (any, error) {
		t.Helper()
		config := piiTextConfig(&mgmtv1alpha1.TransformPiiText{DefaultAnonymizer: anonymizer})
		opts = append(opts, WithPiiText(findingJohnDoe(t), nil))
		executor, err := InitializeTransformerByConfigType(context.Background(), config, opts...)
		require.NoError(t, err)
		return executor.Mutate(text, executor.Opts)
	}

	t.Run("a generator puts what it generates in place of the finding", func(t *testing.T) {
		result, err := run(t, transformWith(nil))
		require.NoError(t, err)
		require.Regexp(t, `^Hello, .+!$`, result)
		require.NotContains(t, result, "John Doe")
	})

	t.Run("a transformer receives the finding and its answer takes its place", func(t *testing.T) {
		result, err := run(t, transformWith(&mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
				TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return "[" + value + "]";`},
			},
		}))
		require.NoError(t, err)
		require.Equal(t, "Hello, [John Doe]!", result)
	})

	t.Run("a number a transformer answers is written as it prints", func(t *testing.T) {
		result, err := run(t, transformWith(&mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateCardNumberConfig{
				GenerateCardNumberConfig: &mgmtv1alpha1.GenerateCardNumber{},
			},
		}))
		require.NoError(t, err)
		require.Regexp(t, `^Hello, [0-9]+!$`, result)
	})

	t.Run("a user-defined transformer is resolved with the resolver of the executor", func(t *testing.T) {
		resolver := NewMockUserDefinedTransformerResolver(t)
		resolver.On("GetUserDefinedTransformer", context.Background(), "some-id").Once().
			Return(&mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}, nil)
		result, err := run(t, transformWith(&mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
				UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: "some-id"},
			},
		}), WithUserDefinedTransformerResolver(resolver))
		require.NoError(t, err)
		require.Equal(t, text, result)
	})

	t.Run("a transformer that fails fails the value", func(t *testing.T) {
		result, err := run(t, transformWith(&mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
				TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `throw new Error("no");`},
			},
		}))
		require.Error(t, err)
		require.Empty(t, result)
	})
}

func Test_textOf(t *testing.T) {
	text, number := "text", int64(42)
	var none *string
	require.Equal(t, "text", textOf("text"))
	require.Equal(t, "text", textOf(&text))
	require.Equal(t, "", textOf(nil))
	require.Equal(t, "", textOf(none))
	require.Equal(t, "42", textOf(number))
	require.Equal(t, "42", textOf(&number))
	require.Equal(t, "true", textOf(true))
}
