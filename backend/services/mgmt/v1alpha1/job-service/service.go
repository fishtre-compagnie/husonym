package v1alpha1_jobservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	jobhooks "github.com/fishtre-compagnie/husonym/backend/internal/ee/hooks/jobs"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	sql_manager "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	clientmanager "github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
)

type Service struct {
	cfg               *Config
	db                *husonymdb.HusonymDb
	connectionService mgmtv1alpha1connect.ConnectionServiceClient
	userdataclient    userdata.Interface
	sqlmanager        sql_manager.SqlManagerClient

	temporalmgr clientmanager.Interface

	hookService           jobhooks.Interface
	connectiondatabuilder connectiondata.ConnectionDataBuilder
}

type RunLogType string

const (
	KubePodRunLogType RunLogType = "k8s-pods"
	LokiRunLogType    RunLogType = "loki"
)

type KubePodRunLogConfig struct {
	Namespace     string
	WorkerAppName string
}

type LokiRunLogConfig struct {
	BaseUrl string

	LabelsQuery string // Labels to filter loki by, without the curly braces
	// labels to keep after the json filtering. Keeps ordering. Not sure if it will always need to equal the labels query keys, so separating this
	KeepLabels []string
}

type Config struct {
	IsAuthEnabled  bool
	IsHusonymCloud bool

	RunLogConfig *RunLogConfig
}

type RunLogConfig struct {
	IsEnabled        bool
	RunLogType       *RunLogType
	RunLogPodConfig  *KubePodRunLogConfig // required if RunLogType is k8s-pods
	LokiRunLogConfig *LokiRunLogConfig    // required if RunLogType is loki
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	temporalWfManager clientmanager.Interface,
	connectionService mgmtv1alpha1connect.ConnectionServiceClient,
	sqlmanager sql_manager.SqlManagerClient,
	jobhookService jobhooks.Interface,
	userdataclient userdata.Interface,
	connectiondatabuilder connectiondata.ConnectionDataBuilder,
) *Service {
	return &Service{
		cfg:                   cfg,
		db:                    db,
		temporalmgr:           temporalWfManager,
		connectionService:     connectionService,
		sqlmanager:            sqlmanager,
		hookService:           jobhookService,
		userdataclient:        userdataclient,
		connectiondatabuilder: connectiondatabuilder,
	}
}
