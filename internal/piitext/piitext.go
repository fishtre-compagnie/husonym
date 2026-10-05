// Package piitext anonymizes free text: the transformer TransformPiiText.
//
// A Presidio analyzer finds the personal data of a value; this package rewrites it. Each finding
// is rewritten by the operator configured for its entity type, else by the default operator,
// else by its entity type between angle brackets. Text no finding designates is left as it is,
// byte for byte.
//
// The analyzer counts positions in characters (Unicode code points). They are turned into byte
// offsets once, when the findings are read (locate), and nothing else here reads the positions
// of a finding.
//
// A value is rewritten exactly or not at all: an analyzer that fails, a finding that does not
// fit the text, a transformer that fails each fail the value, and no text is returned for it.
package piitext

import (
	"context"
	"fmt"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
)

// fallbackLanguage is the language of a text when neither the configuration nor the deployment
// names one.
const fallbackLanguage = "en"

// Engine anonymizes free text with one Presidio analyzer. It is safe for concurrent use.
type Engine struct {
	analyzer        presidio.Analyzer
	defaultLanguage string
	// processKey is the hash key of the transformers that are given none: drawn when the
	// engine is built, it lives as long as the process.
	processKey HashKey
}

// NewEngine returns the engine of an analyzer. defaultLanguage is the language of a
// configuration that sets none; empty means "en". It draws the hash key of the process.
func NewEngine(analyzer presidio.Analyzer, defaultLanguage string) (*Engine, error) {
	key, err := drawHashKey()
	if err != nil {
		return nil, err
	}
	if defaultLanguage == "" {
		defaultLanguage = fallbackLanguage
	}
	return &Engine{analyzer: analyzer, defaultLanguage: defaultLanguage, processKey: key}, nil
}

// Options is what a Transformer needs beyond its configuration.
type Options struct {
	// Build runs the Husonym transformers of the transform operators. Without it, a value in
	// which such an operator has something to rewrite fails.
	Build SnippetTransformerBuilder
	// HashKey is the key of the consistency scope the values belong to. Nil: the key of the
	// process, which hashes a text the same way only as long as the process lives.
	HashKey *HashKey
}

// Transformer anonymizes values under one configuration. It is safe for concurrent use.
type Transformer struct {
	analyzer  presidio.Analyzer
	request   presidio.AnalyzeRequest
	allowed   allowedPhrases
	operators *operators
}

// Transformer returns the transformer of a configuration; nil is the empty configuration. A
// configuration that cannot be applied is a *ConfigError. The configuration is never modified.
func (e *Engine) Transformer(config *mgmtv1alpha1.TransformPiiText, opts Options) (*Transformer, error) {
	if err := Validate(config); err != nil {
		return nil, err
	}
	key := e.processKey
	if opts.HashKey != nil {
		key = *opts.HashKey
	}
	return &Transformer{
		analyzer:  e.analyzer,
		request:   analyzeRequest(config, e.defaultLanguage),
		allowed:   newAllowedPhrases(config.GetAllowedPhrases()),
		operators: newOperators(config, key, opts.Build),
	}, nil
}

// Transform returns value with every personal data found in it rewritten, or an error and no
// text. An empty value is returned as it is, without any call.
func (t *Transformer) Transform(ctx context.Context, value string) (string, error) {
	if value == "" {
		return "", nil
	}

	request := t.request
	request.Text = value
	findings, err := t.analyzer.Analyze(ctx, &request)
	if err != nil {
		return "", fmt.Errorf("unable to analyze input: %w", err)
	}
	located, err := locate(value, findings)
	if err != nil {
		return "", fmt.Errorf("unable to analyze input: %w", err)
	}
	targets := resolve(value, t.allowed.filter(value, located))

	// The output is built from left to right, from the value as it was given: nothing is
	// rewritten in place, so no position moves.
	var out strings.Builder
	out.Grow(len(value))
	end := 0
	for _, target := range targets {
		rewritten, err := t.operators.of(target.entity)(ctx, target.entity, target.of(value))
		if err != nil {
			return "", fmt.Errorf("unable to transform %s entity: %w", target.entity, err)
		}
		out.WriteString(value[end:target.start])
		out.WriteString(rewritten)
		end = target.end
	}
	out.WriteString(value[end:])
	return out.String(), nil
}
