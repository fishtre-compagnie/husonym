package v1alpha1_apikeyservice

import (
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

type Service struct {
	cfg            *Config
	db             *husonymdb.HusonymDb
	userdataclient userdata.Interface
}

type Config struct {
	IsAuthEnabled bool
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userdataclient userdata.Interface,
) *Service {
	return &Service{cfg: cfg, db: db, userdataclient: userdataclient}
}
