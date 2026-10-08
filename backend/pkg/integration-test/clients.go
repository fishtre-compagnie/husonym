package integrationtests_test

import (
	"net/http"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	http_client "github.com/fishtre-compagnie/husonym/internal/http/client"
)

type HusonymClients struct {
	httpUrl string
}

func newHusonymClients(httpUrl string) *HusonymClients {
	return &HusonymClients{
		httpUrl: httpUrl,
	}
}

type clientConfig struct {
	userId string
	issuer string
	// identityType is what the token says it was issued to: nothing, for a person.
	identityType string
}

type ClientConfigOption func(*clientConfig)

func WithUserId(userId string) ClientConfigOption {
	return func(c *clientConfig) {
		c.userId = userId
	}
}

// WithIssuer has the fake token of the client claim another issuer than TestIssuer: the identity
// of somebody a provider the deployment is not configured with vouches for.
func WithIssuer(issuer string) ClientConfigOption {
	return func(c *clientConfig) {
		c.issuer = issuer
	}
}

// WithApplicationToken has the fake token of the client say it was issued to an application
// rather than to a person, as the token of a service principal does.
func WithApplicationToken() ClientConfigOption {
	return func(c *clientConfig) {
		c.identityType = "app"
	}
}

func (s *HusonymClients) Users(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.UserAccountServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewUserAccountServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) Connections(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.ConnectionServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewConnectionServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) Anonymize(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.AnonymizationServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewAnonymizationServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) Jobs(opts ...ClientConfigOption) mgmtv1alpha1connect.JobServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewJobServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) Transformers(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.TransformersServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewTransformersServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) ConnectionData(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.ConnectionDataServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewConnectionDataServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) AccountHooks(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.AccountHookServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewAccountHookServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) AccountSettings(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.AccountSettingServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewAccountSettingServiceClient(getHttpClient(config), s.httpUrl)
}

func (s *HusonymClients) Usage(
	opts ...ClientConfigOption,
) mgmtv1alpha1connect.UsageServiceClient {
	config := getHydratedClientConfig(opts...)
	return mgmtv1alpha1connect.NewUsageServiceClient(getHttpClient(config), s.httpUrl)
}

func getHydratedClientConfig(opts ...ClientConfigOption) *clientConfig {
	config := &clientConfig{}
	for _, opt := range opts {
		opt(config)
	}
	return config
}

func getHttpClient(config *clientConfig) *http.Client {
	client := &http.Client{}
	if config.userId != "" {
		client = http_client.WithBearerAuth(client, &config.userId)
	}
	if config.issuer != "" {
		client = http_client.WithHeaders(client, map[string]string{issuerHeader: config.issuer})
	}
	if config.identityType != "" {
		client = http_client.WithHeaders(client, map[string]string{identityTypeHeader: config.identityType})
	}
	return client
}
