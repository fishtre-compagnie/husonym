package piitext

import (
	"context"
	"strings"
	"unicode/utf8"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// operator rewrites the text of one finding of an entity type.
type operator func(ctx context.Context, entity, text string) (string, error)

// operators are the operators of a configuration: one per entity type that has its own, and
// the one of every other entity type.
type operators struct {
	byEntity map[string]operator
	fallback operator
}

// newOperators reads the operators of a configuration. An anonymizer that sets no kind counts
// as absent: an entity type with such an entry gets the default, and such a default writes the
// entity type between angle brackets, as no default does.
func newOperators(config *mgmtv1alpha1.TransformPiiText, key HashKey, build SnippetTransformerBuilder) *operators {
	snippets := &snippets{build: build}
	ops := &operators{
		byEntity: make(map[string]operator, len(config.GetEntityAnonymizers())),
		fallback: newOperator(config.GetDefaultAnonymizer(), key, snippets, "default"),
	}
	if ops.fallback == nil {
		ops.fallback = replaceOperator("")
	}
	for entity, anonymizer := range config.GetEntityAnonymizers() {
		if op := newOperator(anonymizer, key, snippets, "entity:"+entity); op != nil {
			ops.byEntity[entity] = op
		}
	}
	return ops
}

// of returns the operator of an entity type: its own, else the default.
func (o *operators) of(entity string) operator {
	if op, ok := o.byEntity[entity]; ok {
		return op
	}
	return o.fallback
}

// newOperator returns the operator an anonymizer configures, or nil when it sets no kind. name
// tells the anonymizer apart from the others of its configuration.
func newOperator(anonymizer *mgmtv1alpha1.PiiAnonymizer, key HashKey, snippets *snippets, name string) operator {
	switch kind := anonymizer.GetConfig().(type) {
	case *mgmtv1alpha1.PiiAnonymizer_Replace_:
		return replaceOperator(kind.Replace.GetValue())
	case *mgmtv1alpha1.PiiAnonymizer_Redact_:
		return redactOperator
	case *mgmtv1alpha1.PiiAnonymizer_Mask_:
		return maskOperator(kind.Mask)
	case *mgmtv1alpha1.PiiAnonymizer_Hash_:
		return hashOperator(key, kind.Hash.GetAlgo())
	case *mgmtv1alpha1.PiiAnonymizer_Transform_:
		return snippets.operator(name, kind.Transform.GetConfig())
	default:
		return nil
	}
}

// replaceOperator puts value in place of a finding, or its entity type between angle brackets when
// there is no value.
func replaceOperator(value string) operator {
	return func(_ context.Context, entity, _ string) (string, error) {
		if value == "" {
			return "<" + entity + ">", nil
		}
		return value, nil
	}
}

// redactOperator removes a finding.
func redactOperator(context.Context, string, string) (string, error) {
	return "", nil
}

// maskOperator overwrites characters of a finding with the masking character, from its start or from
// its end. Without a count, all of them; with one, that many, and none when it is not positive.
// Without a masking character, the characters it would overwrite are removed.
//
// Characters are Unicode code points: the letter and the accent of a decomposed letter are two.
func maskOperator(config *mgmtv1alpha1.PiiAnonymizer_Mask) operator {
	return func(_ context.Context, _, text string) (string, error) {
		length := utf8.RuneCountInString(text)
		count := length
		if config.CharsToMask != nil {
			count = min(length, max(0, int(config.GetCharsToMask())))
		}
		masking := strings.Repeat(config.GetMaskingChar(), count)
		if config.GetFromEnd() {
			return text[:offsetOf(text, length-count)] + masking, nil
		}
		return masking + text[offsetOf(text, count):], nil
	}
}

// offsetOf returns the byte offset of the character at index in text, and the length of text
// for the index one past its last character.
func offsetOf(text string, index int) int {
	for offset := range text {
		if index == 0 {
			return offset
		}
		index--
	}
	return len(text)
}
