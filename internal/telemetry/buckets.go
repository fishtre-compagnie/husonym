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

// sourceMajor is the shape of the major version of a source engine: one or two numbers.
var sourceMajor = regexp.MustCompile(`^\d{1,3}(\.\d{1,3})?$`)

// SourceMajor is the major version of a source engine as given when it is one or two numbers,
// such as 16 or 8.0, empty otherwise: the caller then leaves the row out.
func SourceMajor(raw string) string {
	if sourceMajor.MatchString(raw) {
		return raw
	}
	return ""
}

// versionShape is the version of the software: a release, or a build stamped with a short suffix
// such as -rc.1 or the default v0.0.0-main. The suffix is one or two parts of letters and digits,
// sixteen at most each, joined by a dot: a longer tail could be the name of a host.
var versionShape = regexp.MustCompile(
	`^v?\d{1,4}\.\d{1,4}\.\d{1,4}(-[0-9A-Za-z]{1,16}(\.[0-9A-Za-z]{1,16})?)?$`)

// HusonymVersion is the version when it has the shape of a release, other otherwise.
func HusonymVersion(raw string) string {
	if versionShape.MatchString(raw) {
		return raw
	}
	return other
}

// licenseIDShape is the id the license tool generates: 8 random bytes in lowercase hex.
var licenseIDShape = regexp.MustCompile(`^[0-9a-f]{16}$`)

// LicenseId is the id of the license when it is the one the license tool generates, other for
// an id someone chose.
func LicenseId(raw string) string {
	if licenseIDShape.MatchString(raw) {
		return raw
	}
	return other
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
// in parentheses is dropped, information_schema's ARRAY, a trailing [] or a leading _ (the catalog's spelling) makes an array,
// MySQL's unsigned and zerofill are dropped, and a type that is not a built-in one (a domain, an
// enum or a type of the customer) is other.
func ColumnTypeFamily(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "array" || strings.HasSuffix(name, "[]") || strings.HasPrefix(name, "_") {
		return "array"
	}
	name = strings.Join(strings.Fields(lengthSuffix.ReplaceAllString(name, "")), " ")
	name = strings.TrimSuffix(strings.TrimSuffix(name, " zerofill"), " unsigned")
	if family, ok := columnTypeFamilies[name]; ok {
		return family
	}
	return other
}

// RowsBucket is the band a number of rows falls in; a negative number is in the lowest band.
func RowsBucket(n int64) string {
	for i, limit := range []int64{1_000, 10_000, 100_000, 1_000_000, 10_000_000, 100_000_000} {
		if n < limit {
			return RowsBuckets[i]
		}
	}
	return RowsBuckets[len(RowsBuckets)-1]
}
