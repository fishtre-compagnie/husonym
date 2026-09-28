package benthosbuilder_builders

import (
	"context"
	"errors"
	"strings"
	"testing"

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

// The config of a run is stored where job:view reads it: it names its connections and never
// carries what they hold. Every credential of the connection is a sentinel the config must
// not contain.
func Test_StoredConfigHoldsNoConnectionSecret(t *testing.T) {
	const secret = "sentinel-secret"
	credentials := &mgmtv1alpha1.AwsS3Credentials{
		Profile:         ptr(secret + "-profile"),
		AccessKeyId:     ptr(secret + "-access-key-id"),
		SecretAccessKey: ptr(secret + "-secret-access-key"),
		SessionToken:    ptr(secret + "-session-token"),
		RoleArn:         ptr(secret + "-role-arn"),
		RoleExternalId:  ptr(secret + "-role-external-id"),
	}
	sourceConfig := &bb_internal.BenthosSourceConfig{
		TableSchema: "public",
		TableName:   "users",
		RunType:     runconfigs.RunTypeInsert,
	}

	sources := []*husonym_benthos.BenthosConfig{}
	outputs := []husonym_benthos.Outputs{}

	t.Run("openai generate source", func(t *testing.T) {
		configs := buildBenthosAiGenerateSourceConfigResponses("openai-connection", []*aiGenerateMappings{{
			Schema:  "public",
			Table:   "users",
			Columns: []*aiGenerateColumn{{Column: "name", DataType: "text"}},
			Count:   1,
		}}, "gpt", nil, nil)
		require.Len(t, configs, 1)
		require.Equal(t, "openai-connection", configs[0].BenthosDsns[0].ConnectionId,
			"the worker only resolves the connections a config declares")
		sources = append(sources, configs[0].Config)
	})

	t.Run("aws s3 destination", func(t *testing.T) {
		connection := &mgmtv1alpha1.Connection{
			Id: "s3-connection",
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_AwsS3Config{
					AwsS3Config: &mgmtv1alpha1.AwsS3ConnectionConfig{
						Bucket:      secret + "-bucket",
						Region:      ptr(secret + "-region"),
						Endpoint:    ptr(secret + "-endpoint"),
						Credentials: credentials,
					},
				},
			},
		}
		config, err := NewAwsS3SyncBuilder().BuildDestinationConfig(context.Background(), &bb_internal.DestinationParams{
			SourceConfig: sourceConfig,
			JobRunId:     "run",
			DestinationOpts: &mgmtv1alpha1.JobDestinationOptions{
				Config: &mgmtv1alpha1.JobDestinationOptions_AwsS3Options{
					AwsS3Options: &mgmtv1alpha1.AwsS3DestinationConnectionOptions{},
				},
			},
			DestConnection: connection,
		})
		require.NoError(t, err)
		requireNamesWithoutHolding(t, config, connection.GetId(), secret)
		outputs = append(outputs, config.Outputs...)
	})

	t.Run("dynamodb destination", func(t *testing.T) {
		connection := &mgmtv1alpha1.Connection{
			Id: "dynamodb-connection",
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_DynamodbConfig{
					DynamodbConfig: &mgmtv1alpha1.DynamoDBConnectionConfig{
						Region:      ptr(secret + "-region"),
						Endpoint:    ptr(secret + "-endpoint"),
						Credentials: credentials,
					},
				},
			},
		}
		config, err := NewDynamoDbSyncBuilder(nil).BuildDestinationConfig(context.Background(), &bb_internal.DestinationParams{
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
			DestConnection: connection,
		})
		require.NoError(t, err)
		requireNamesWithoutHolding(t, config, connection.GetId(), secret)
		outputs = append(outputs, config.Outputs...)
	})

	// What the builders write is what the worker's plugins accept: every config is linted as the
	// worker builds its stream, by the worker's own Benthos environment.
	t.Run("the plugins accept what the builders write", func(t *testing.T) {
		env, err := benthos_environment.NewEnvironment(
			testutil.GetTestLogger(t),
			benthos_environment.WithConnections(func(string) (connectionmanager.ConnectionInput, error) {
				return nil, errors.New("not resolved: the configs are only linted")
			}),
			benthos_environment.WithStopChannel(make(chan error, 1)),
			benthos_environment.WithBlobEnv(bloblang.NewEnvironment()),
		)
		require.NoError(t, err)
		require.Len(t, sources, 1)
		require.Len(t, outputs, 2)

		// As the generator does: the destinations' outputs go into the broker of the source's.
		stream := sources[0]
		stream.Output.Broker.Outputs = append(stream.Output.Broker.Outputs, outputs...)
		configs := []*husonym_benthos.BenthosConfig{stream}
		for _, config := range configs {
			stored, err := yaml.Marshal(config)
			require.NoError(t, err)
			require.NoError(t, env.NewStreamBuilder().SetYAML(string(stored)), "%s", stored)
		}
	})
}

// requireNamesWithoutHolding checks that the stored form of a destination config names its
// connection, so that the worker resolves it, and holds nothing of it.
func requireNamesWithoutHolding(
	t *testing.T,
	config *bb_internal.BenthosDestinationConfig,
	connectionId string,
	secret string,
) {
	t.Helper()
	stored, err := yaml.Marshal(husonym_benthos.OutputConfig{
		Outputs: husonym_benthos.Outputs{Broker: &husonym_benthos.OutputBrokerConfig{Outputs: config.Outputs}},
	})
	require.NoError(t, err)
	require.False(t, strings.Contains(string(stored), secret), "the stored config holds a secret:\n%s", stored)
	require.Contains(t, string(stored), "connection_id: "+connectionId)

	named := []string{}
	for _, dsn := range config.BenthosDsns {
		named = append(named, dsn.ConnectionId)
	}
	require.Contains(t, named, connectionId, "the worker only resolves the connections a config declares")
}

func ptr[T any](v T) *T { return &v }
