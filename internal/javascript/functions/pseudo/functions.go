// Package pseudo_functions offers scripts the deterministic functions of Athanor, under
// the pseudo namespace: the same value gives the same output, on every row, table and run
// of the job's consistency scope — what the native transformers give, and what the random
// husonym functions cannot.
//
// The functions derive from the consistency scope of the run in progress, which the
// engine hands to each run through its context (ContextWithSource): a VM serves any job.
// Benthos has no consistency scope, and a call there fails.
package pseudo_functions

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dop251/goja"
	javascript_functions "github.com/fishtre-compagnie/husonym/internal/javascript/functions"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
)

// Namespace is the global object holding the functions.
const Namespace = "pseudo"

// Kinds are the fakes offered as pseudo.<kind>(value): each returns what the native
// transformer of the same kind returns for the value.
var Kinds = []string{
	"firstName", "lastName", "fullName", "email", "phone",
	"city", "state", "zipcode", "streetAddress", "country", "businessName",
}

// Source is the consistency scope of a run.
type Source interface {
	// Fake returns what the native transformer of kind returns for value.
	Fake(kind string, value any) (any, error)
	// Seed derives the seed of value in a domain of the rule's own, apart from the
	// domains of the native transformers.
	Seed(domain string, value any) consistency.Seed
}

type sourceKey struct{}

// ContextWithSource hands the consistency scope of a run to the functions it calls.
func ContextWithSource(ctx context.Context, source Source) context.Context {
	if source == nil {
		return ctx
	}
	return context.WithValue(ctx, sourceKey{}, source)
}

func sourceFromContext(ctx context.Context, name string) (Source, error) {
	source, _ := ctx.Value(sourceKey{}).(Source)
	if source == nil {
		return nil, fmt.Errorf("%s.%s is only available when Athanor runs the job", Namespace, name)
	}
	return source, nil
}

// Get returns the functions.
func Get() []*javascript_functions.FunctionDefinition {
	fns := make([]*javascript_functions.FunctionDefinition, 0, len(Kinds)+3)
	for _, kind := range Kinds {
		fns = append(fns, define(kind, func(source Source, call goja.FunctionCall) (any, error) {
			var value any
			if err := javascript_functions.ParseFunctionArguments(call, &value); err != nil {
				return nil, err
			}
			if value == nil {
				return nil, nil
			}
			return source.Fake(kind, value)
		}))
	}
	fns = append(fns,
		// hash(value, domain): the hexadecimal digest of the value in the domain.
		define("hash", func(source Source, call goja.FunctionCall) (any, error) {
			value, domain, err := valueAndDomain(call, 2)
			if err != nil || value == nil {
				return nil, err
			}
			seed := source.Seed(domain, value)
			return hex.EncodeToString(seed[:]), nil
		}),
		// int(value, domain, min, max): an integer of [min, max].
		define("int", func(source Source, call goja.FunctionCall) (any, error) {
			if len(call.Arguments) != 4 {
				return nil, errors.New("expects (value, domain, min, max)")
			}
			var (
				value  any
				domain string
				lo, hi int64
			)
			if err := javascript_functions.ParseFunctionArguments(call, &value, &domain, &lo, &hi); err != nil {
				return nil, err
			}
			if domain == "" {
				return nil, errors.New("the domain must not be empty")
			}
			if lo > hi {
				return nil, fmt.Errorf("min %d is above max %d", lo, hi)
			}
			if value == nil {
				return nil, nil
			}
			return source.Seed(domain, value).IntInRange(lo, hi), nil
		}),
		// pick(list, value, domain): an element of the list.
		define("pick", func(source Source, call goja.FunctionCall) (any, error) {
			if len(call.Arguments) != 3 {
				return nil, errors.New("expects (list, value, domain)")
			}
			var (
				list   []any
				value  any
				domain string
			)
			if err := javascript_functions.ParseFunctionArguments(call, &list, &value, &domain); err != nil {
				return nil, err
			}
			if len(list) == 0 {
				return nil, errors.New("the list must not be empty")
			}
			if domain == "" {
				return nil, errors.New("the domain must not be empty")
			}
			if value == nil {
				return nil, nil
			}
			return list[source.Seed(domain, value).Index(len(list))], nil
		}),
	)
	return fns
}

// valueAndDomain parses (value, domain), the first arguments of a primitive.
func valueAndDomain(call goja.FunctionCall, want int) (value any, domain string, err error) {
	if len(call.Arguments) != want {
		return nil, "", errors.New("expects (value, domain)")
	}
	if err := javascript_functions.ParseFunctionArguments(call, &value, &domain); err != nil {
		return nil, "", err
	}
	if domain == "" {
		return nil, "", errors.New("the domain must not be empty")
	}
	return value, domain, nil
}

func define(name string, fn func(Source, goja.FunctionCall) (any, error)) *javascript_functions.FunctionDefinition {
	return javascript_functions.NewFunctionDefinition(Namespace, name,
		func(javascript_functions.Runner) javascript_functions.Function {
			return func(ctx context.Context, call goja.FunctionCall, _ *goja.Runtime, _ *slog.Logger) (any, error) {
				source, err := sourceFromContext(ctx, name)
				if err != nil {
					return nil, err
				}
				result, err := fn(source, call)
				if err != nil {
					return nil, fmt.Errorf("%s.%s: %w", Namespace, name, err)
				}
				return result, nil
			}
		})
}
