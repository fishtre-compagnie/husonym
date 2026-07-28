package v1alpha1_connectionservice

import (
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/pkg/mongoconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	sql_manager "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	awsmanager "github.com/fishtre-compagnie/husonym/internal/aws"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

type Service struct {
	cfg            *Config
	db             *husonymdb.HusonymDb
	userclient     userdata.Interface
	sqlConnector   sqlconnect.SqlConnector
	sqlmanager     sql_manager.SqlManagerClient
	mongoconnector mongoconnect.Interface
	awsManager     awsmanager.HusonymAwsManagerClient
}

type Config struct {
	IsHusonymCloud bool
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	userclient userdata.Interface,
	mongoconnector mongoconnect.Interface,
	awsManager awsmanager.HusonymAwsManagerClient,
	sqlmanager sql_manager.SqlManagerClient,
	sqlconnector sqlconnect.SqlConnector,
) *Service {
	return &Service{
		cfg:            cfg,
		db:             db,
		userclient:     userclient,
		sqlmanager:     sqlmanager,
		mongoconnector: mongoconnector,
		awsManager:     awsManager,
		sqlConnector:   sqlconnector,
	}
}
