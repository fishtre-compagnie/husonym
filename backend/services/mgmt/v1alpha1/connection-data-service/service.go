package v1alpha1_connectiondataservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
)

type Service struct {
	cfg                   *Config
	connectionService     mgmtv1alpha1connect.ConnectionServiceClient
	connectiondatabuilder connectiondata.ConnectionDataBuilder
	// analyze est le client Presidio utilisé pour le scan de contenu PII.
	// nil si Presidio n'est pas configuré (PRESIDIO_ANALYZER_URL vide).
	analyze presidio.Analyzer
}

type Config struct {
	// IsPresidioEnabled indique si le scan de contenu PII est disponible.
	IsPresidioEnabled bool
	// PresidioDefaultLanguage est la langue par défaut envoyée à Presidio.
	PresidioDefaultLanguage *string
}

func New(
	cfg *Config,
	connectionService mgmtv1alpha1connect.ConnectionServiceClient,
	connectiondatabuilder connectiondata.ConnectionDataBuilder,
	analyze presidio.Analyzer,
) *Service {
	return &Service{
		cfg:                   cfg,
		connectionService:     connectionService,
		connectiondatabuilder: connectiondatabuilder,
		analyze:               analyze,
	}
}
