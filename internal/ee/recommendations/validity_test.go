package recommendations

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func testCatalog() []*mgmtv1alpha1.SystemTransformer {
	return []*mgmtv1alpha1.SystemTransformer{
		{
			Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_EMAIL,
			DataTypes: []mgmtv1alpha1.TransformerDataType{
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
			},
			Config: transformEmailConfig(),
		},
		{
			Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UUID,
			DataTypes: []mgmtv1alpha1.TransformerDataType{
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID,
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
			},
			Config: generateUuidConfig(),
		},
		{
			Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_STRING,
			DataTypes: []mgmtv1alpha1.TransformerDataType{
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
			},
			Config: transformStringConfig(),
		},
		{
			Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_PASSTHROUGH,
			DataTypes: []mgmtv1alpha1.TransformerDataType{
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY,
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
			},
			Config: passthroughConfig(),
		},
		{
			Source: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_DEFAULT,
			DataTypes: []mgmtv1alpha1.TransformerDataType{
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY,
				mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_NULL,
			},
			Config: generateDefaultConfig(),
		},
	}
}

func Test_FilterForValidity(t *testing.T) {
	t.Parallel()
	catalog := testCatalog()

	t.Run("generated column always forces GenerateDefault", func(t *testing.T) {
		t.Parallel()
		got := FilterForValidity(catalog, transformEmailConfig(), mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING, true, false)
		require.IsType(t, &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{}, got.GetConfig())
	})

	t.Run("identity column always forces Passthrough", func(t *testing.T) {
		t.Parallel()
		got := FilterForValidity(catalog, transformEmailConfig(), mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING, false, true)
		require.IsType(t, &mgmtv1alpha1.TransformerConfig_PassthroughConfig{}, got.GetConfig())
	})

	t.Run("compatible data type passes through unchanged", func(t *testing.T) {
		t.Parallel()
		rec := transformEmailConfig()
		got := FilterForValidity(catalog, rec, mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING, false, false)
		require.Same(t, rec, got)
	})

	t.Run("incompatible data type falls back to passthrough", func(t *testing.T) {
		t.Parallel()
		got := FilterForValidity(catalog, transformEmailConfig(), mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64, false, false)
		require.IsType(t, &mgmtv1alpha1.TransformerConfig_PassthroughConfig{}, got.GetConfig())
	})

	t.Run("config not present in catalog falls back to passthrough", func(t *testing.T) {
		t.Parallel()
		got := FilterForValidity(catalog, generateFirstNameConfig(), mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING, false, false)
		require.IsType(t, &mgmtv1alpha1.TransformerConfig_PassthroughConfig{}, got.GetConfig())
	})

	t.Run("nil config falls back to passthrough", func(t *testing.T) {
		t.Parallel()
		got := FilterForValidity(catalog, nil, mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING, false, false)
		require.IsType(t, &mgmtv1alpha1.TransformerConfig_PassthroughConfig{}, got.GetConfig())
	})

	t.Run("uuid generator is valid for string columns", func(t *testing.T) {
		t.Parallel()
		rec := generateUuidConfig()
		got := FilterForValidity(catalog, rec, mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING, false, false)
		require.Same(t, rec, got)
	})
}

func Test_SqlDataTypeToTransformerDataType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sqlType string
		want    mgmtv1alpha1.TransformerDataType
	}{
		{"character varying", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING},
		{"varchar(255)", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING},
		{"text", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING},
		{"bigint", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64},
		{"integer", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64},
		{"int4", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64},
		{"boolean", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_BOOLEAN},
		{"numeric", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_FLOAT64},
		{"decimal(10,2)", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_FLOAT64},
		{"uuid", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UUID},
		{"timestamp", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME},
		{"date", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME},
		{"jsonb", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY},
		{"text[]", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY},
		{"some_unknown_type", mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.sqlType, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, SqlDataTypeToTransformerDataType(tt.sqlType))
		})
	}
}
