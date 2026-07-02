package v1alpha1_transformersservice

import (
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

type Service struct {
	cfg            *Config
	db             *husonymdb.HusonymDb
	entityclient   presidioapi.EntityInterface
	userdataclient userdata.Interface
	license        license.EEInterface
}

type Config struct {
	IsPresidioEnabled bool
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	recognizerclient presidioapi.EntityInterface,
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
