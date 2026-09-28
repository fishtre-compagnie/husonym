package job

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// The types a column is read with, as each database spells them. The UI keeps the same table
// (dbDataTypeToTransformerDataType, schema-constraint-handler.test.ts checks these same cases):
// what the job builder offers and what the API accepts are the same set.
func Test_TransformerDataTypeOf(t *testing.T) {
	for dataType, want := range map[string]mgmtv1alpha1.TransformerDataType{
		"character varying(40)":       mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
		"character(2)":                mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
		"bpchar":                      mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
		"tinytext":                    mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_STRING,
		"timestamp without time zone": mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_TIME,
		"integer[]":                   mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_ANY,
		"tinyint":                     mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_INT64,
		"inet":                        mgmtv1alpha1.TransformerDataType_TRANSFORMER_DATA_TYPE_UNSPECIFIED,
	} {
		require.Equal(t, want, TransformerDataTypeOf(dataType), dataType)
	}
}
