package pg_models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every secret ToDto masks is told masked once the config comes back: a caller who read it
// masked cannot store the mask in its place.
func Test_ConnectionConfig_MaskedSecret_TellsEveryMask(t *testing.T) {
	for name, config := range secretCases() {
		t.Run(name, func(t *testing.T) {
			masked, err := config.ToDto(false)
			require.NoError(t, err)
			back := &ConnectionConfig{}
			require.NoError(t, back.FromDto(masked))
			_, ok := back.MaskedSecret()
			require.True(t, ok, "the masked config passes for a clear one")

			clear, err := config.ToDto(true)
			require.NoError(t, err)
			back = &ConnectionConfig{}
			require.NoError(t, back.FromDto(clear))
			field, ok := back.MaskedSecret()
			require.False(t, ok, "the clear config is told masked: %s", field)
		})
	}
}

func Test_ConnectionConfig_MaskedSecret_NamesTheField(t *testing.T) {
	cases := map[string]struct {
		config *ConnectionConfig
		field  string
	}{
		"postgres password": {
			&ConnectionConfig{PgConfig: &PostgresConnectionConfig{Connection: &PostgresConnection{Pass: sensitiveValue}}},
			"password",
		},
		"mysql dsn": {
			&ConnectionConfig{MysqlConfig: &MysqlConnectionConfig{Url: ptr("u:" + uriSensitiveValue + "@tcp(db:3306)/app")}},
			"connection url",
		},
		"s3 session token": {
			&ConnectionConfig{AwsS3Config: &AwsS3ConnectionConfig{Credentials: &AwsS3Credentials{SessionToken: ptr(sensitiveValue)}}},
			"session token",
		},
		"openai url": {
			&ConnectionConfig{OpenAiConfig: &OpenAiConnectionConfig{ApiUrl: "https://api?api-key=" + uriSensitiveValue}},
			"api url",
		},
		"mongo client tls": {
			&ConnectionConfig{
				MongoConfig: &MongoConnectionConfig{Url: ptr("mongodb://db"), ClientTls: &ClientTls{ClientKey: ptr(sensitiveValue)}},
			},
			"client tls key",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			field, ok := tc.config.MaskedSecret()
			require.True(t, ok)
			require.Equal(t, tc.field, field)
		})
	}
}

// A config with no secret, or whose secrets are left empty, has nothing masked.
func Test_ConnectionConfig_MaskedSecret_NothingToMask(t *testing.T) {
	cases := map[string]*ConnectionConfig{
		"local directory":   {LocalDirectoryConfig: &LocalDirectoryConnectionConfig{Path: "/tmp"}},
		"postgres from env": {PgConfig: &PostgresConnectionConfig{UrlEnv: ptr("PG_URL")}},
		"postgres without password": {PgConfig: &PostgresConnectionConfig{
			Connection: &PostgresConnection{Host: "db"},
		}},
		"s3 without credentials": {AwsS3Config: &AwsS3ConnectionConfig{Bucket: "b"}},
		"gcs without key":        {GcpCloudStorageConfig: &GcpCloudStorageConfig{Bucket: "b"}},
		"postgres url without secret": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://u@db/app_" + uriSensitiveValue + "?sslmode=require"),
		}},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			field, ok := config.MaskedSecret()
			require.False(t, ok, "told masked: %s", field)
		})
	}
}
