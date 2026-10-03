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
			&ConnectionConfig{PgConfig: &PostgresConnectionConfig{Connection: &PostgresConnection{Pass: SensitiveValue}}},
			"password",
		},
		"mysql dsn": {
			&ConnectionConfig{MysqlConfig: &MysqlConnectionConfig{Url: ptr("u:" + uriSensitiveValue + "@tcp(db:3306)/app")}},
			"connection url",
		},
		"s3 session token": {
			&ConnectionConfig{AwsS3Config: &AwsS3ConnectionConfig{Credentials: &AwsS3Credentials{SessionToken: ptr(SensitiveValue)}}},
			"session token",
		},
		"openai url": {
			&ConnectionConfig{OpenAiConfig: &OpenAiConnectionConfig{ApiUrl: "https://api?api-key=" + uriSensitiveValue}},
			"api url",
		},
		"mongo client tls": {
			&ConnectionConfig{
				MongoConfig: &MongoConnectionConfig{Url: ptr("mongodb://db"), ClientTls: &ClientTls{ClientKey: ptr(SensitiveValue)}},
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

// A DSN that does not parse is masked whole, and told masked though the mask does not parse
// either.
func Test_ConnectionConfig_MaskedSecret_MysqlDsnMaskedWhole(t *testing.T) {
	config := &ConnectionConfig{MysqlConfig: &MysqlConnectionConfig{Url: ptr("%%" + secret)}}
	masked, err := config.ToDto(false)
	require.NoError(t, err)
	require.Equal(t, uriSensitiveValue, masked.GetMysqlConfig().GetUrl())
	back := &ConnectionConfig{}
	require.NoError(t, back.FromDto(masked))
	field, ok := back.MaskedSecret()
	require.True(t, ok)
	require.Equal(t, "connection url", field)
}

// A secret the config does not hold is not masked on the way out: the mask would pass for one
// on the way back, and the config be refused for a secret it never had.
func Test_ConnectionConfig_MaskedSecret_AbsentSecretRoundTrips(t *testing.T) {
	cases := map[string]*ConnectionConfig{
		"client tls without key": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://db/app"), ClientTls: &ClientTls{RootCert: ptr("ca")},
		}},
		"ssh private key without passphrase": {MysqlConfig: &MysqlConnectionConfig{
			Connection: &MysqlConnection{Host: "db"},
			SSHTunnel: &SSHTunnel{Host: "bastion", SSHAuthentication: &SSHAuthentication{
				SSHPrivateKey: &SSHPrivateKey{},
			}},
		}},
		"ssh private key with an empty passphrase": {MssqlConfig: &MssqlConfig{
			Url: ptr("sqlserver://db"),
			SSHTunnel: &SSHTunnel{Host: "bastion", SSHAuthentication: &SSHAuthentication{
				SSHPrivateKey: &SSHPrivateKey{Passphrase: ptr("")},
			}},
		}},
		"client tls with an empty key": {MysqlConfig: &MysqlConnectionConfig{
			Connection: &MysqlConnection{Host: "db"}, ClientTls: &ClientTls{ClientKey: ptr("")},
		}},
		"url with an empty password":         {PgConfig: &PostgresConnectionConfig{Url: ptr("postgres://u:@db/app")}},
		"url with an empty secret parameter": {PgConfig: &PostgresConnectionConfig{Url: ptr("postgres://u@db/app?sslpassword=")}},
		"dsn with an empty secret parameter": {MysqlConfig: &MysqlConnectionConfig{
			Url: ptr("u@tcp(db:3306)/app?sslpassword="),
		}},
		"ssh without passphrase": {MongoConfig: &MongoConnectionConfig{
			Url: ptr("mongodb://db"),
			SSHTunnel: &SSHTunnel{Host: "bastion", SSHAuthentication: &SSHAuthentication{
				SSHPassphrase: &SSHPassphrase{},
			}},
		}},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			masked, err := config.ToDto(false)
			require.NoError(t, err)
			back := &ConnectionConfig{}
			require.NoError(t, back.FromDto(masked))
			field, ok := back.MaskedSecret()
			require.False(t, ok, "told masked: %s", field)
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
