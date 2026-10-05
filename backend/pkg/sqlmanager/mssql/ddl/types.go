package ddl

import (
	"fmt"
	"strconv"
)

// How a system type takes its numbers.
type typeShape int

const (
	shapeBare typeShape = iota
	// shapeBytes is written with its length in bytes, or max.
	shapeBytes
	// shapeCharacters is written with its length in characters, two bytes each, or max.
	shapeCharacters
	shapePrecisionScale
	shapePrecision
	shapeScale
)

// systemTypes is the list of the system types a column is written with. It is closed: a name
// that is not in it is never written.
var systemTypes = map[string]typeShape{
	"bigint":           shapeBare,
	"int":              shapeBare,
	"smallint":         shapeBare,
	"tinyint":          shapeBare,
	"bit":              shapeBare,
	"money":            shapeBare,
	"smallmoney":       shapeBare,
	"decimal":          shapePrecisionScale,
	"numeric":          shapePrecisionScale,
	"float":            shapePrecision,
	"real":             shapeBare,
	"char":             shapeBytes,
	"varchar":          shapeBytes,
	"binary":           shapeBytes,
	"varbinary":        shapeBytes,
	"nchar":            shapeCharacters,
	"nvarchar":         shapeCharacters,
	"sysname":          shapeBare,
	"date":             shapeBare,
	"datetime":         shapeBare,
	"smalldatetime":    shapeBare,
	"time":             shapeScale,
	"datetime2":        shapeScale,
	"datetimeoffset":   shapeScale,
	"uniqueidentifier": shapeBare,
	"sql_variant":      shapeBare,
	"text":             shapeBare,
	"ntext":            shapeBare,
	"image":            shapeBare,
	"timestamp":        shapeBare,
	"xml":              shapeBare,
	"hierarchyid":      shapeBare,
	"geometry":         shapeBare,
	"geography":        shapeBare,
}

// isSystemType tells whether a name is in the list of system types.
func isSystemType(name string) bool {
	_, ok := systemTypes[name]
	return ok
}

// systemType writes a system type with the numbers sys.columns gives of it: maxLength in bytes,
// -1 for max. A name that is not in the list of system types is written as nothing.
func systemType(name string, maxLength, precision, scale int) string {
	if !isSystemType(name) {
		return ""
	}
	switch systemTypes[name] {
	case shapeBytes:
		return name + "(" + length(maxLength, 1) + ")"
	case shapeCharacters:
		return name + "(" + length(maxLength, 2) + ")"
	case shapePrecisionScale:
		return fmt.Sprintf("%s(%d,%d)", name, precision, scale)
	case shapePrecision:
		return fmt.Sprintf("%s(%d)", name, precision)
	case shapeScale:
		return fmt.Sprintf("%s(%d)", name, scale)
	default:
		return name
	}
}

func length(maxLength, bytesPerUnit int) string {
	if maxLength == -1 {
		return "max"
	}
	return strconv.Itoa(maxLength / bytesPerUnit)
}

// columnType writes the type of a column: an alias by its name alone, which carries its numbers,
// a system type with them. A column whose type cannot be written is refused before.
func columnType(column *Column) string {
	if column.IsUserDefinedType {
		return QualifiedName(column.TypeSchema, column.TypeName)
	}
	return systemType(column.TypeName, column.MaxLength, column.Precision, column.Scale)
}

// CharacterLength is the length of a type that has one, in characters for nchar and nvarchar,
// in bytes for char, varchar, binary and varbinary. It is -1 for max, and for the types that
// have no length. baseType is the system type the column is stored as.
func CharacterLength(baseType string, maxLength int) int {
	if maxLength == -1 {
		return -1
	}
	switch systemTypes[baseType] {
	case shapeBytes:
		return maxLength
	case shapeCharacters:
		return maxLength / 2
	default:
		return -1
	}
}
