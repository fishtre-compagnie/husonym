package v1alpha_anonymizationservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"go.opentelemetry.io/otel/metric"
)

type Service struct {
	cfg                *Config
	meter              metric.Meter // optional
	userdataclient     userdata.Interface
	useraccountService mgmtv1alpha1connect.UserAccountServiceClient
	transformerClient  mgmtv1alpha1connect.TransformersServiceClient
	analyze            presidio.Analyzer
	anonymize          presidio.Anonymizer
	db                 *husonymdb.HusonymDb
	license            license.EEInterface
}

type Config struct {
	IsAuthEnabled           bool
	IsPresidioEnabled       bool
	PresidioDefaultLanguage *string
}

func New(
	cfg *Config,
	meter metric.Meter,
	userdataclient userdata.Interface,
	useraccountService mgmtv1alpha1connect.UserAccountServiceClient,
	transformerClient mgmtv1alpha1connect.TransformersServiceClient,
	analyzeclient presidio.Analyzer,
	anonymizeclient presidio.Anonymizer,
	db *husonymdb.HusonymDb,
	licenseClient license.EEInterface,
) *Service {
	return &Service{
		cfg:                cfg,
		meter:              meter,
		userdataclient:     userdataclient,
		useraccountService: useraccountService,
		transformerClient:  transformerClient,
		analyze:            analyzeclient,
		anonymize:          anonymizeclient,
		db:                 db,
		license:            licenseClient,
	}
}
