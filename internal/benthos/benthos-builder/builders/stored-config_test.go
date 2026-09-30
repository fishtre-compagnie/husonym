package benthosbuilder_builders

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	bb_internal "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/internal"
	_ "github.com/fishtre-compagnie/husonym/internal/benthos/imports" // the components the worker imports
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	benthos_environment "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/environment"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const sentinelSecret = "sentinel-secret"

// The config of a run is stored where job:view reads it: it names its connections and never
// carries what they hold. Every credential of the connections is a sentinel the config must not
// contain, and what the builders write is linted by the worker's own Benthos environment.
func Test_StoredConfigHoldsNoConnectionSecret(t *testing.T) {
	credentials := &mgmtv1alpha1.AwsS3Credentials{
		Profile:         ptr(sentinelSecret + "-profile"),
		AccessKeyId:     ptr(sentinelSecret + "-access-key-id"),
		SecretAccessKey: ptr(sentinelSecret + "-secret-access-key"),
		SessionToken:    ptr(sentinelSecret + "-session-token"),
		RoleArn:         ptr(sentinelSecret + "-role-arn"),
		RoleExternalId:  ptr(sentinelSecret + "-role-external-id"),
	}
	s3Connection := &mgmtv1alpha1.Connection{
		Id: "s3-connection",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_AwsS3Config{
				AwsS3Config: &mgmtv1alpha1.AwsS3ConnectionConfig{
					Bucket:      sentinelSecret + "-bucket",
					Region:      ptr(sentinelSecret + "-region"),
					Endpoint:    ptr(sentinelSecret + "-endpoint"),
					Credentials: credentials,
				},
			},
		},
	}
	dynamoConnection := &mgmtv1alpha1.Connection{
		Id: "dynamodb-connection",
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_DynamodbConfig{
				DynamodbConfig: &mgmtv1alpha1.DynamoDBConnectionConfig{
					Region:      ptr(sentinelSecret + "-region"),
					Endpoint:    ptr(sentinelSecret + "-endpoint"),
					Credentials: credentials,
				},
			},
		},
	}
	sourceConfig := &bb_internal.BenthosSourceConfig{
		TableSchema: "public",
		TableName:   "users",
		RunType:     runconfigs.RunTypeInsert,
	}

	aiSources := buildBenthosAiGenerateSourceConfigResponses("openai-connection", []*aiGenerateMappings{{
		Schema:  "public",
		Table:   "users",
		Columns: []*aiGenerateColumn{{Column: "name", DataType: "text"}},
		Count:   1,
	}}, "gpt", nil, nil)
	require.Len(t, aiSources, 1)

	s3Destination, err := NewAwsS3SyncBuilder().BuildDestinationConfig(context.Background(), &bb_internal.DestinationParams{
		SourceConfig: sourceConfig,
		JobRunId:     "run",
		DestinationOpts: &mgmtv1alpha1.JobDestinationOptions{
			Config: &mgmtv1alpha1.JobDestinationOptions_AwsS3Options{
				AwsS3Options: &mgmtv1alpha1.AwsS3DestinationConnectionOptions{},
			},
		},
		DestConnection: s3Connection,
	})
	require.NoError(t, err)

	dynamoDestination, err := NewDynamoDbSyncBuilder(nil, nil).BuildDestinationConfig(context.Background(), &bb_internal.DestinationParams{
		SourceConfig: sourceConfig,
		DestinationOpts: &mgmtv1alpha1.JobDestinationOptions{
			Config: &mgmtv1alpha1.JobDestinationOptions_DynamodbOptions{
				DynamodbOptions: &mgmtv1alpha1.DynamoDBDestinationConnectionOptions{
					TableMappings: []*mgmtv1alpha1.DynamoDBDestinationTableMapping{
						{SourceTable: "users", DestinationTable: "users"},
					},
				},
			},
		},
		DestConnection: dynamoConnection,
	})
	require.NoError(t, err)

	// The sources receive only the id of their connection; what they write is linted below, and
	// the DynamoDB workflow test checks the config a real run stores.
	t.Run("openai generate source declares its connection", func(t *testing.T) {
		require.Equal(t, "openai-connection", aiSources[0].BenthosDsns[0].ConnectionId,
			"the worker only resolves the connections a config declares")
	})
	t.Run("aws s3 destination", func(t *testing.T) {
		requireNamesWithoutHolding(t, s3Destination, s3Connection.GetId())
	})
	t.Run("dynamodb destination", func(t *testing.T) {
		requireNamesWithoutHolding(t, dynamoDestination, dynamoConnection.GetId())
	})

	t.Run("the plugins accept what the builders write", func(t *testing.T) {
		env, err := benthos_environment.NewEnvironment(
			testutil.GetTestLogger(t),
			benthos_environment.WithConnections(func(string) (connectionmanager.ConnectionInput, error) {
				return nil, errors.New("not resolved: the configs are only linted")
			}, cloudidentity.Policy{}),
			benthos_environment.WithStopChannel(make(chan error, 1)),
			benthos_environment.WithBlobEnv(bloblang.NewEnvironment()),
		)
		require.NoError(t, err)

		// As the generator does: the destinations' outputs go into the broker of a source.
		outputs := []husonym_benthos.Outputs{}
		outputs = append(outputs, s3Destination.Outputs...)
		outputs = append(outputs, dynamoDestination.Outputs...)
		aiStream := *aiSources[0].Config
		output := *aiStream.Output
		broker := *output.Broker
		broker.Outputs = outputs
		output.Broker = &broker
		aiStream.Output = &output

		dynamoStream := aiStream
		dynamoStream.Input = &husonym_benthos.InputConfig{Inputs: husonym_benthos.Inputs{
			AwsDynamoDB: dynamoDbInput("users", ptr("id = '1'"), true, dynamoConnection.GetId()),
		}}

		for _, stream := range []husonym_benthos.BenthosConfig{aiStream, dynamoStream} {
			stored, err := yaml.Marshal(stream)
			require.NoError(t, err)
			require.NoError(t, env.NewStreamBuilder().SetYAML(string(stored)), "%s", stored)
		}
	})
}

// requireNamesWithoutHolding checks that a destination config names its connection, so that the
// worker resolves it, and holds nothing of it.
func requireNamesWithoutHolding(t *testing.T, config *bb_internal.BenthosDestinationConfig, connectionId string) {
	t.Helper()
	stored := requireHoldsNoSecret(t, husonym_benthos.OutputConfig{
		Outputs: husonym_benthos.Outputs{Broker: &husonym_benthos.OutputBrokerConfig{Outputs: config.Outputs}},
	})
	require.Contains(t, stored, "connection_id: "+connectionId)

	named := []string{}
	for _, dsn := range config.BenthosDsns {
		named = append(named, dsn.ConnectionId)
	}
	require.Contains(t, named, connectionId, "the worker only resolves the connections a config declares")
}

// requireHoldsNoSecret checks the stored form of a config, and returns it.
func requireHoldsNoSecret(t *testing.T, config any) string {
	t.Helper()
	stored, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(stored), sentinelSecret), "the stored config holds a secret:\n%s", stored)
	return string(stored)
}

func ptr[T any](v T) *T { return &v }
