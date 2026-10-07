package telemetry

import (
	"regexp"
	"strings"
)

// temporalVersion is the shape of the version of the Temporal server the report may carry.
var temporalVersion = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){0,2}$`)

// TemporalVersion is the version as given when it is only digits and dots, empty otherwise.
func TemporalVersion(raw string) string {
	if temporalVersion.MatchString(raw) {
		return raw
	}
	return ""
}

// lengthSuffix is a length or a precision in parentheses, as in varchar(255) or numeric(10,2).
var lengthSuffix = regexp.MustCompile(`\s*\([^)]*\)`)

// typeNamesByFamily lists the type names of PostgreSQL, MySQL and SQL Server, lower-cased and
// without length, under their family.
var typeNamesByFamily = map[string][]string{
	"integer": {
		"smallint", "integer", "int", "bigint", "int2", "int4", "int8", "serial", "smallserial",
		"bigserial", "serial2", "serial4", "serial8", "tinyint", "mediumint", "year",
	},
	"decimal": {"numeric", "decimal", "money", "smallmoney", "dec", "fixed"},
	"float":   {"real", "float", "float4", "float8", "double", "double precision"},
	"boolean": {"boolean", "bool", "bit"},
	"text": {
		"text",
		"varchar",
		"char",
		"character",
		"character varying",
		"bpchar",
		"citext",
		"name",
		"nchar",
		"nvarchar",
		"ntext",
		"tinytext",
		"mediumtext",
		"longtext",
	},
	"binary": {"bytea", "blob", "tinyblob", "mediumblob", "longblob", "binary", "varbinary", "image"},
	"date":   {"date"},
	"time":   {"time", "timetz", "time with time zone", "time without time zone"},
	"timestamp": {
		"timestamp",
		"timestamptz",
		"datetime",
		"datetime2",
		"smalldatetime",
		"datetimeoffset",
		"timestamp with time zone",
		"timestamp without time zone",
	},
	"interval": {"interval"},
	"uuid":     {"uuid", "uniqueidentifier"},
	"json":     {"json", "jsonb"},
	"xml":      {"xml"},
	"enum":     {"enum", "set"},
	"network":  {"inet", "cidr", "macaddr", "macaddr8"},
	"geometric": {
		"point", "line", "lseg", "box", "path", "polygon", "circle", "geometry", "geography",
		"linestring", "multipoint", "multilinestring", "multipolygon", "geometrycollection",
	},
}

// columnTypeFamilies is typeNamesByFamily turned around, to look a type name up.
var columnTypeFamilies = func() map[string]string {
	families := make(map[string]string)
	for family, names := range typeNamesByFamily {
		for _, name := range names {
			families[name] = family
		}
	}
	return families
}()

// ColumnTypeFamily is the family of a column type as a database spells it: a length or precision
// in parentheses is dropped, a trailing [] or a leading _ (the catalog's spelling) makes an array,
// MySQL's unsigned and zerofill are dropped, and a type that is not a built-in one (a domain, an
// enum or a type of the customer) is other.
func ColumnTypeFamily(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasSuffix(name, "[]") || strings.HasPrefix(name, "_") {
		return "array"
	}
	name = strings.Join(strings.Fields(lengthSuffix.ReplaceAllString(name, "")), " ")
	name = strings.TrimSuffix(strings.TrimSuffix(name, " zerofill"), " unsigned")
	if family, ok := columnTypeFamilies[name]; ok {
		return family
	}
	return other
}
