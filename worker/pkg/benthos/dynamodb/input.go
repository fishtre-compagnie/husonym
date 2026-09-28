package husonym_benthos_dynamodb

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	database_record_mapper "github.com/fishtre-compagnie/husonym/internal/database-record-mapper/builder"
	dynamodbmapper "github.com/fishtre-compagnie/husonym/internal/database-record-mapper/dynamodb"
	husonym_benthos_metadata "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/metadata"
	"github.com/redpanda-data/benthos/v4/public/service"
)

func dynamoInputConfigSpec() *service.ConfigSpec {
	spec := service.NewConfigSpec().
		Categories("Services").
		Summary("Scans an entire dynamodb table and creates a message for each document received").
		Field(service.NewStringField("table").
			Description("The table to retrieve items from.")).
		Field(service.NewStringField("where").
			Description("Optional PartiQL where clause that gets tacked on to the end of the select query").
			Optional()).
		Field(service.NewBoolField("consistent_read").
			Description("Optional field that enforces strong read consistency. Default is eventually consistent reads").
			Default(false)).
		Field(connectionIdField())

	return spec
}

func RegisterDynamoDbInput(
	env *service.Environment,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
) error {
	return env.RegisterBatchInput(
		"aws_dynamodb", dynamoInputConfigSpec(),
		func(conf *service.ParsedConfig, mgr *service.Resources) (service.BatchInput, error) {
			return newDynamoDbBatchInput(conf, getConnection, mgr.Logger())
		},
	)
}

type dynamoDBAPIV2 interface {
	DescribeTable(
		ctx context.Context,
		params *dynamodb.DescribeTableInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.DescribeTableOutput, error)
	ExecuteStatement(
		ctx context.Context,
		params *dynamodb.ExecuteStatementInput,
		optFns ...func(*dynamodb.Options),
	) (*dynamodb.ExecuteStatementOutput, error)
}

func newDynamoDbBatchInput(
	conf *service.ParsedConfig,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
	logger *service.Logger,
) (service.BatchInput, error) {
	table, err := conf.FieldString("table")
	if err != nil {
		return nil, err
	}

	var whereClause *string
	if conf.Contains("where") {
		where, err := conf.FieldString("where")
		if err != nil {
			return nil, err
		}
		whereClause = &where
	}

	consistentRead, err := conf.FieldBool("consistent_read")
	if err != nil {
		return nil, err
	}

	connection, err := resolveDynamoDbConnection(conf, getConnection)
	if err != nil {
		return nil, err
	}

	return &dynamodbInput{
		connection: connection,
		logger:     logger,

		recordMapper: dynamodbmapper.NewDynamoBuilder(),

		table:          table,
		where:          whereClause,
		consistentRead: consistentRead,
	}, nil
}

type dynamodbInput struct {
	client     dynamoDBAPIV2 // lazy
	connection dynamoDbConnection
	logger     *service.Logger
	readMu     sync.Mutex

	table string
	where *string

	recordMapper database_record_mapper.DatabaseRecordMapper[any]

	consistentRead bool

	nextToken *string
	done      bool
}

var _ service.BatchInput = &dynamodbInput{}

func (d *dynamodbInput) Connect(ctx context.Context) error {
	d.readMu.Lock()
	defer d.readMu.Unlock()

	if d.client != nil {
		return nil
	}

	client := d.connection.client()

	tableOutput, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: &d.table,
	})
	if err != nil {
		return fmt.Errorf("unable to describe dynamodb table when connecting to read: %w", err)
	}
	if !isTableActive(tableOutput) {
		return fmt.Errorf("dynamodb table %q must be active to read", d.table)
	}

	d.client = client
	return nil
}

func isTableActive(output *dynamodb.DescribeTableOutput) bool {
	return output != nil && output.Table != nil &&
		output.Table.TableStatus == types.TableStatusActive
}

func (d *dynamodbInput) ReadBatch(
	ctx context.Context,
) (service.MessageBatch, service.AckFunc, error) {
	d.readMu.Lock()
	defer d.readMu.Unlock()
	if d.client == nil {
		return nil, nil, service.ErrNotConnected
	}
	if d.done {
		return nil, nil, service.ErrEndOfInput
	}

	// todo: allow specifying batch size
	result, err := d.client.ExecuteStatement(ctx, &dynamodb.ExecuteStatementInput{
		Statement:      aws.String(buildExecStatement(d.table, d.where)),
		NextToken:      d.nextToken,
		ConsistentRead: aws.Bool(d.consistentRead),
	})
	if err != nil {
		return nil, nil, err
	}
	batch := service.MessageBatch{}
	for _, item := range result.Items {
		if item == nil {
			continue
		}

		resMap, keyTypeMap, err := d.recordMapper.MapRecordWithKeyType(item)
		if err != nil {
			return nil, nil, err
		}

		msg := service.NewMessage(nil)
		msg.MetaSetMut(husonym_benthos_metadata.MetaTypeMapStr, keyTypeMap)
		msg.SetStructuredMut(resMap)
		batch = append(batch, msg)
	}
	d.nextToken = result.NextToken
	d.done = result.NextToken == nil

	return batch, emptyAck, nil
}

func buildExecStatement(table string, where *string) string {
	stmt := fmt.Sprintf("SELECT * FROM %q", table)
	if where != nil && *where != "" {
		return fmt.Sprintf("%s WHERE %s", stmt, *where)
	}
	return stmt
}

func emptyAck(ctx context.Context, err error) error {
	return nil
}

func (d *dynamodbInput) Close(ctx context.Context) error {
	d.readMu.Lock()
	defer d.readMu.Unlock()
	if d.client == nil {
		return nil
	}
	d.client = nil
	return nil
}
