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
	// kindAny is a type that is not given, or one the rules do not know: a type of the
	// schema (a domain, an enum), JSON, binary, an array. It refuses no rule.
	kindAny columnKind = iota
	// kindText is a character type.
	kindText
	// kindNumber stands for both kinds of numbers in what a rule accepts.
	kindNumber
	kindInteger
	kindDecimal
	kindMoment
	kindBoolean
	// kindReference is a type made to identify a row: uuid, uniqueidentifier.
	kindReference
)

// The types by their name alone, as the three dialects write them: without a schema, a
// length, a precision or an attribute (see typeName).
var typeKinds = kindsByName(map[columnKind][]string{
	kindText: {
		"text", "varchar", "char", "character", "character varying", "bpchar", "citext", "nchar", "nvarchar",
		"ntext", "national character", "national character varying", "tinytext", "mediumtext", "longtext",
	},
	kindInteger: {
		"int", "integer", "smallint", "bigint", "tinyint", "mediumint", "int2", "int4", "int8",
		"serial", "smallserial", "bigserial", "serial2", "serial4", "serial8",
	},
	// The numeric types that hold a fraction, or are written as if they did.
	kindDecimal: {
		"numeric", "decimal", "dec", "number", "float", "float4", "float8", "double", "double precision", "real",
		"money", "smallmoney",
	},
	kindMoment: {
		"date", "time", "timetz", "timestamp", "timestamptz", "datetime", "datetime2", "smalldatetime",
		"datetimeoffset", "time without time zone", "time with time zone", "timestamp without time zone",
		"timestamp with time zone", "interval", "year",
	},
	// SQL Server's boolean is bit; MySQL writes its own tinyint(1) (see kindOf).
	kindBoolean:   {"bool", "boolean", "bit"},
	kindReference: {"uuid", "uniqueidentifier"},
})

// The integer types that hold a number of ten digits or more: a phone number, a card
// number.
var wideIntegerTypes = wordSet("bigint", "int8", "bigserial", "serial8")

func kindsByName(names map[columnKind][]string) map[string]columnKind {
	out := map[string]columnKind{}
	for kind, list := range names {
		for _, name := range list {
			out[name] = kind
		}
	}
	return out
}

var (
	typeArguments  = regexp.MustCompile(`\([^)]*\)`)
	typeAttributes = regexp.MustCompile(` (unsigned|signed|zerofill)\b`)
	typeSpaces     = regexp.MustCompile(`\s+`)
)

// typeName is the name of a type alone, in lowercase: what a catalogue writes without
// the schema before it, the arguments in parentheses and MySQL's attributes after it.
// arguments are those of the type, as written; array tells a type written with [].
func typeName(dataType string) (name, arguments string, array bool) {
	t := strings.ToLower(strings.TrimSpace(dataType))
	array = strings.HasSuffix(t, "[]")
	arguments = typeArguments.FindString(t)
	t = typeArguments.ReplaceAllString(strings.TrimRight(t, "[] "), "")
	t = typeAttributes.ReplaceAllString(t, "")
	if dot := strings.LastIndex(t, "."); dot >= 0 {
		t = t[dot+1:]
	}
	t = strings.Trim(typeSpaces.ReplaceAllString(t, " "), "\"`[] ")
	return t, arguments, array
}

func kindOf(dataType string) columnKind {
	name, arguments, array := typeName(dataType)
	if array {
		return kindAny
	}
	if strings.HasPrefix(name, "interval ") {
		return kindMoment
	}
	kind, known := typeKinds[name]
	switch {
	case !known:
		return kindAny
	case name == "tinyint" && arguments == "(1)":
		return kindBoolean
	case name == "bit" && arguments != "" && arguments != "(1)":
		// A string of bits.
		return kindAny
	}
	return kind
}

// holds tells whether a column of that kind can hold the datum of the rule.
func (ru *rule) holds(kind columnKind) bool {
	if kind == kindInteger || kind == kindDecimal {
		kind = kindNumber
	}
	return kind == kindAny || kind == kindText || slices.Contains(ru.alsoHolds, kind)
}

// The transformers that write a value of a type whatever the datum: every finding in a
// column of a type one of them writes has a transformer to suggest. None of them reads
// the value it replaces, except the scramble, which keeps its length and draws each
// character again in its class: a letter for a letter, a digit for a digit, a sign for a
// sign. The generators draw at random: two rows of the same value do not get the same
// output. The scramble does the same, except under a consistency scope, where equal
// values give equal outputs.
const (
	scrambleText      = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_CHARACTER_SCRAMBLE
	generateInteger   = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64
	generateDecimal   = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FLOAT64
	generateBoolean   = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_BOOL
	generateMoment    = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UTCTIMESTAMP
	generateReference = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UUID
)

// SuggestionForBirthDate is the transformer suggested for a column whose values are
// birth dates, as the rule of that name suggests it.
func SuggestionForBirthDate(dataType string) mgmtv1alpha1.TransformerSource {
	return suggestionFor(dataType, scrambleText, unspecified, generateMoment)
}

// suggestionFor picks the transformer to suggest for a column of a type: one that takes
// the type, or none. text is the one of the datum, for a column that holds text or whose
// type is not given; integer and moment are the ones of the datum for an integer and for
// a temporal column, when it has any. The datum's own integers are ten digits or more:
// a narrower integer column gets the generator of integers. A number, a boolean, a
// moment or an identifier column gets a transformer that writes a value of its type. A
// column of any other type — JSON, binary, an array, a type of the schema — gets none.
func suggestionFor(dataType string, text, integer, moment mgmtv1alpha1.TransformerSource) mgmtv1alpha1.TransformerSource {
	switch kindOf(dataType) {
	case kindInteger:
		if name, _, _ := typeName(dataType); integer != unspecified && wideIntegerTypes[name] {
			return integer
		}
		return generateInteger
	case kindReference:
		return generateReference
	case kindText:
		return text
	case kindAny:
		if strings.TrimSpace(dataType) == "" {
			return text
		}
		return unspecified
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
		return unspecified
	}
}
