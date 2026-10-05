package serve_connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func Test_newPresidioClients(t *testing.T) {
	t.Run("the analyzer enables every Presidio feature", func(t *testing.T) {
		clients, err := newPresidioClients("http://analyzer:3000", "", "")
		require.NoError(t, err)
		require.NotNil(t, clients.analyzer)
		require.NotNil(t, clients.entities)
		require.NotNil(t, clients.piiText)
		require.True(t, clients.transformsText())

		message := clients.summary()
		require.Contains(t, message, "PII text transformation")
		require.Contains(t, message, "PII content scan")
	})

	t.Run("without an analyzer nothing is enabled", func(t *testing.T) {
		clients, err := newPresidioClients("", "", "fr")
		require.NoError(t, err)
		// Nil, and not an interface that holds a nil client: the services compare it to nil.
		require.Nil(t, clients.analyzer)
		require.Nil(t, clients.entities)
		require.Nil(t, clients.piiText)
		require.False(t, clients.transformsText())

		require.Contains(t, clients.summary(), "not configured")
	})

	t.Run("a URL that cannot be one stops the startup", func(t *testing.T) {
		_, err := newPresidioClients("analyzer:3000", "", "")
		require.ErrorContains(t, err, "PRESIDIO_ANALYZER_URL")
	})
}

// A deployment that names an anonymizer starts as one that names none: the setting is not
// read, and the operator is told so.
func Test_unusedPresidioSettings(t *testing.T) {
	t.Run("nothing to tell when no anonymizer is named", func(t *testing.T) {
		_, ok := unusedPresidioSettings()
		require.False(t, ok)
	})

	t.Run("an anonymizer that is named is not used, whatever its URL", func(t *testing.T) {
		viper.Set("PRESIDIO_ANALYZER_URL", "http://analyzer:3000")
		viper.Set("PRESIDIO_ANONYMIZER_URL", "not a url")
		t.Cleanup(func() {
			viper.Set("PRESIDIO_ANALYZER_URL", nil)
			viper.Set("PRESIDIO_ANONYMIZER_URL", nil)
		})

		clients, err := getPresidioClients()
		require.NoError(t, err)
		require.True(t, clients.transformsText())

		message, ok := unusedPresidioSettings()
		require.True(t, ok)
		require.Contains(t, message, "PRESIDIO_ANONYMIZER_URL")
		require.Contains(t, message, "not used")
	})
}

// The transformer service lists the entities for the language the deployment sets: it is told
// that language, and whether the analyzer is there.
func Test_presidioClients_transformerServiceConfig(t *testing.T) {
	withAnalyzer, err := newPresidioClients("http://analyzer:3000", "", "")
	require.NoError(t, err)
	without, err := newPresidioClients("", "", "")
	require.NoError(t, err)

	t.Run("the language the deployment sets", func(t *testing.T) {
		viper.Set("PRESIDIO_DEFAULT_LANGUAGE", "fr")
		t.Cleanup(func() { viper.Set("PRESIDIO_DEFAULT_LANGUAGE", nil) })

		config := withAnalyzer.transformerServiceConfig()
		require.True(t, config.IsPresidioEnabled)
		require.NotNil(t, config.PresidioDefaultLanguage)
		require.Equal(t, "fr", *config.PresidioDefaultLanguage)
	})

	t.Run("no language set", func(t *testing.T) {
		config := withAnalyzer.transformerServiceConfig()
		require.True(t, config.IsPresidioEnabled)
		require.Nil(t, config.PresidioDefaultLanguage)
	})

	t.Run("without an analyzer the listing is not enabled", func(t *testing.T) {
		require.False(t, without.transformerServiceConfig().IsPresidioEnabled)
	})
}

// The authorization of the deployment accompanies the calls to the analyzer, as it is set.
func Test_newPresidioClients_SendsTheAuthorization(t *testing.T) {
	var authorizations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	clients, err := newPresidioClients(srv.URL, "Bearer secret", "")
	require.NoError(t, err)
	_, err = clients.entities.SupportedEntities(context.Background(), "en")
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer secret"}, authorizations)

	authorizations = nil
	clients, err = newPresidioClients(srv.URL, "", "")
	require.NoError(t, err)
	_, err = clients.entities.SupportedEntities(context.Background(), "en")
	require.NoError(t, err)
	require.Equal(t, []string{""}, authorizations)
}
