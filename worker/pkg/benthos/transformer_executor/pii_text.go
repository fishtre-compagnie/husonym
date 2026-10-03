package transformer_executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
)

// WithPiiText enables TransformPiiText in this process, on the engine that anonymizes free
// text. hashKey is the key of the consistency scope the values belong to, nil when they belong
// to none. A nil engine enables nothing.
func WithPiiText(engine *piitext.Engine, hashKey *piitext.HashKey) TransformerExecutorOption {
	return func(c *TransformerExecutorConfig) {
		c.piiText = engine
		c.piiTextHashKey = hashKey
	}
}

// WithTransformPiiTextApi enables TransformPiiText (and the PII helpers exposed to
// JavaScript transformers) through an already-built API: that of callers that reach the engine
// over the network. It takes precedence over WithPiiText.
func WithTransformPiiTextApi(api transformers.TransformPiiTextApi) TransformerExecutorOption {
	return func(c *TransformerExecutorConfig) {
		c.piiTextApi = api
	}
}

// withoutPiiText is the option of the transformers TransformPiiText runs on what it finds:
// they may be any transformer but TransformPiiText itself.
func withoutPiiText(c *TransformerExecutorConfig) {
	c.piiText = nil
	c.piiTextHashKey = nil
	c.piiTextApi = nil
}

var errPiiTextNotEnabled = fmt.Errorf("transformer: TransformPiiText is not enabled: %w", errors.ErrUnsupported)

// piiTextOptions is what the engine needs beyond a configuration, for an executor built with
// opts.
func (c *TransformerExecutorConfig) piiTextOptions(opts []TransformerExecutorOption) piitext.Options {
	return piitext.Options{Build: snippetTransformerBuilder(opts), HashKey: c.piiTextHashKey}
}

// piiTextApi returns what the scripts of an executor built with opts call as transformPiiText,
// or nil when TransformPiiText is not enabled.
func (c *TransformerExecutorConfig) resolvePiiTextApi(opts []TransformerExecutorOption) transformers.TransformPiiTextApi {
	if c.piiTextApi != nil {
		return c.piiTextApi
	}
	if c.piiText != nil {
		return &enginePiiTextApi{engine: c.piiText, options: c.piiTextOptions(opts)}
	}
	return nil
}

// enginePiiTextApi answers a script from the engine of this process. A script gives its
// configuration with every call, so each call builds its transformer.
type enginePiiTextApi struct {
	engine  *piitext.Engine
	options piitext.Options
}

func (p *enginePiiTextApi) Transform(
	ctx context.Context,
	config *mgmtv1alpha1.TransformPiiText,
	value string,
) (string, error) {
	transformer, err := p.engine.Transformer(config, p.options)
	if err != nil {
		return "", err
	}
	return transformer.Transform(ctx, value)
}

// piiTextTransform returns what rewrites the values of one TransformPiiText configuration, for
// an executor built with opts. In this process the transformer is built once, here; over the
// network every value carries its configuration.
func (c *TransformerExecutorConfig) piiTextTransform(
	config *mgmtv1alpha1.TransformPiiText,
	opts []TransformerExecutorOption,
) (func(ctx context.Context, value string) (string, error), error) {
	switch {
	case c.piiTextApi != nil:
		if config == nil {
			config = &mgmtv1alpha1.TransformPiiText{}
		}
		return func(ctx context.Context, value string) (string, error) {
			return c.piiTextApi.Transform(ctx, config, value)
		}, nil
	case c.piiText != nil:
		transformer, err := c.piiText.Transformer(config, c.piiTextOptions(opts))
		if err != nil {
			return nil, err
		}
		return transformer.Transform, nil
	default:
		return nil, errPiiTextNotEnabled
	}
}

// snippetTransformerBuilder builds the transformers TransformPiiText runs on what it finds, as
// executors that share the options of the one they serve.
func snippetTransformerBuilder(opts []TransformerExecutorOption) piitext.SnippetTransformerBuilder {
	opts = append(slices.Clone(opts), withoutPiiText)
	return func(ctx context.Context, config *mgmtv1alpha1.TransformerConfig) (piitext.SnippetTransformer, error) {
		executor, err := InitializeTransformerByConfigType(ctx, config, opts...)
		if err != nil {
			return nil, err
		}
		return func(_ context.Context, snippet string) (string, error) {
			result, err := executor.Mutate(snippet, executor.Opts)
			if err != nil {
				return "", err
			}
			return textOf(result), nil
		}, nil
	}
}

// textOf is what a transformer answered, as the text that takes the place of a finding: a
// string as it is, nothing as an empty text, anything else as it prints.
func textOf(result any) string {
	value := reflect.ValueOf(result)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return ""
	}
	if text, ok := value.Interface().(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value.Interface())
}
