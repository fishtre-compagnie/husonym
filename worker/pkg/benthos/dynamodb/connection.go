package husonym_benthos_dynamodb

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmanager "github.com/fishtre-compagnie/husonym/internal/aws"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/redpanda-data/benthos/v4/public/service"
)

const fieldConnectionId = "connection_id"

func connectionIdField() *service.ConfigField {
	return service.NewStringField(fieldConnectionId).
		Description("The Husonym connection whose region, endpoint and credentials are used.")
}

// dynamoDbAwsConfig resolves the AWS config of the DynamoDB connection a config names. The
// region, endpoint and credentials live in the connection only, so that the stored config of a
// run holds no secret. It is resolved when the component is built, as a config that cannot be
// resolved must fail the stream rather than have Connect retried forever.
func dynamoDbAwsConfig(
	conf *service.ParsedConfig,
	getConnection func(connectionId string) (connectionmanager.ConnectionInput, error),
) (*aws.Config, error) {
	connectionId, err := conf.FieldString(fieldConnectionId)
	if err != nil {
		return nil, err
	}
	connection, err := getConnection(connectionId)
	if err != nil {
		return nil, err
	}
	dynamoConfig := connection.GetConnectionConfig().GetDynamodbConfig()
	if dynamoConfig == nil {
		return nil, fmt.Errorf("connection %q is not a DynamoDB connection", connectionId)
	}
	awsConfig, err := awsmanager.DynamoDbAwsConfig(context.Background(), dynamoConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve the aws config of connection %q: %w", connectionId, err)
	}
	return awsConfig, nil
}
