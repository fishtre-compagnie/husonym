package dtomaps

import (
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToConnectionDto(
	input *db_queries.HusonymApiConnection,
	canViewSensitive bool,
) (*mgmtv1alpha1.Connection, error) {
	ccDto, err := input.ConnectionConfig.ToDto(canViewSensitive)
	if err != nil {
		return nil, err
	}
	return &mgmtv1alpha1.Connection{
		Id:               husonymdb.UUIDString(input.ID),
		Name:             input.Name,
		ConnectionConfig: ccDto,
		CreatedAt:        timestamppb.New(input.CreatedAt.Time),
		UpdatedAt:        timestamppb.New(input.UpdatedAt.Time),
		CreatedByUserId:  husonymdb.UUIDString(input.CreatedByID),
		UpdatedByUserId:  husonymdb.UUIDString(input.UpdatedByID),
		AccountId:        husonymdb.UUIDString(input.AccountID),
	}, nil
}

// ConnectionTypeName maps a connection config onto the stable name a license allowlist
// uses. Kept as a plain switch rather than derived from the protobuf type name so that
// renaming a generated type cannot silently invalidate licenses already in the field.
func ConnectionTypeName(cfg *mgmtv1alpha1.ConnectionConfig) string {
	switch cfg.GetConfig().(type) {
	case *mgmtv1alpha1.ConnectionConfig_PgConfig:
		return "postgres"
	case *mgmtv1alpha1.ConnectionConfig_MysqlConfig:
		return "mysql"
	case *mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return "mssql"
	case *mgmtv1alpha1.ConnectionConfig_MongoConfig:
		return "mongodb"
	case *mgmtv1alpha1.ConnectionConfig_DynamodbConfig:
		return "dynamodb"
	case *mgmtv1alpha1.ConnectionConfig_AwsS3Config:
		return "aws-s3"
	case *mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig:
		return "gcp-cloud-storage"
	case *mgmtv1alpha1.ConnectionConfig_OpenaiConfig:
		return "openai"
	default:
		return "unknown"
	}
}
