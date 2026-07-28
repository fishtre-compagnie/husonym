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
}

type ClientConfigOption func(*clientConfig)

func WithUserId(userId string) ClientConfigOption {
	return func(c *clientConfig) {
		c.userId = userId
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

func getHydratedClientConfig(opts ...ClientConfigOption) *clientConfig {
	config := &clientConfig{}
	for _, opt := range opts {
		opt(config)
	}
	return config
}

func getHttpClient(config *clientConfig) *http.Client {
	if config.userId != "" {
		return http_client.WithBearerAuth(&http.Client{}, &config.userId)
	}
	return &http.Client{}
}
