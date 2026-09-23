package job

import (
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// TransformerDataTypeOf reads a column's SQL type, as its source reports it, into the type a
// transformer declares it takes. UNSPECIFIED when no dialect recognizes it.
//
// Every dialect is tried, because a type carries no dialect with it. The tables are the ones the
// UI uses to fill the transformer list of a column (dbDataTypeToTransformerDataType, in
// schema-constraint-handler.ts): what the product offers a person and what it chooses for them
// has to be the same set.
func TransformerDataTypeOf(dataType string) mgmtv1alpha1.TransformerDataType {
	if dt := postgresDataType(dataType); dt != mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED {
		return dt
	}
	if dt := mysqlDataType(dataType); dt != mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED {
		return dt
	}
	return mssqlDataType(dataType)
}

// baseType drops what follows a type's name: the length of a varchar, the precision of a numeric.
func baseType(dataType string) string {
	name, _, _ := strings.Cut(dataType, "(")
	return strings.ToLower(strings.TrimSpace(name))
}

func postgresDataType(dataType string) mgmtv1alpha1.TransformerDataType {
	if strings.HasSuffix(strings.TrimSpace(dataType), "[]") {
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY
	}
	switch baseType(dataType) {
	case "bigint", "integer", "smallint", "bigserial", "serial":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64
	case "text", "varchar", "char", "citext", "character varying":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING
	case "boolean":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_BOOLEAN
	case "real", "double precision", "numeric":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_FLOAT64
	case "uuid":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID
	// PostgreSQL spells a column's type out in full (format_type): a timestamp column reads
	// "timestamp without time zone", never "timestamp".
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone",
		"date",
		"time", "timetz", "time without time zone", "time with time zone":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME
	case "json", "jsonb":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY
	default:
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED
	}
}

func mysqlDataType(dataType string) mgmtv1alpha1.TransformerDataType {
	if baseType(dataType) == "tinyint" && strings.Contains(dataType, "(1)") {
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_BOOLEAN
	}
	switch baseType(dataType) {
	case "int", "integer", "smallint", "mediumint", "bigint", "tinyint":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64
	case "varchar", "text", "char", "enum", "set", "mediumtext", "longtext":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING
	case "float", "double", "decimal":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_FLOAT64
	case "datetime", "timestamp", "date", "time", "year":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME
	case "json":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY
	case "uuid":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID
	default:
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED
	}
}

func mssqlDataType(dataType string) mgmtv1alpha1.TransformerDataType {
	switch baseType(dataType) {
	case "int", "bigint", "smallint", "tinyint":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64
	case "bit":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_BOOLEAN
	case "decimal", "numeric", "money", "smallmoney", "float", "real":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_FLOAT64
	case "char", "varchar", "text", "nchar", "nvarchar", "ntext":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING
	case "date", "datetime", "datetime2", "smalldatetime", "datetimeoffset", "time":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME
	case "uniqueidentifier":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID
	case "binary", "varbinary", "image", "xml", "json", "sql_variant":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY
	default:
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED
	}
}

// acceptsDataType reports whether a transformer that declares these types takes a column of that
// type. A column whose type no dialect recognizes only takes a transformer that takes anything —
// the same rule the UI applies to the list it offers.
func acceptsDataType(
	declared []mgmtv1alpha1.TransformerDataType,
	column mgmtv1alpha1.TransformerDataType,
) bool {
	takesAny := false
	for _, dt := range declared {
		if dt == mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY {
			takesAny = true
		}
		if dt == column &&
			column != mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED {
			return true
		}
	}
	return takesAny
}
