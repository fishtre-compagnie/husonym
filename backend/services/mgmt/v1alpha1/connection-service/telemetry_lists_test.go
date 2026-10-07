package v1alpha1_connectionservice

import (
	"testing"

	"github.com/stretchr/testify/require"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
)

// The usage report counts connections by the names connectionTypeName gives their types; a type
// named here and missing from the report's list would be counted as other.
func Test_TheUsageReportConnectionTypes_AreTheNamesOfTheService(t *testing.T) {
	configs := []*mgmtv1alpha1.ConnectionConfig{
		{Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_MssqlConfig{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_MongoConfig{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_DynamodbConfig{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_AwsS3Config{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig{}},
		{Config: &mgmtv1alpha1.ConnectionConfig_OpenaiConfig{}},
	}
	names := make([]string, 0, len(configs)+1)
	for _, cfg := range configs {
		name := connectionTypeName(cfg)
		require.NotEqual(t, "unknown", name)
		require.Equal(t, name, telemetry.ConnectionType(name))
		names = append(names, name)
	}
	require.ElementsMatch(t, append(names, "other"), telemetry.ConnectionTypes)
	require.Equal(t, "other", telemetry.ConnectionType(connectionTypeName(&mgmtv1alpha1.ConnectionConfig{})))
}
