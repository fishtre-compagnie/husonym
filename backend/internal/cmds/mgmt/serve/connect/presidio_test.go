package serve_connect

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

func Test_newPresidioClients(t *testing.T) {
	t.Run("both services", func(t *testing.T) {
		clients, err := newPresidioClients("http://analyzer:3000", "http://anonymizer:3000", "")
		require.NoError(t, err)
		require.NotNil(t, clients.analyzer)
		require.NotNil(t, clients.entities)
		require.NotNil(t, clients.anonymizer)
		require.True(t, clients.transformsText())

		level, message := clients.summary()
		require.Equal(t, slog.LevelInfo, level)
		require.Contains(t, message, "PII text transformation")
		require.Contains(t, message, "PII content scan")
	})

	t.Run("the analyzer alone scans content and transforms no text", func(t *testing.T) {
		clients, err := newPresidioClients("http://analyzer:3000", "", "")
		require.NoError(t, err)
		require.NotNil(t, clients.analyzer)
		require.NotNil(t, clients.entities)
		// Nil, and not an interface that holds a nil client: the services compare it to nil.
		require.Nil(t, clients.anonymizer)
		require.False(t, clients.transformsText())

		level, message := clients.summary()
		require.Equal(t, slog.LevelInfo, level)
		require.Contains(t, message, "PII content scan only")
		require.Contains(t, message, "PRESIDIO_ANONYMIZER_URL")
	})

	t.Run("the anonymizer alone enables nothing, and says so", func(t *testing.T) {
		clients, err := newPresidioClients("", "http://anonymizer:3000", "")
		require.NoError(t, err)
		require.Nil(t, clients.analyzer)
		require.Nil(t, clients.entities)
		require.NotNil(t, clients.anonymizer)
		require.False(t, clients.transformsText())

		level, message := clients.summary()
		require.Equal(t, slog.LevelWarn, level)
		require.Contains(t, message, "PRESIDIO_ANALYZER_URL")
	})

	t.Run("neither", func(t *testing.T) {
		clients, err := newPresidioClients("", "", "")
		require.NoError(t, err)
		require.Nil(t, clients.analyzer)
		require.Nil(t, clients.entities)
		require.Nil(t, clients.anonymizer)
		require.False(t, clients.transformsText())

		level, message := clients.summary()
		require.Equal(t, slog.LevelInfo, level)
		require.Contains(t, message, "not configured")
	})

	t.Run("a URL that cannot be one stops the startup", func(t *testing.T) {
		_, err := newPresidioClients("analyzer:3000", "", "")
		require.ErrorContains(t, err, "PRESIDIO_ANALYZER_URL")
		_, err = newPresidioClients("", "anonymizer", "")
		require.ErrorContains(t, err, "PRESIDIO_ANONYMIZER_URL")
	})
}

// The authorization of the deployment accompanies the calls to both services, as it is set.
func Test_newPresidioClients_SendsTheAuthorization(t *testing.T) {
	var authorizations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/anonymize" {
			_, _ = w.Write([]byte(`{"text":"x","items":[]}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	clients, err := newPresidioClients(srv.URL, srv.URL, "Bearer secret")
	require.NoError(t, err)
	_, err = clients.entities.SupportedEntities(context.Background(), "en")
	require.NoError(t, err)
	_, err = clients.anonymizer.Anonymize(context.Background(), &presidio.AnonymizeRequest{Text: "x"})
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer secret", "Bearer secret"}, authorizations)

	authorizations = nil
	clients, err = newPresidioClients(srv.URL, srv.URL, "")
	require.NoError(t, err)
	_, err = clients.entities.SupportedEntities(context.Background(), "en")
	require.NoError(t, err)
	require.Equal(t, []string{""}, authorizations)
}
