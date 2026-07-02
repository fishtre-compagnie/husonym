package v1alpha1_useraccountservice

import (
	auth_client "github.com/fishtre-compagnie/husonym/backend/internal/auth/client"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	"github.com/fishtre-compagnie/husonym/internal/billing"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
)

type Service struct {
	cfg                    *Config
	db                     *husonymdb.HusonymDb
	temporalConfigProvider clientmanager.ConfigProvider
	authclient             auth_client.Interface
	authadminclient        authmgmt.Interface
	billingclient          billing.Interface
	rbacClient             rbac.Interface
	licenseclient          license.EEInterface
}

type Config struct {
	IsAuthEnabled            bool
	IsHusonymCloud           bool
	DefaultMaxAllowedRecords *int64
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	temporalConfigProvider clientmanager.ConfigProvider,
	authclient auth_client.Interface,
	authadminclient authmgmt.Interface,
	billingclient billing.Interface,
	rbacClient rbac.Interface,
	licenseclient license.EEInterface,
) *Service {
	return &Service{
		cfg:                    cfg,
		db:                     db,
		temporalConfigProvider: temporalConfigProvider,
		authclient:             authclient,
		authadminclient:        authadminclient,
		billingclient:          billingclient,
		rbacClient:             rbacClient,
		licenseclient:          licenseclient,
	}
}

func (s *Service) UserDataClient() userdata.Interface {
	return userdata.NewClient(s, s.rbacClient, s.licenseclient)
}
