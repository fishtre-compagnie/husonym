package v1alpha1_transformersservice

import (
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

type Service struct {
	cfg            *Config
	db             *husonymdb.HusonymDb
	entityclient   presidio.EntityLister
	userdataclient userdata.Interface
	license        license.EEInterface
}

type Config struct {
	IsPresidioEnabled bool
	// PresidioDefaultLanguage is the language the PII entities are listed for. Unset, they
	// are listed for English.
	PresidioDefaultLanguage *string
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	recognizerclient presidio.EntityLister,
	userdataclient userdata.Interface,
	licenseClient license.EEInterface,
) *Service {
	return &Service{
		cfg:            cfg,
		db:             db,
		entityclient:   recognizerclient,
		userdataclient: userdataclient,
		license:        licenseClient,
	}
}
