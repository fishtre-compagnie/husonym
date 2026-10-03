package serve_connect

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	v1alpha1_transformerservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/transformers-service"
	http_client "github.com/fishtre-compagnie/husonym/internal/http/client"
	"github.com/spf13/viper"
)

// presidioClients are the clients of the Presidio services the deployment configures. A
// service that is not configured has a nil client.
type presidioClients struct {
	analyzer   presidio.Analyzer
	entities   presidio.EntityLister
	anonymizer presidio.Anonymizer
}

func getPresidioClients() (*presidioClients, error) {
	return newPresidioClients(
		viper.GetString("PRESIDIO_ANALYZER_URL"),
		viper.GetString("PRESIDIO_ANONYMIZER_URL"),
		viper.GetString("PRESIDIO_HEADER_AUTH_TOKEN"),
	)
}

// newPresidioClients builds the client of each service that has a URL. Both share one HTTP
// client, which sends the authorization, when there is one, exactly as it is given.
func newPresidioClients(analyzerURL, anonymizerURL, authorization string) (*presidioClients, error) {
	httpClient := &http.Client{}
	if authorization != "" {
		httpClient = http_client.WithAuth(httpClient, authorization)
	}

	clients := &presidioClients{}
	if analyzerURL != "" {
		analyzer, err := presidio.NewAnalyzerClient(analyzerURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("PRESIDIO_ANALYZER_URL: %w", err)
		}
		clients.analyzer = analyzer
		clients.entities = analyzer
	}
	if anonymizerURL != "" {
		anonymizer, err := presidio.NewAnonymizerClient(anonymizerURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("PRESIDIO_ANONYMIZER_URL: %w", err)
		}
		clients.anonymizer = anonymizer
	}
	return clients, nil
}

// transformsText says whether texts can be transformed, which takes both services: one finds
// the personal data, the other rewrites it.
func (c *presidioClients) transformsText() bool {
	return c.analyzer != nil && c.anonymizer != nil
}

// transformerServiceConfig is what the transformer service is told about Presidio: whether the
// PII text transformer and the listing of its entities are enabled, and the language the
// deployment sets, which the entities are listed for.
func (c *presidioClients) transformerServiceConfig() *v1alpha1_transformerservice.Config {
	return &v1alpha1_transformerservice.Config{
		IsPresidioEnabled:       c.transformsText(),
		PresidioDefaultLanguage: getPresidioDefaultLanguage(),
	}
}

// summary tells the operator what Presidio is used for, given the services configured.
func (c *presidioClients) summary() (level slog.Level, message string) {
	switch {
	case c.transformsText():
		return slog.LevelInfo, "presidio is enabled: PII text transformation, PII entity listing and PII content scan"
	case c.analyzer != nil:
		return slog.LevelInfo, "presidio analyzer is enabled: PII content scan only " +
			"(set PRESIDIO_ANONYMIZER_URL to enable the PII text transformation)"
	case c.anonymizer != nil:
		return slog.LevelWarn, "PRESIDIO_ANONYMIZER_URL is set without PRESIDIO_ANALYZER_URL: " +
			"no Presidio feature is enabled"
	default:
		return slog.LevelInfo, "presidio is not configured: PII text transformation and PII content scan are disabled"
	}
}

func getPresidioDefaultLanguage() *string {
	lang := viper.GetString("PRESIDIO_DEFAULT_LANGUAGE")
	if lang == "" {
		return nil
	}
	return &lang
}
