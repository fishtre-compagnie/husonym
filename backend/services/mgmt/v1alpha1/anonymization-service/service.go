package v1alpha_anonymizationservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"go.opentelemetry.io/otel/metric"
)

type Service struct {
	cfg                *Config
	meter              metric.Meter // optional
	userdataclient     userdata.Interface
	useraccountService mgmtv1alpha1connect.UserAccountServiceClient
	transformerClient  mgmtv1alpha1connect.TransformersServiceClient
	analyze            presidioapi.AnalyzeInterface
	anonymize          presidioapi.AnonymizeInterface
	db                 *husonymdb.HusonymDb
	license            license.EEInterface
}

type Config struct {
	IsAuthEnabled           bool
	IsHusonymCloud          bool
	IsPresidioEnabled       bool
	PresidioDefaultLanguage *string
}

func New(
	cfg *Config,
	meter metric.Meter,
	userdataclient userdata.Interface,
	useraccountService mgmtv1alpha1connect.UserAccountServiceClient,
	transformerClient mgmtv1alpha1connect.TransformersServiceClient,
	analyzeclient presidioapi.AnalyzeInterface,
	anonymizeclient presidioapi.AnonymizeInterface,
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
