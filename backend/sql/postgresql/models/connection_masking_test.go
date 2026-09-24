package pg_models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// secret marks every value a caller without connection:view_sensitive must not see. Each case
// below puts it in one place a credential lives, and the masked config must not carry it back.
const secret = "S3CRETmarker"

func ptr(s string) *string { return &s }

func sshTunnels() map[string]*SSHTunnel {
	return map[string]*SSHTunnel{
		"ssh passphrase": {Host: "bastion", SSHAuthentication: &SSHAuthentication{
			SSHPassphrase: &SSHPassphrase{Value: secret},
		}},
		"ssh private key": {Host: "bastion", SSHAuthentication: &SSHAuthentication{
			SSHPrivateKey: &SSHPrivateKey{Value: secret, Passphrase: ptr(secret)},
		}},
	}
}

func clientTls() *ClientTls {
	return &ClientTls{ClientKey: ptr(secret)}
}

func Test_ConnectionConfig_ToDto_MasksEverySecret(t *testing.T) {
	awsCredentials := func() *AwsS3Credentials {
		return &AwsS3Credentials{AccessKeyId: ptr("AKIA"), SecretAccessKey: ptr(secret), SessionToken: ptr(secret)}
	}
	cases := map[string]*ConnectionConfig{
		"postgres password": {PgConfig: &PostgresConnectionConfig{
			Connection: &PostgresConnection{Host: "db", Pass: secret},
		}},
		"postgres url, user password": {PgConfig: &PostgresConnectionConfig{Url: ptr("postgres://u:" + secret + "@db/app")}},
		"postgres url, password parameter": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://u@db/app?sslmode=require&password=" + secret),
		}},
		"postgres url, sslpassword parameter": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://u@db/app?sslpassword=" + secret),
		}},
		"postgres keyword string": {PgConfig: &PostgresConnectionConfig{Url: ptr("host=db password=" + secret)}},
		"postgres client tls":     {PgConfig: &PostgresConnectionConfig{Url: ptr("postgres://db/app"), ClientTls: clientTls()}},
		"mysql password": {MysqlConfig: &MysqlConnectionConfig{
			Connection: &MysqlConnection{Host: "db", Pass: secret},
		}},
		"mysql dsn":                {MysqlConfig: &MysqlConnectionConfig{Url: ptr("u:" + secret + "@tcp(db:3306)/app")}},
		"mssql url, user password": {MssqlConfig: &MssqlConfig{Url: ptr("sqlserver://u:" + secret + "@db")}},
		"mssql url, password parameter": {MssqlConfig: &MssqlConfig{
			Url: ptr("sqlserver://u@db?database=app&password=" + secret),
		}},
		"mssql ado string":         {MssqlConfig: &MssqlConfig{Url: ptr("server=db;user id=u;password=" + secret)}},
		"mongo url, user password": {MongoConfig: &MongoConnectionConfig{Url: ptr("mongodb://u:" + secret + "@db/app")}},
		"mongo url, aws session token": {MongoConfig: &MongoConnectionConfig{
			Url: ptr("mongodb://db/?authMechanism=MONGODB-AWS&authMechanismProperties=AWS_SESSION_TOKEN:" + secret),
		}},
		"aws s3":           {AwsS3Config: &AwsS3ConnectionConfig{Bucket: "b", Credentials: awsCredentials()}},
		"dynamo":           {DynamoDBConfig: &DynamoDBConfig{Credentials: awsCredentials()}},
		"gcp":              {GcpCloudStorageConfig: &GcpCloudStorageConfig{Bucket: "b", ServiceAccountCredentials: ptr(secret)}},
		"openai":           {OpenAiConfig: &OpenAiConnectionConfig{ApiUrl: "https://api", ApiKey: secret}},
		"mssql opaque url": {MssqlConfig: &MssqlConfig{Url: ptr("sqlserver:db;password=" + secret)}},
		"postgres url, fragment": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://u@db/app#password=" + secret),
		}},
		"postgres url, query split by semicolons": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://u@db/app?sslmode=require;password=" + secret),
		}},
		"postgres url, pass parameter": {PgConfig: &PostgresConnectionConfig{Url: ptr("postgres://u@db/app?pass=" + secret)}},
		// An unencoded / or # in a password breaks the parse, and the parse error quotes it.
		"postgres url that does not parse": {PgConfig: &PostgresConnectionConfig{
			Url: ptr("postgres://admin:" + secret + "/x@db/app"),
		}},
		"mongo url that does not parse": {MongoConfig: &MongoConnectionConfig{
			Url: ptr("mongodb://admin:" + secret + "#x@db/app"),
		}},
		"mysql dsn parameter": {MysqlConfig: &MysqlConnectionConfig{
			Url: ptr("u@tcp(db:3306)/app?tls=true&sslpassword=" + secret),
		}},
		"openai key in the url": {OpenAiConfig: &OpenAiConnectionConfig{
			ApiUrl: "https://example.openai.azure.com/openai?api-key=" + secret,
		}},
	}
	for name, tunnel := range sshTunnels() {
		cases["postgres "+name] = &ConnectionConfig{PgConfig: &PostgresConnectionConfig{
			Connection: &PostgresConnection{Host: "db"}, SSHTunnel: tunnel,
		}}
	}

	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			dto, err := config.ToDto(false)
			require.NoError(t, err)
			out, err := protojson.Marshal(dto)
			require.NoError(t, err)
			require.NotContains(t, string(out), secret)

			// The same config, for a caller who may see secrets, does carry the marker: the case
			// does put it where a secret lives, rather than pass for having nothing to mask.
			clear, err := config.ToDto(true)
			require.NoError(t, err)
			out, err = protojson.Marshal(clear)
			require.NoError(t, err)
			require.True(t, strings.Contains(string(out), secret), "the case hides no secret: %s", out)
		})
	}
}

func Test_maskUrl_KeepsWhatIsNotSecret(t *testing.T) {
	config := &ConnectionConfig{PgConfig: &PostgresConnectionConfig{
		Url: ptr("postgres://u:" + secret + "@db.internal:5432/app?sslmode=require&password=" + secret),
	}}
	dto, err := config.ToDto(false)
	require.NoError(t, err)
	url := dto.GetPgConfig().GetUrl()
	require.Contains(t, url, "u:"+uriSensitiveValue+"@db.internal:5432/app")
	require.Contains(t, url, "sslmode=require")
	require.Contains(t, url, "password="+uriSensitiveValue)
}
