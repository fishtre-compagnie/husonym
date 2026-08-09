package integrationtests_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	auth_client "github.com/fishtre-compagnie/husonym/backend/internal/auth/client"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	"github.com/fishtre-compagnie/husonym/internal/billing"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	ee_slack "github.com/fishtre-compagnie/husonym/internal/ee/slack"
	neomigrate "github.com/fishtre-compagnie/husonym/internal/migrate"
	promapiv1mock "github.com/fishtre-compagnie/husonym/internal/mocks/github.com/prometheus/client_golang/api/prometheus/v1"
	clientmanager "github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	tmprl_mocks "go.temporal.io/sdk/mocks"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Mocks struct {
	TemporalClientManager  *clientmanager.MockInterface
	TemporalConfigProvider *clientmanager.MockConfigProvider
	TemporalClient         *tmprl_mocks.Client
	Authclient             *auth_client.MockInterface
	Authmanagerclient      *authmgmt.MockInterface
	Prometheusclient       *promapiv1mock.MockAPI
	Billingclient          *billing.MockInterface
	Presidio               Presidiomocks
	Slackclient            *ee_slack.MockInterface
}

type Presidiomocks struct {
	Analyzer   *presidioapi.MockAnalyzeInterface
	Anonymizer *presidioapi.MockAnonymizeInterface
	Entities   *presidioapi.MockEntityInterface
}

type HusonymApiTestClient struct {
	HusonymQuerier db_queries.Querier
	systemQuerier  pg_queries.Querier

	Pgcontainer   *tcpostgres.PostgresTestContainer
	migrationsDir string

	httpsrv *httptest.Server

	// OSS, Unauthenticated, Licensed
	OSSUnauthenticatedLicensedClients *HusonymClients
	// OSS, Authenticated, Licensed
	OSSAuthenticatedLicensedClients *HusonymClients
	// OSS, Unauthenticated, Unlicensed
	OSSUnauthenticatedUnlicensedClients *HusonymClients
	// OSS, Unauthenticated, Licensed with small usage caps — for exercising limit
	// enforcement
	OSSUnauthenticatedLimitedClients *HusonymClients
	// NeoCloud, Authenticated, Licensed
	HusonymCloudAuthenticatedLicensedClients *HusonymClients

	Mocks *Mocks
}

// Option is a functional option for configuring Husonym Api Test Client
type Option func(*HusonymApiTestClient)

func NewHusonymApiTestClient(
	ctx context.Context,
	t testing.TB,
	opts ...Option,
) (*HusonymApiTestClient, error) {
	neoApi := &HusonymApiTestClient{
		migrationsDir: "../../../../sql/postgresql/schema",
	}
	for _, opt := range opts {
		opt(neoApi)
	}
	err := neoApi.Setup(ctx, t)
	if err != nil {
		return nil, err
	}
	return neoApi, nil
}

// Sets husonym database migrations directory path
func WithMigrationsDirectory(directoryPath string) Option {
	return func(a *HusonymApiTestClient) {
		a.migrationsDir = directoryPath
	}
}

func (s *HusonymApiTestClient) Setup(ctx context.Context, t testing.TB) error {
	pgcontainer, err := tcpostgres.NewPostgresTestContainer(ctx)
	if err != nil {
		return err
	}

	s.Pgcontainer = pgcontainer
	s.HusonymQuerier = db_queries.New()
	s.systemQuerier = pg_queries.New()

	s.Mocks = &Mocks{
		TemporalClientManager:  clientmanager.NewMockInterface(t),
		TemporalConfigProvider: clientmanager.NewMockConfigProvider(t),
		TemporalClient:         tmprl_mocks.NewClient(t),
		Authclient:             auth_client.NewMockInterface(t),
		Authmanagerclient:      authmgmt.NewMockInterface(t),
		Prometheusclient:       promapiv1mock.NewMockAPI(t),
		Billingclient:          billing.NewMockInterface(t),
		Presidio: Presidiomocks{
			Analyzer:   presidioapi.NewMockAnalyzeInterface(t),
			Anonymizer: presidioapi.NewMockAnonymizeInterface(t),
			Entities:   presidioapi.NewMockEntityInterface(t),
		},
		Slackclient: ee_slack.NewMockInterface(t),
	}

	err = s.InitializeTest(ctx, t)
	if err != nil {
		return err
	}

	rootmux := http.NewServeMux()

	logger := testutil.GetConcurrentTestLogger(t)

	ossUnauthLicensedMux, err := s.setupOssUnauthenticatedLicensedMux(ctx, pgcontainer, logger)
	if err != nil {
		return fmt.Errorf("unable to setup oss unauthenticated licensed mux: %w", err)
	}
	rootmux.Handle(
		openSourceUnauthenticatedLicensedPostfix+"/",
		http.StripPrefix(openSourceUnauthenticatedLicensedPostfix, ossUnauthLicensedMux),
	)

	ossAuthLicensedMux, err := s.setupOssLicensedAuthMux(ctx, pgcontainer, logger)
	if err != nil {
		return fmt.Errorf("unable to setup oss authenticated licensed mux: %w", err)
	}
	rootmux.Handle(
		openSourceAuthenticatedLicensedPostfix+"/",
		http.StripPrefix(openSourceAuthenticatedLicensedPostfix, ossAuthLicensedMux),
	)

	ossUnauthUnlicensedMux, err := s.setupOssUnlicensedMux(pgcontainer, logger)
	if err != nil {
		return fmt.Errorf("unable to setup oss unauthenticated unlicensed mux: %w", err)
	}
	rootmux.Handle(
		openSourceUnauthenticatedUnlicensedPostfix+"/",
		http.StripPrefix(openSourceUnauthenticatedUnlicensedPostfix, ossUnauthUnlicensedMux),
	)

	ossLimitedMux, err := s.setupOssLimitedMux(pgcontainer, logger)
	if err != nil {
		return fmt.Errorf("unable to setup oss unauthenticated limited mux: %w", err)
	}
	rootmux.Handle(
		openSourceUnauthenticatedLimitedPostfix+"/",
		http.StripPrefix(openSourceUnauthenticatedLimitedPostfix, ossLimitedMux),
	)

	neoCloudAuthdMux, err := s.setupNeoCloudMux(ctx, pgcontainer, logger)
	if err != nil {
		return fmt.Errorf("unable to setup neo cloud authenticated mux: %w", err)
	}
	rootmux.Handle(
		neoCloudAuthenticatedLicensedPostfix+"/",
		http.StripPrefix(neoCloudAuthenticatedLicensedPostfix, neoCloudAuthdMux),
	)

	s.httpsrv = startHTTPServer(t, rootmux)
	rootmux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("404 for URL: %s\n", r.URL.Path)
		http.NotFound(w, r)
	})

	s.OSSUnauthenticatedLicensedClients = newHusonymClients(
		s.httpsrv.URL + openSourceUnauthenticatedLicensedPostfix,
	)
	s.OSSAuthenticatedLicensedClients = newHusonymClients(
		s.httpsrv.URL + openSourceAuthenticatedLicensedPostfix,
	)
	s.OSSUnauthenticatedUnlicensedClients = newHusonymClients(
		s.httpsrv.URL + openSourceUnauthenticatedUnlicensedPostfix,
	)
	s.OSSUnauthenticatedLimitedClients = newHusonymClients(
		s.httpsrv.URL + openSourceUnauthenticatedLimitedPostfix,
	)
	s.HusonymCloudAuthenticatedLicensedClients = newHusonymClients(
		s.httpsrv.URL + neoCloudAuthenticatedLicensedPostfix,
	)

	return nil
}

func (s *HusonymApiTestClient) MockTemporalForCreateJob(returnId string) {
	s.Mocks.TemporalClientManager.
		On(
			"DoesAccountHaveNamespace", mock.Anything, mock.Anything, mock.Anything,
		).
		Return(true, nil).
		Once()
	s.Mocks.TemporalClientManager.
		On(
			"GetSyncJobTaskQueue", mock.Anything, mock.Anything, mock.Anything,
		).
		Return("sync-job", nil).
		Once()
	s.Mocks.TemporalClientManager.
		On(
			"CreateSchedule", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		).
		Return(returnId, nil).
		Once()
}

// Used for any API call that uses GetJobRun() as this mocks the response from Temporal for that execution
func (s *HusonymApiTestClient) MockTemporalForDescribeWorkflowExecution(
	accountId, jobId, jobRunId, workflowName string,
) {
	s.Mocks.TemporalClientManager.EXPECT().
		DescribeWorklowExecution(mock.Anything, accountId, jobRunId, mock.Anything).
		Return(&workflowservice.DescribeWorkflowExecutionResponse{
			WorkflowExecutionInfo: &workflow.WorkflowExecutionInfo{
				Execution: &common.WorkflowExecution{
					WorkflowId: jobRunId,
				},
				CloseTime: timestamppb.New(time.Now()),
				StartTime: timestamppb.New(time.Now().Add(-time.Minute)),
				Status:    enums.WORKFLOW_EXECUTION_STATUS_COMPLETED,
				Type: &common.WorkflowType{
					Name: workflowName,
				},
				SearchAttributes: &common.SearchAttributes{
					IndexedFields: map[string]*common.Payload{
						"TemporalScheduledById": {
							Data: []byte(jobId),
							Metadata: map[string][]byte{
								"jobId": []byte(jobId),
							}, // this doesnt seem to work as it's not the correct format for what temporal expects
						},
					},
				},
			},
		}, nil).
		Once()
}
func (s *HusonymApiTestClient) InitializeTest(ctx context.Context, t testing.TB) error {
	err := neomigrate.Up(ctx, s.Pgcontainer.URL, s.migrationsDir, testutil.GetTestLogger(t))
	if err != nil {
		return err
	}
	return nil
}

func (s *HusonymApiTestClient) CleanupTest(ctx context.Context) error {
	// Dropping here because 1) more efficient and 2) we have a bad down migration
	// _jobs-connection-id-null.down that breaks due to having a null connection_id column.
	// we should do something about that at some point. Running this single drop is easier though
	_, err := s.Pgcontainer.DB.Exec(ctx, "DROP SCHEMA IF EXISTS husonym_api CASCADE")
	if err != nil {
		return err
	}
	_, err = s.Pgcontainer.DB.Exec(ctx, "DROP TABLE IF EXISTS public.schema_migrations")
	if err != nil {
		return err
	}
	return nil
}

func (s *HusonymApiTestClient) TearDown(ctx context.Context) error {
	if s.Pgcontainer != nil {
		_, err := s.Pgcontainer.DB.Exec(ctx, "DROP SCHEMA IF EXISTS husonym_api CASCADE")
		if err != nil {
			return err
		}
		_, err = s.Pgcontainer.DB.Exec(ctx, "DROP TABLE IF EXISTS public.schema_migrations")
		if err != nil {
			return err
		}
		if s.Pgcontainer.DB != nil {
			s.Pgcontainer.DB.Close()
		}
		if s.Pgcontainer.TestContainer != nil {
			err := s.Pgcontainer.TestContainer.Terminate(ctx)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func startHTTPServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.Start()
	tb.Cleanup(srv.Close)
	return srv
}

func NewTestSqlManagerClient() *sqlmanager.SqlManager {
	return sqlmanager.NewSqlManager(
		sqlmanager.WithConnectionManagerOpts(connectionmanager.WithCloseOnRelease()),
	)
}
