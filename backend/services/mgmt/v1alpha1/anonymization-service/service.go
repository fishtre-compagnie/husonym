package v1alpha_anonymizationservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/licenserefusal"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/piitext"
	"go.opentelemetry.io/otel/metric"
)

type Service struct {
	cfg                *Config
	meter              metric.Meter // optional
	userdataclient     userdata.Interface
	useraccountService mgmtv1alpha1connect.UserAccountServiceClient
	transformerClient  mgmtv1alpha1connect.TransformersServiceClient
	// piiText anonymizes free text; nil in a deployment with no Presidio analyzer.
	piiText *piitext.Engine
	db      *husonymdb.HusonymDb
	license license.EEInterface
	// refusals counts the refusals that are answered as a status and not as an error.
	refusals licenserefusal.Counter
}

type Config struct {
	IsAuthEnabled bool
	// WorkerOnly tells the worker from the other callers: a run hands the key of its hashes
	// with its calls, and it is read from the worker alone.
	WorkerOnly userdata.WorkerOnly
}

func New(
	cfg *Config,
	meter metric.Meter,
	userdataclient userdata.Interface,
	useraccountService mgmtv1alpha1connect.UserAccountServiceClient,
	transformerClient mgmtv1alpha1connect.TransformersServiceClient,
	piiText *piitext.Engine,
	db *husonymdb.HusonymDb,
	licenseClient license.EEInterface,
	refusals licenserefusal.Counter,
) *Service {
	return &Service{
		cfg:                cfg,
		meter:              meter,
		userdataclient:     userdataclient,
		useraccountService: useraccountService,
		transformerClient:  transformerClient,
		piiText:            piiText,
		db:                 db,
		license:            licenseClient,
		refusals:           refusals,
	}
}
