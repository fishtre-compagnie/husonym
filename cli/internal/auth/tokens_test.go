package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	cli_logger "github.com/fishtre-compagnie/husonym/cli/internal/logger"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

type authStatus struct {
	mgmtv1alpha1connect.UnimplementedAuthServiceHandler
	enabled bool
}

func (a authStatus) GetAuthStatus(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetAuthStatusRequest],
) (*connect.Response[mgmtv1alpha1.GetAuthStatusResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetAuthStatusResponse{IsEnabled: a.enabled}), nil
}

// A caller that asked for the API key only never gets a person's session in its place.
// Not parallel: the API's address is read from viper, which is global.
func Test_GetHusonymHttpClient_ApiKeyOnly(t *testing.T) {
	logger := cli_logger.NewSLogger(cli_logger.GetCharmLevelOrDefault(false))
	serve := func(t *testing.T, enabled bool) {
		t.Helper()
		mux := http.NewServeMux()
		mux.Handle(mgmtv1alpha1connect.NewAuthServiceHandler(authStatus{enabled: enabled}))
		api := httptest.NewServer(mux)
		t.Cleanup(api.Close)
		viper.Set("HUSONYM_API_URL", api.URL)
		t.Cleanup(func() { viper.Set("HUSONYM_API_URL", "") })
	}
	empty := ""
	key := "key-123"

	t.Run("refused without a key when the API requires authentication", func(t *testing.T) {
		serve(t, true)
		_, err := GetHusonymHttpClient(t.Context(), logger, WithApiKey(&empty), WithApiKeyOnly())
		require.ErrorContains(t, err, ApiKeyEnvVarName)
	})

	t.Run("given a key", func(t *testing.T) {
		serve(t, true)
		client, err := GetHusonymHttpClient(t.Context(), logger, WithApiKey(&key), WithApiKeyOnly())
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("no key needed when the API requires none", func(t *testing.T) {
		serve(t, false)
		client, err := GetHusonymHttpClient(t.Context(), logger, WithApiKey(&empty), WithApiKeyOnly())
		require.NoError(t, err)
		require.NotNil(t, client)
	})
}
