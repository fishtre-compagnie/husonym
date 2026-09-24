// Package connection holds what the CLI's commands share about a connection.
package connection

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Category names the kind of system a connection points at.
func Category(cc *mgmtv1alpha1.ConnectionConfig) string {
	if cc == nil {
		return "Unknown"
	}
	switch cc.GetConfig().(type) {
	case *mgmtv1alpha1.ConnectionConfig_PgConfig:
		return "PostgreSQL"
	case *mgmtv1alpha1.ConnectionConfig_MysqlConfig:
		return "MySQL"
	case *mgmtv1alpha1.ConnectionConfig_AwsS3Config:
		return "AWS S3"
	case *mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig:
		return "GCP Cloud Storage"
	case *mgmtv1alpha1.ConnectionConfig_MongoConfig:
		return "MongoDB"
	case *mgmtv1alpha1.ConnectionConfig_OpenaiConfig:
		return "OpenAI"
	case *mgmtv1alpha1.ConnectionConfig_DynamodbConfig:
		return "DynamoDB"
	case *mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return "MSSQL"
	default:
		return "Unknown"
	}
}
