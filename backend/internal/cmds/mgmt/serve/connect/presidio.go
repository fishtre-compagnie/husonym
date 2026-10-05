package serve_connect

import (
	"fmt"
	"net/http"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	v1alpha1_transformerservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/transformers-service"
	http_client "github.com/fishtre-compagnie/husonym/internal/http/client"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"github.com/spf13/viper"
)

// presidioClients is what the deployment does with Presidio's analyzer, the one Presidio
// service the API calls. Without an analyzer everything here is nil.
type presidioClients struct {
	analyzer presidio.Analyzer
	entities presidio.EntityLister
	// piiText anonymizes free text: the analyzer finds the personal data, the engine
	// rewrites it.
	piiText *piitext.Engine
}

func getPresidioClients() (*presidioClients, error) {
	return newPresidioClients(
		viper.GetString("PRESIDIO_ANALYZER_URL"),
		viper.GetString("PRESIDIO_HEADER_AUTH_TOKEN"),
		viper.GetString("PRESIDIO_DEFAULT_LANGUAGE"),
	)
}

// newPresidioClients builds what an analyzer at analyzerURL serves. Its HTTP client sends the
// authorization, when there is one, exactly as it is given. defaultLanguage is the language of
// the texts whose transformer names none.
func newPresidioClients(analyzerURL, authorization, defaultLanguage string) (*presidioClients, error) {
	clients := &presidioClients{}
	if analyzerURL == "" {
		return clients, nil
	}

	httpClient := &http.Client{}
	if authorization != "" {
		httpClient = http_client.WithAuth(httpClient, authorization)
	}
	analyzer, err := presidio.NewAnalyzerClient(analyzerURL, httpClient)
	if err != nil {
		return nil, fmt.Errorf("PRESIDIO_ANALYZER_URL: %w", err)
	}
	engine, err := piitext.NewEngine(analyzer, defaultLanguage)
	if err != nil {
		return nil, err
	}
	clients.analyzer = analyzer
	clients.entities = analyzer
	clients.piiText = engine
	return clients, nil
}

// transformsText says whether texts can be transformed, which takes the analyzer.
func (c *presidioClients) transformsText() bool {
	return c.piiText != nil
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

// summary tells the operator what Presidio is used for.
func (c *presidioClients) summary() string {
	if c.transformsText() {
		return "presidio is enabled: PII text transformation, PII entity listing and PII content scan"
	}
	return "presidio is not configured: PII text transformation and PII content scan are disabled"
}

// unusedPresidioSettings tells the operator about a Presidio setting the API does not read, or
// nothing. The API rewrites what the analyzer finds by itself: it calls no anonymizer, and a
// deployment that names one starts and runs the same as one that does not.
func unusedPresidioSettings() (message string, ok bool) {
	if viper.GetString("PRESIDIO_ANONYMIZER_URL") == "" {
		return "", false
	}
	return "PRESIDIO_ANONYMIZER_URL is set and not used: the API rewrites what the analyzer finds, " +
		"and calls no Presidio anonymizer", true
}

func getPresidioDefaultLanguage() *string {
	lang := viper.GetString("PRESIDIO_DEFAULT_LANGUAGE")
	if lang == "" {
		return nil
	}
	return &lang
}
