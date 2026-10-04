package job

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// The highest value an integer type holds when it is signed, by the name the three
// dialects give it. SQL Server's tinyint holds up to 255: the signed bound fits it too.
var highestIntegers = map[string]int64{
	"tinyint":   math.MaxInt8,
	"smallint":  math.MaxInt16,
	"int2":      math.MaxInt16,
	"mediumint": 1<<23 - 1,
	"int":       math.MaxInt32,
	"integer":   math.MaxInt32,
	"int4":      math.MaxInt32,
	"serial":    math.MaxInt32,
}

// The types that hold a number of digits and no more: numeric(p,s).
var fixedPointTypes = map[string]bool{"numeric": true, "decimal": true, "dec": true, "number": true}

// The precision and the scale a fixed-point type is written with: (p,s) or (p).
var fixedPointArguments = regexp.MustCompile(`\(\s*(\d+)\s*(?:,\s*(\d+)\s*)?\)`)

// The widest whole part a float64 holds without losing units.
const exactDigits = 15

// typeWord is the first word of a type's name: "int" for "int(11) unsigned".
func typeWord(dataType string) string {
	words := strings.Fields(baseType(dataType))
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

// highestValue is the highest number a column holds, when its type bounds it: the width
// of an integer, the digits of a fixed-point number. A catalogue that names the type
// alone gives the precision and the scale beside it. False for a type that bounds
// nothing a generated number would reach.
func highestValue(column *sqlmanager_shared.DatabaseSchemaRow) (float64, bool) {
	if column == nil {
		return 0, false
	}
	word := typeWord(column.DataType)
	if highest, ok := highestIntegers[word]; ok {
		return float64(highest), true
	}
	if !fixedPointTypes[word] {
		return 0, false
	}
	precision, scale := column.NumericPrecision, max(column.NumericScale, 0)
	if written := fixedPointArguments.FindStringSubmatch(column.DataType); written != nil {
		precision, _ = strconv.Atoi(written[1])
		scale = 0
		if written[2] != "" {
			scale, _ = strconv.Atoi(written[2])
		}
	}
	whole := precision - scale
	switch {
	case precision <= 0 || whole > exactDigits:
		return 0, false
	case whole <= 0:
		// No digit before the point: the highest value is 0.99… at the scale.
		return 1 - math.Pow10(-scale), true
	}
	return math.Pow10(whole) - 1, true
}

// fitRange cuts a range at the highest value a column holds. A range that starts above it
// starts at zero instead.
func fitRange(lowest, highest, limit float64) (fitLowest, fitHighest float64) {
	highest = math.Min(highest, limit)
	if lowest > highest {
		lowest = 0
	}
	return lowest, highest
}

// fitColumn bounds the numbers a config generates by what the column holds.
func fitColumn(config *mgmtv1alpha1.TransformerConfig, column *sqlmanager_shared.DatabaseSchemaRow) {
	limit, bounded := highestValue(column)
	if !bounded {
		return
	}
	if generated := config.GetGenerateInt64Config(); generated != nil {
		lowest, highest := fitRange(float64(generated.GetMin()), float64(generated.GetMax()), limit)
		low, high := int64(lowest), int64(highest)
		generated.Min, generated.Max = &low, &high
	}
	if generated := config.GetGenerateFloat64Config(); generated != nil {
		lowest, highest := fitRange(generated.GetMin(), generated.GetMax(), limit)
		generated.Min, generated.Max = &lowest, &highest
	}
}
