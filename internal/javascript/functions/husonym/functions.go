package husonym_functions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"

	"github.com/dop251/goja"
	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
)

const (
	// Namespace is the global object exposing the husonym functions to JavaScript.
	Namespace = "husonym"
	// LegacyNamespace is the name the same object had before the Neosync rename.
	LegacyNamespace = "neosync"

	namespace = Namespace
)

// Get returns the husonym functions. transformPiiText calls the PII text API the run's
// context holds (transformers.ContextWithPiiTextApi), and fails without one.
func Get() ([]*javascript_functions.FunctionDefinition, error) {
	generatorFns, err := getHusonymGenerators()
	if err != nil {
		return nil, err
	}
	transformerFns, err := getHusonymTransformers()
	if err != nil {
		return nil, err
	}
	patchStructuredMessage := getPatchStructuredMessage(namespace)

	output := make(
		[]*javascript_functions.FunctionDefinition,
		0,
		len(generatorFns)+len(transformerFns)+1,
	)
	output = append(output, generatorFns...)
	output = append(output, transformerFns...)
	output = append(output, patchStructuredMessage)
	return output, nil
}

func getPatchStructuredMessage(namespace string) *javascript_functions.FunctionDefinition {
	fnName := "patchStructuredMessage"
	return javascript_functions.NewFunctionDefinition(
		namespace,
		fnName,
		func(r javascript_functions.Runner) javascript_functions.Function {
			return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (result any, err error) {
				defer func() {
					if r := recover(); r != nil {
						// we set the named "err" argument to the error so that it can be returned
						err = fmt.Errorf("panic recovered: %s.%s: %v", namespace, fnName, r)
						l.Error(
							"recovered from panic in custom husonym function",
							"error", err,
							"function", fmt.Sprintf("%s.%s", namespace, fnName),
							"stack", string(debug.Stack()),
						)
					}
				}()
				var updates map[string]any
				if err := javascript_functions.ParseFunctionArguments(call, &updates); err != nil {
					return nil, err
				}

				originalData, err := r.ValueApi().AsStructured()
				if err != nil {
					return nil, fmt.Errorf("failed to get structured data: %w", err)
				}

				originalMap, ok := originalData.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("structured data is not a map")
				}

				for key, value := range updates {
					setNestedProperty(originalMap, key, javascript_functions.FromScript(value))
				}

				r.ValueApi().SetStructured(originalMap)

				return nil, nil
			}
		},
	)
}

func setNestedProperty(obj map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := obj

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
		} else {
			if _, ok := current[part]; !ok {
				current[part] = make(map[string]any)
			}
			current = current[part].(map[string]any)
		}
	}
}

func getHusonymGenerators() ([]*javascript_functions.FunctionDefinition, error) {
	generators := transformers.GetHusonymGenerators()
	fns := make([]*javascript_functions.FunctionDefinition, 0, len(generators))
	for _, f := range generators {
		templateData, err := f.GetJsTemplateData()
		if err != nil {
			return nil, err
		}

		fn := javascript_functions.NewFunctionDefinition(
			namespace,
			templateData.Name,
			func(r javascript_functions.Runner) javascript_functions.Function {
				return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (result any, err error) {
					defer func() {
						if r := recover(); r != nil {
							// we set the named "err" argument to the error so that it can be returned
							err = fmt.Errorf(
								"panic recovered: %s.%s: %v",
								namespace,
								templateData.Name,
								r,
							)
							l.Error(
								"recovered from panic in custom husonym function",
								"error", err,
								"function", fmt.Sprintf("%s.%s", namespace, templateData.Name),
								"stack", string(debug.Stack()),
							)
						}
					}()
					var (
						opts map[string]any
					)

					if err := javascript_functions.ParseFunctionArguments(call, &opts); err != nil {
						return nil, err
					}
					goOpts, err := f.ParseOptions(opts)
					if err != nil {
						return nil, err
					}
					return f.Generate(goOpts)
				}
			},
		)
		fns = append(fns, fn)
	}
	return fns, nil
}

func getHusonymTransformers() ([]*javascript_functions.FunctionDefinition, error) {
	husonymTransformers := transformers.GetHusonymTransformers()
	fns := make([]*javascript_functions.FunctionDefinition, 0, len(husonymTransformers)+1)
	for _, f := range husonymTransformers {
		fn, err := transformerFunction(f, func(context.Context) (transformers.HusonymTransformer, error) {
			return f, nil
		})
		if err != nil {
			return nil, err
		}
		fns = append(fns, fn)
	}

	// The API is the one of the account the script runs for, which changes from one run to
	// the next: it comes with the run's context.
	piiText, err := transformerFunction(transformers.NewTransformPiiText(nil),
		func(ctx context.Context) (transformers.HusonymTransformer, error) {
			api := transformers.PiiTextApiFromContext(ctx)
			if api == nil {
				return nil, errors.New("transformPiiText is not available here")
			}
			return transformers.NewTransformPiiText(api), nil
		})
	if err != nil {
		return nil, err
	}
	return append(fns, piiText), nil
}

// transformerFunction exposes a transformer to scripts, under the name of template. The
// transformer that runs is the one resolve returns for the run.
func transformerFunction(
	template transformers.HusonymTransformer,
	resolve func(context.Context) (transformers.HusonymTransformer, error),
) (*javascript_functions.FunctionDefinition, error) {
	templateData, err := template.GetJsTemplateData()
	if err != nil {
		return nil, err
	}
	return javascript_functions.NewFunctionDefinition(
		namespace,
		templateData.Name,
		func(r javascript_functions.Runner) javascript_functions.Function {
			return func(ctx context.Context, call goja.FunctionCall, rt *goja.Runtime, l *slog.Logger) (result any, err error) {
				defer func() {
					if r := recover(); r != nil {
						// we set the named "err" argument to the error so that it can be returned
						err = fmt.Errorf(
							"panic recovered: %s.%s: %v",
							namespace,
							templateData.Name,
							r,
						)
						l.Error(
							"recovered from panic in custom husonym function",
							"error", err,
							"function", fmt.Sprintf("%s.%s", namespace, templateData.Name),
							"stack", string(debug.Stack()),
						)
					}
				}()
				var (
					value any
					opts  map[string]any
				)

				if err := javascript_functions.ParseFunctionArguments(call, &value, &opts); err != nil {
					return nil, err
				}
				f, err := resolve(ctx)
				if err != nil {
					return nil, err
				}
				goOpts, err := f.ParseOptions(opts)
				if err != nil {
					return nil, err
				}
				return f.Transform(value, goOpts)
			}
		},
	), nil
}
