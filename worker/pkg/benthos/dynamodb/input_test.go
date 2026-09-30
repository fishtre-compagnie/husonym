package husonym_benthos_dynamodb

import (
	"context"
	"fmt"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	dynamodbmapper "github.com/fishtre-compagnie/husonym/internal/database-record-mapper/dynamodb"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_isTableActive(t *testing.T) {
	type testcase struct {
		input    *dynamodb.DescribeTableOutput
		expected bool
	}

	testcases := []testcase{
		{nil, false},
		{&dynamodb.DescribeTableOutput{}, false},
		{&dynamodb.DescribeTableOutput{Table: nil}, false},
		{&dynamodb.DescribeTableOutput{Table: &types.TableDescription{}}, false},
		{
			&dynamodb.DescribeTableOutput{
				Table: &types.TableDescription{TableStatus: types.TableStatusArchived},
			},
			false,
		},
		{
			&dynamodb.DescribeTableOutput{
				Table: &types.TableDescription{TableStatus: types.TableStatusActive},
			},
			true,
		},
	}

	for _, tc := range testcases {
		actual := isTableActive(tc.input)
		require.Equal(t, tc.expected, actual)
	}
}

func Test_dynamoDbBatchInput_Connect_Client(t *testing.T) {
	mockClient := NewMockdynamoDBAPIV2(t)
	input := &dynamodbInput{client: mockClient}
	err := input.Connect(context.Background())
	require.NoError(t, err)
}

func Test_dynamoDbBatchInput_ReadBatch_NotConnected(t *testing.T) {
	input := &dynamodbInput{}
	_, _, err := input.ReadBatch(context.Background())
	require.Error(t, err)
	require.Equal(t, service.ErrNotConnected, err)
}

func Test_dynamoDbBatchInput_ReadBatch_EndOfInput(t *testing.T) {
	mockClient := NewMockdynamoDBAPIV2(t)
	input := &dynamodbInput{client: mockClient, done: true}
	_, _, err := input.ReadBatch(context.Background())
	require.Error(t, err)
	require.Equal(t, service.ErrEndOfInput, err)
}

func Test_dynamoDbBatchInput_ReadBatch_SinglePage(t *testing.T) {
	mockClient := NewMockdynamoDBAPIV2(t)
	input := &dynamodbInput{
		client:       mockClient,
		table:        "foo",
		recordMapper: dynamodbmapper.NewDynamoBuilder(),
	}

	mockClient.On("ExecuteStatement", mock.Anything, mock.Anything).
		Return(&dynamodb.ExecuteStatementOutput{
			Items: []map[string]types.AttributeValue{
				{"f": &types.AttributeValueMemberBOOL{Value: false}},
				{"g": &types.AttributeValueMemberBOOL{Value: true}},
			},
		}, nil)

	batch, _, err := input.ReadBatch(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2)
	require.Nil(t, input.nextToken)
	require.True(t, input.done)
}

func Test_dynamoDbBatchInput_ReadBatch_MultiPage(t *testing.T) {
	mockClient := NewMockdynamoDBAPIV2(t)
	input := &dynamodbInput{
		client:       mockClient,
		table:        "foo",
		recordMapper: dynamodbmapper.NewDynamoBuilder(),
	}

	mockClient.On("ExecuteStatement", mock.Anything, mock.Anything).
		Return(&dynamodb.ExecuteStatementOutput{
			Items: []map[string]types.AttributeValue{
				{"f": &types.AttributeValueMemberBOOL{Value: false}},
				{"g": &types.AttributeValueMemberBOOL{Value: true}},
			},
			NextToken: aws.String("foo"),
		}, nil)

	batch, _, err := input.ReadBatch(context.Background())
	require.NoError(t, err)
	require.Len(t, batch, 2)
	require.NotNil(t, input.nextToken)
	require.False(t, input.done)
}

func Test_dynamoDbBatchInput_Close(t *testing.T) {
	mockClient := NewMockdynamoDBAPIV2(t)

	input := &dynamodbInput{}
	err := input.Close(context.Background())
	require.NoError(t, err)

	input.client = mockClient
	err = input.Close(context.Background())
	require.NoError(t, err)
	require.Nil(t, input.client)
}

func Test_buildExecStatement(t *testing.T) {
	tests := []struct {
		name     string
		table    string
		where    *string
		expected string
	}{
		{
			name:     "No Where Clause",
			table:    "users",
			where:    nil,
			expected: `SELECT * FROM "users"`,
		},
		{
			name:     "Empty Where Clause",
			table:    "users",
			where:    func() *string { s := ""; return &s }(),
			expected: `SELECT * FROM "users"`,
		},
		{
			name:     "Valid Where Clause",
			table:    "users",
			where:    func() *string { s := "id = 1"; return &s }(),
			expected: `SELECT * FROM "users" WHERE id = 1`,
		},
		{
			name:     "Another Table with Where Clause",
			table:    "orders",
			where:    func() *string { s := "status = 'shipped'"; return &s }(),
			expected: `SELECT * FROM "orders" WHERE status = 'shipped'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildExecStatement(tt.table, tt.where)
			require.True(t, result == tt.expected, "expected %v, got %v", tt.expected, result)
		})
	}
}

func Test_RegisterDynamoDBInput(t *testing.T) {
	err := RegisterDynamoDbInput(service.NewEmptyEnvironment(), connectionsOf(), cloudidentity.Policy{})
	require.NoError(t, err)
}

func Test_InputBasic_Config(t *testing.T) {
	conf, err := dynamoInputConfigSpec().ParseYAML(`
table: test-table
connection_id: dynamo
`, service.NewEmptyEnvironment())
	require.NoError(t, err)
	require.NotNil(t, conf)
}

func Test_InputBasic_Config_Opts(t *testing.T) {
	conf, err := dynamoInputConfigSpec().ParseYAML(`
table: test-table
where: foo = '123'
consistent_read: true
connection_id: dynamo
`, service.NewEmptyEnvironment())
	require.NoError(t, err)
	require.NotNil(t, conf)
}

func Test_Input_ReadsTheConnection(t *testing.T) {
	dynamoConfig := &mgmtv1alpha1.DynamoDBConnectionConfig{
		Region: aws.String("us-west-2"),
		Credentials: &mgmtv1alpha1.AwsS3Credentials{
			AccessKeyId: aws.String("the-key"), SecretAccessKey: aws.String("the-secret"),
		},
	}
	getConnection := connectionsOf(dynamoConnection("dynamo", dynamoConfig))

	input, err := newDynamoDbBatchInput(parseInputConfig(t, "dynamo"), getConnection, cloudidentity.Policy{}, nil)
	require.NoError(t, err)
	require.Equal(t, "us-west-2", input.(*dynamodbInput).connection.awsConfig.Region)

	_, err = newDynamoDbBatchInput(parseInputConfig(t, "unknown"), getConnection, cloudidentity.Policy{}, nil)
	require.ErrorContains(t, err, "unknown")

	getConnection = connectionsOf(&mgmtv1alpha1.Connection{
		Id: "pg",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
		},
	})
	_, err = newDynamoDbBatchInput(parseInputConfig(t, "pg"), getConnection, cloudidentity.Policy{}, nil)
	require.ErrorContains(t, err, "not a DynamoDB connection")
}

func parseInputConfig(t *testing.T, connectionId string) *service.ParsedConfig {
	t.Helper()
	conf, err := dynamoInputConfigSpec().ParseYAML(`
table: test-table
connection_id: `+connectionId+`
`, service.NewEmptyEnvironment())
	require.NoError(t, err)
	return conf
}

func dynamoConnection(id string, config *mgmtv1alpha1.DynamoDBConnectionConfig) *mgmtv1alpha1.Connection {
	return &mgmtv1alpha1.Connection{
		Id: id,
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_DynamodbConfig{DynamodbConfig: config},
		},
	}
}

// connectionsOf resolves connections the way the worker does: by id, among those of the run.
func connectionsOf(
	connections ...*mgmtv1alpha1.Connection,
) func(connectionId string) (connectionmanager.ConnectionInput, error) {
	return func(connectionId string) (connectionmanager.ConnectionInput, error) {
		for _, connection := range connections {
			if connection.GetId() == connectionId {
				return connection, nil
			}
		}
		return nil, fmt.Errorf("unable to find connection by id: %q", connectionId)
	}
}

// A connection whose AWS config cannot be resolved fails the stream when it is built, not in a
// Connect that would be retried forever.
func Test_UnresolvableAwsConfigFailsTheBuild(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/credentials")
	t.Setenv("AWS_PROFILE", "")
	profile := "husonym-absent-profile"
	getConnection := connectionsOf(dynamoConnection("dynamo", &mgmtv1alpha1.DynamoDBConnectionConfig{
		Credentials: &mgmtv1alpha1.AwsS3Credentials{Profile: &profile},
	}))

	// Where the deployment allows the server's identity, the profile is looked up, and missed.
	allowed := cloudidentity.Policy{AllowServerIdentity: true}
	_, err := newDynamoDbBatchInput(parseInputConfig(t, "dynamo"), getConnection, allowed, nil)
	require.ErrorContains(t, err, profile)

	outputConf, err := dynamoOutputConfigSpec().ParseYAML(`
table: FooTable
connection_id: dynamo
string_columns:
  id: ${!json("id")}
`, nil)
	require.NoError(t, err)
	_, err = ddboConfigFromParsed(outputConf, getConnection, allowed)
	require.ErrorContains(t, err, profile)

	// Elsewhere, a profile is the server's: the connection is refused before anything is read.
	_, err = newDynamoDbBatchInput(parseInputConfig(t, "dynamo"), getConnection, cloudidentity.Policy{}, nil)
	require.ErrorContains(t, err, "credentials of its own")
	_, err = ddboConfigFromParsed(outputConf, getConnection, cloudidentity.Policy{})
	require.ErrorContains(t, err, "credentials of its own")
}
