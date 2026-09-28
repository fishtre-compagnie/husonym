package openaigenerate

import (
	"fmt"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/redpanda-data/benthos/v4/public/service"
	"github.com/stretchr/testify/require"
)

func Test_GenerateReader_ReadsTheConnection(t *testing.T) {
	getConnection := func(connectionId string) (connectionmanager.ConnectionInput, error) {
		switch connectionId {
		case "openai":
			return openaiConnection("https://llm.example.com/v1"), nil
		case "openai-default-url":
			return openaiConnection(""), nil
		case "pg":
			return &mgmtv1alpha1.Connection{
				Id: "pg",
				ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
					Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
				},
			}, nil
		}
		return nil, fmt.Errorf("unable to find connection by id: %q", connectionId)
	}

	reader, err := newGenerateReader(parseConfig(t, "openai"), getConnection, service.MockResources())
	require.NoError(t, err)
	require.Equal(t, "the-key", reader.apikey)
	require.Equal(t, "https://llm.example.com/v1", reader.apiUrl)

	reader, err = newGenerateReader(parseConfig(t, "openai-default-url"), getConnection, service.MockResources())
	require.NoError(t, err)
	require.Equal(t, openaiApiUrl, reader.apiUrl)

	_, err = newGenerateReader(parseConfig(t, "unknown"), getConnection, service.MockResources())
	require.ErrorContains(t, err, "unknown")

	_, err = newGenerateReader(parseConfig(t, "pg"), getConnection, service.MockResources())
	require.ErrorContains(t, err, "not an OpenAI connection")
}

func openaiConnection(apiUrl string) *mgmtv1alpha1.Connection {
	return &mgmtv1alpha1.Connection{
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_OpenaiConfig{
				OpenaiConfig: &mgmtv1alpha1.OpenAiConnectionConfig{ApiKey: "the-key", ApiUrl: apiUrl},
			},
		},
	}
}

func parseConfig(t *testing.T, connectionId string) *service.ParsedConfig {
	t.Helper()
	conf, err := getSpec().ParseYAML(`
connection_id: `+connectionId+`
columns: [a]
data_types: [text]
model: gpt
count: 1
batch_size: 1
`, service.NewEmptyEnvironment())
	require.NoError(t, err)
	return conf
}
