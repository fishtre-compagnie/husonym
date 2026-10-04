package piidetect

import (
	"slices"
	"strings"
)

// columnKind is what the SQL type of a column can hold, as far as the name rules care.
type columnKind int

const (
	// kindAny is a type that is not given, or one that can hold text: character types,
	// JSON, binary. It refuses no rule.
	kindAny columnKind = iota
	kindNumber
	kindMoment
	kindBoolean
	// kindReference is a type made to identify a row: uuid, uniqueidentifier.
	kindReference
)

// The types that hold a yes or a no. MySQL writes its boolean tinyint(1); SQL Server's
// is bit.
var booleanTypes = wordSet("bool", "boolean", "bit", "bit(1)", "tinyint(1)")

var referenceTypes = wordSet("uuid", "uniqueidentifier")

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
	case isNumericType(t):
		return kindNumber
	default:
		return kindAny
	}
}

// holds tells whether a column of that kind can hold the datum of the rule.
func (ru *rule) holds(kind columnKind) bool {
	return kind == kindAny || slices.Contains(ru.alsoHolds, kind)
}
