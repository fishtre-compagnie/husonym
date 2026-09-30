package pg_models

import (
	"log/slog"
	"net/url"
	"slices"

	dbconnectconfig "github.com/fishtre-compagnie/husonym/backend/pkg/dbconnect-config"
)

// MaskedSecret names a secret of the config that holds the mask ToDto puts in its place for a
// caller who may not see secrets. Such a config was read masked and sent back: stored, the mask
// would replace the secret. Nor may the stored secret be kept in its stead: the caller could
// point the connection at a server of theirs and have the secret sent there, unseen.
func (c *ConnectionConfig) MaskedSecret() (string, bool) {
	switch {
	case c.PgConfig != nil:
		pg := c.PgConfig
		if pg.Connection != nil && pg.Connection.Pass == sensitiveValue {
			return "password", true
		}
		if pg.Url != nil && isMaskedUrl(*pg.Url) {
			return "connection url", true
		}
		return maskedTransportSecret(pg.SSHTunnel, pg.ClientTls)
	case c.MysqlConfig != nil:
		my := c.MysqlConfig
		if my.Connection != nil && my.Connection.Pass == sensitiveValue {
			return "password", true
		}
		if my.Url != nil && isMaskedMysqlDsn(*my.Url) {
			return "connection url", true
		}
		return maskedTransportSecret(my.SSHTunnel, my.ClientTls)
	case c.MssqlConfig != nil:
		if c.MssqlConfig.Url != nil && isMaskedUrl(*c.MssqlConfig.Url) {
			return "connection url", true
		}
		return maskedTransportSecret(c.MssqlConfig.SSHTunnel, c.MssqlConfig.ClientTls)
	case c.MongoConfig != nil:
		if c.MongoConfig.Url != nil && isMaskedUrl(*c.MongoConfig.Url) {
			return "connection url", true
		}
		return maskedTransportSecret(c.MongoConfig.SSHTunnel, c.MongoConfig.ClientTls)
	case c.AwsS3Config != nil:
		return c.AwsS3Config.Credentials.maskedSecret()
	case c.DynamoDBConfig != nil:
		return c.DynamoDBConfig.Credentials.maskedSecret()
	case c.GcpCloudStorageConfig != nil:
		if isMasked(c.GcpCloudStorageConfig.ServiceAccountCredentials) {
			return "service account credentials", true
		}
	case c.OpenAiConfig != nil:
		if c.OpenAiConfig.ApiKey == sensitiveValue {
			return "api key", true
		}
		if isMaskedUrl(c.OpenAiConfig.ApiUrl) {
			return "api url", true
		}
	}
	return "", false
}

func (a *AwsS3Credentials) maskedSecret() (string, bool) {
	if a == nil {
		return "", false
	}
	if isMasked(a.SecretAccessKey) {
		return "secret access key", true
	}
	if isMasked(a.SessionToken) {
		return "session token", true
	}
	return "", false
}

func maskedTransportSecret(tunnel *SSHTunnel, tls *ClientTls) (string, bool) {
	if tunnel != nil && tunnel.SSHAuthentication != nil {
		auth := tunnel.SSHAuthentication
		if auth.SSHPassphrase != nil && auth.SSHPassphrase.Value == sensitiveValue {
			return "ssh passphrase", true
		}
		if auth.SSHPrivateKey != nil &&
			(auth.SSHPrivateKey.Value == sensitiveValue || isMasked(auth.SSHPrivateKey.Passphrase)) {
			return "ssh private key", true
		}
	}
	if tls != nil && isMasked(tls.ClientKey) {
		return "client tls key", true
	}
	return "", false
}

func isMasked(value *string) bool {
	return value != nil && *value == sensitiveValue
}

// isMaskedUrl says whether maskUrl masked part of the URL: the whole of it, the user's
// password, a parameter that may carry a credential or the fragment.
func isMaskedUrl(raw string) bool {
	if raw == uriSensitiveValue {
		return true
	}
	uri, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if password, ok := uri.User.Password(); ok && password == uriSensitiveValue {
		return true
	}
	if uri.Fragment == uriSensitiveValue {
		return true
	}
	for key, values := range uri.Query() {
		if isSecretQueryKey(key) && slices.Contains(values, uriSensitiveValue) {
			return true
		}
	}
	return false
}

// isMaskedMysqlDsn says whether maskMysqlDsn masked part of the connection string.
func isMaskedMysqlDsn(raw string) bool {
	if raw == uriSensitiveValue {
		return true
	}
	// Quiet: the string is the caller's, read at each write, and a warning of it is no news.
	dsn, err := dbconnectconfig.GetMysqlDsn(raw, slog.New(slog.DiscardHandler))
	if err != nil {
		return false
	}
	if dsn.Passwd == uriSensitiveValue {
		return true
	}
	for key, value := range dsn.Params {
		if isSecretQueryKey(key) && value == uriSensitiveValue {
			return true
		}
	}
	return false
}
