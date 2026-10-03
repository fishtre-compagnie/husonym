package v1alpha1_connectiondataservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

type Service struct {
	cfg                   *Config
	connectionService     mgmtv1alpha1connect.ConnectionServiceClient
	connectiondatabuilder connectiondata.ConnectionDataBuilder
	// analyze est le client Presidio utilisé pour le scan de contenu PII.
	// nil si Presidio n'est pas configuré (PRESIDIO_ANALYZER_URL vide).
	analyze presidio.Analyzer
	// transformers runs a transformer on sampled values for the column preview.
	transformers Transformers
}

type Config struct {
	// IsPresidioEnabled indique si le scan de contenu PII est disponible.
	IsPresidioEnabled bool
	// PresidioDefaultLanguage est la langue par défaut envoyée à Presidio.
	PresidioDefaultLanguage *string
}

// Transformers is what the column preview needs to run a transformer exactly the way a job and
// AnonymizeMany do: the transformer service, to resolve user-defined transformers by id, and the
// Presidio clients, for the transformers that call Presidio. Presidio may be absent, in which case
// IsPresidioEnabled is false and those transformers report the failure in the preview itself.
//
// The transformers that call Presidio also need a valid license, as they do in AnonymizeMany.
// License is read on every preview, so a license that lapses or comes back is followed without
// a restart; the other transformers never look at it.
type Transformers struct {
	Client            mgmtv1alpha1connect.TransformersServiceClient
	IsPresidioEnabled bool
	Analyze           presidioapi.AnalyzeInterface
	Anonymize         presidioapi.AnonymizeInterface
	License           license.EEInterface
}

func New(
	cfg *Config,
	connectionService mgmtv1alpha1connect.ConnectionServiceClient,
	connectiondatabuilder connectiondata.ConnectionDataBuilder,
	analyze presidio.Analyzer,
	transformers Transformers,
) *Service {
	return &Service{
		cfg:                   cfg,
		connectionService:     connectionService,
		connectiondatabuilder: connectiondatabuilder,
		analyze:               analyze,
		transformers:          transformers,
	}
}
