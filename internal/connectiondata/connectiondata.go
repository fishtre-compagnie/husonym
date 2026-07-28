package connectiondata

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/mongoconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	sql_manager "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	aws_manager "github.com/fishtre-compagnie/husonym/internal/aws"
	husonym_gcp "github.com/fishtre-compagnie/husonym/internal/gcp"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
)

type SampleDataStream interface {
	Send(resp *mgmtv1alpha1.GetConnectionDataStreamResponse) error
}

type ConnectionDataService interface {
	StreamData(
		ctx context.Context,
		stream *connect.ServerStream[mgmtv1alpha1.GetConnectionDataStreamResponse],
		StreamConfig *mgmtv1alpha1.ConnectionStreamConfig,
		schema, table string,
	) error
	GetSchema(
		ctx context.Context,
		config *mgmtv1alpha1.ConnectionSchemaConfig,
	) ([]*mgmtv1alpha1.DatabaseColumn, error)
	GetInitStatements(
		ctx context.Context,
		options *mgmtv1alpha1.InitStatementOptions,
	) (*mgmtv1alpha1.GetConnectionInitStatementsResponse, error)
	GetTableConstraints(
		ctx context.Context,
	) (*mgmtv1alpha1.GetConnectionTableConstraintsResponse, error)
	GetTableSchema(
		ctx context.Context,
		schema, table string,
	) ([]*mgmtv1alpha1.DatabaseColumn, error)
	GetTableRowCount(ctx context.Context, schema, table string, whereClause *string) (int64, error)
	SampleData(
		ctx context.Context,
		stream SampleDataStream,
		schema, table string,
		numRows uint,
	) error
	GetAllTables(ctx context.Context) ([]TableIdentifier, error)
	GetAllSchemas(ctx context.Context) ([]string, error)
}

type TableIdentifier struct {
	Schema string
	Table  string
}

type ConnectionDataBuilder interface {
	NewDataConnection(
		logger *slog.Logger,
		connection *mgmtv1alpha1.Connection,
	) (ConnectionDataService, error)
}

type DefaultConnectionDataBuilder struct {
	sqlconnector        sqlconnect.SqlConnector
	sqlmanager          sql_manager.SqlManagerClient
	pgquerier           pg_queries.Querier
	mysqlquerier        mysql_queries.Querier
	awsmanager          aws_manager.HusonymAwsManagerClient
	gcpmanager          husonym_gcp.ManagerInterface
	mongoconnector      mongoconnect.Interface
	husonymtyperegistry husonymtypes.HusonymTypeRegistry
}

func NewConnectionDataBuilder(
	sqlconnector sqlconnect.SqlConnector,
	sqlmanager sql_manager.SqlManagerClient,
	pgquerier pg_queries.Querier,
	mysqlquerier mysql_queries.Querier,
	awsmanager aws_manager.HusonymAwsManagerClient,
	gcpmanager husonym_gcp.ManagerInterface,
	mongoconnector mongoconnect.Interface,
	husonymtyperegistry husonymtypes.HusonymTypeRegistry,
) ConnectionDataBuilder {
	return &DefaultConnectionDataBuilder{
		sqlconnector:        sqlconnector,
		sqlmanager:          sqlmanager,
		pgquerier:           pgquerier,
		mysqlquerier:        mysqlquerier,
		awsmanager:          awsmanager,
		gcpmanager:          gcpmanager,
		mongoconnector:      mongoconnector,
		husonymtyperegistry: husonymtyperegistry,
	}
}

func (b *DefaultConnectionDataBuilder) NewDataConnection(
	logger *slog.Logger,
	connection *mgmtv1alpha1.Connection,
) (ConnectionDataService, error) {
	switch config := connection.ConnectionConfig.Config.(type) {
	case *mgmtv1alpha1.ConnectionConfig_MysqlConfig,
		*mgmtv1alpha1.ConnectionConfig_PgConfig,
		*mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return NewSQLConnectionDataService(logger, b.sqlconnector, b.sqlmanager, connection), nil
	case *mgmtv1alpha1.ConnectionConfig_AwsS3Config:
		return NewAwsS3ConnectionDataService(logger, b.awsmanager, b.husonymtyperegistry, connection), nil
	case *mgmtv1alpha1.ConnectionConfig_GcpCloudstorageConfig:
		return NewGcpConnectionDataService(logger, b.gcpmanager, connection), nil
	case *mgmtv1alpha1.ConnectionConfig_DynamodbConfig:
		return NewAwsDynamodbConnectionDataService(logger, b.awsmanager, connection), nil
	case *mgmtv1alpha1.ConnectionConfig_MongoConfig:
		return NewMongoDbConnectionDataService(logger, connection, b.mongoconnector), nil
	default:
		return nil, fmt.Errorf("connection config not supported for connection data service: %T", config)
	}
}

type SchemaOpts struct {
	JobId    *string
	JobRunId *string
}
