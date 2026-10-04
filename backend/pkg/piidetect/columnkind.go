package piidetect

import (
	"regexp"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// columnKind is what the SQL type of a column can hold, as far as the name rules care.
type columnKind int

const (
	// kindAny is a type that is not given, or one that can hold text: character types,
	// JSON, binary. It refuses no rule.
	kindAny columnKind = iota
	// kindNumber stands for both kinds of numbers in what a rule accepts.
	kindNumber
	kindInteger
	kindDecimal
	kindMoment
	kindBoolean
	// kindReference is a type made to identify a row: uuid, uniqueidentifier.
	kindReference
)

// The types that hold a yes or a no. MySQL writes its boolean tinyint(1); SQL Server's
// is bit.
var booleanTypes = wordSet("bool", "boolean", "bit", "bit(1)", "tinyint(1)")

var referenceTypes = wordSet("uuid", "uniqueidentifier")

// The numeric types that hold a fraction, or are written as if they did.
var decimalTypeRe = regexp.MustCompile(`numeric|decimal|number|float|double|real|money`)

func kindOf(dataType string) columnKind {
	t := strings.ToLower(strings.TrimSpace(dataType))
	switch {
	case t == "":
		return kindAny
	case booleanTypes[t]:
		return kindBoolean
	case referenceTypes[t]:
		return kindReference
	case isTemporalType(t), strings.HasPrefix(t, "time"), strings.HasPrefix(t, "interval"), t == "year":
		return kindMoment
	case decimalTypeRe.MatchString(t):
		return kindDecimal
	case isNumericType(t):
		return kindInteger
	default:
		return kindAny
	}
}

// holds tells whether a column of that kind can hold the datum of the rule.
func (ru *rule) holds(kind columnKind) bool {
	if kind == kindInteger || kind == kindDecimal {
		kind = kindNumber
	}
	return kind == kindAny || slices.Contains(ru.alsoHolds, kind)
}

// The transformers that write a value of a type whatever the datum: every finding has
// one to suggest, so that a column found sensitive is never left as it is for want of a
// transformer. None of them reads the value it replaces, except the scramble, which
// keeps its length and draws each character again in its class: a letter for a letter, a
// digit for a digit, a sign for a sign. All of them draw at random: two rows of the same
// value do not get the same output.
const (
	scrambleText    = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE
	generateInteger = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
	generateDecimal = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FLOAT64
	generateBoolean = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_BOOL
	generateMoment  = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UTCTIMESTAMP
)

// SuggestionForBirthDate is the transformer suggested for a column whose values are
// birth dates, as the rule of that name suggests it.
func SuggestionForBirthDate(dataType string) mgmtv1alpha1.TransformerSource {
	return suggestionFor(dataType, scrambleText, unspecified, generateMoment)
}

// suggestionFor picks the transformer to suggest for a column of a type. text is the
// one of the datum, for a column that holds text or whose type is not given; integer and
// moment are the ones of the datum for an integer and for a temporal column, when it has
// any. A column of another type gets a transformer that writes a value of that type.
func suggestionFor(dataType string, text, integer, moment mgmtv1alpha1.TransformerSource) mgmtv1alpha1.TransformerSource {
	switch kindOf(dataType) {
	case kindInteger:
		if integer != unspecified {
			return integer
		}
		return generateInteger
	case kindDecimal:
		return generateDecimal
	case kindMoment:
		if moment != unspecified {
			return moment
		}
		return generateMoment
	case kindBoolean:
		return generateBoolean
	default:
		return text
	}
}
