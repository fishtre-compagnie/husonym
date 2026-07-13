package recommendations

import (
	"reflect"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// FilterForValidity ensures that a recommended transformer configuration is
// actually usable for the target column, given the server's system
// transformer catalog and whatever schema constraints are known about the
// column. A suggestion that would fail ValidateJobMappings is worse than no
// suggestion at all, so any incompatibility here causes a safe fallback
// (GenerateDefault for generated columns, Passthrough otherwise) rather than
// surfacing the incompatible config.
//
// The catalog is injected by the caller (rather than duplicated here) so
// that this package always reflects the single source of truth for which
// data types a given system transformer supports.
func FilterForValidity(
	catalog []*mgmtv1alpha1.SystemTransformer,
	recommended *mgmtv1alpha1.TransformerConfig,
	dataType mgmtv1alpha1.TransformerDataType,
	isGeneratedColumn bool,
	isIdentityColumn bool,
) *mgmtv1alpha1.TransformerConfig {
	// Generated columns never accept a value supplied by the sync/anonymization
	// pipeline; the only safe recommendation is to defer to the database default.
	if isGeneratedColumn {
		return generateDefaultConfig()
	}
	// Identity columns (auto-increment / GENERATED AS IDENTITY) should not be
	// rewritten; passing the existing value through is the only safe choice.
	if isIdentityColumn {
		return passthroughConfig()
	}

	if recommended == nil || recommended.GetConfig() == nil {
		return passthroughConfig()
	}

	systemTransformer, ok := findSystemTransformerForConfig(catalog, recommended)
	if !ok {
		// The recommended config doesn't correspond to any transformer in the
		// catalog (e.g. it references an EE transformer while running without a
		// valid license): fall back rather than surface an unusable config.
		return passthroughConfig()
	}

	if !supportsDataType(systemTransformer.GetDataTypes(), dataType) {
		return passthroughConfig()
	}

	return recommended
}

// findSystemTransformerForConfig locates the catalog entry whose default
// Config has the same oneof case as the recommended config, mirroring
// TransformerHandler.getSystemTransformerByConfigCase on the frontend
// (frontend/apps/web/components/jobs/SchemaTable/transformer-handler.ts).
func findSystemTransformerForConfig(
	catalog []*mgmtv1alpha1.SystemTransformer,
	config *mgmtv1alpha1.TransformerConfig,
) (*mgmtv1alpha1.SystemTransformer, bool) {
	wantType := reflect.TypeOf(config.GetConfig())
	if wantType == nil {
		return nil, false
	}
	for _, st := range catalog {
		if st.GetConfig().GetConfig() == nil {
			continue
		}
		if reflect.TypeOf(st.GetConfig().GetConfig()) == wantType {
			return st, true
		}
	}
	return nil, false
}

func supportsDataType(
	supported []mgmtv1alpha1.TransformerDataType,
	dataType mgmtv1alpha1.TransformerDataType,
) bool {
	for _, dt := range supported {
		if dt == dataType || dt == mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY {
			return true
		}
	}
	return false
}

// SqlDataTypeToTransformerDataType maps a raw SQL column data type (as
// reported by the connection's schema introspection, e.g. "character
// varying", "int4", "varchar(255)") to the coarse TransformerDataType used by
// the system transformer catalog. It mirrors
// frontend/apps/web/components/jobs/SchemaTable/schema-constraint-handler.ts
// (postgresTypeToTransformerDataType / mysqlTypeToTransformerDataType) so
// that server-side recommendations agree with the data types the UI itself
// uses to filter transformers.
func SqlDataTypeToTransformerDataType(sqlDataType string) mgmtv1alpha1.TransformerDataType {
	baseType := strings.ToLower(strings.TrimSpace(sqlDataType))
	isArray := strings.HasSuffix(baseType, "[]")
	baseType = strings.TrimSuffix(baseType, "[]")
	if idx := strings.Index(baseType, "("); idx >= 0 {
		baseType = baseType[:idx]
	}
	baseType = strings.TrimSpace(baseType)

	if isArray {
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY
	}

	switch baseType {
	case "bigint", "integer", "int", "int4", "int8", "smallint", "int2",
		"bigserial", "serial", "mediumint", "tinyint":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64
	case "text", "varchar", "char", "citext", "character varying", "character",
		"enum", "set", "mediumtext", "longtext", "tinytext":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING
	case "boolean", "bool":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_BOOLEAN
	case "real", "double precision", "double", "numeric", "decimal", "float", "float4", "float8":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_FLOAT64
	case "uuid":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID
	case "timestamp", "timestamptz", "date", "time", "timetz", "datetime", "year":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME
	case "json", "jsonb":
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY
	default:
		return mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED
	}
}
