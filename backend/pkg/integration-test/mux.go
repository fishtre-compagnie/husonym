package integrationtests_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	auth_jwt "github.com/fishtre-compagnie/husonym/backend/internal/auth/jwt"
	auth_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/auth"
	accounthooks "github.com/fishtre-compagnie/husonym/backend/internal/ee/hooks/accounts"
	jobhooks "github.com/fishtre-compagnie/husonym/backend/internal/ee/hooks/jobs"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/utils"
	"github.com/fishtre-compagnie/husonym/backend/pkg/mongoconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	v1alpha1_accounthookservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/account-hooks-service"
	v1alpha_anonymizationservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/anonymization-service"
	v1alpha1_connectiondataservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/connection-data-service"
	v1alpha1_connectionservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/connection-service"
	v1alpha1_jobservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/job-service"
	v1alpha1_transformersservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/transformers-service"
	v1alpha1_useraccountservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/user-account-service"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	awsmanager "github.com/fishtre-compagnie/husonym/internal/aws"
	"github.com/fishtre-compagnie/husonym/internal/billing"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac/enforcer"
	husonym_gcp "github.com/fishtre-compagnie/husonym/internal/gcp"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/stdlib"
)

var (
	validAuthUser = &authmgmt.User{Name: "foo", Email: "bar", Picture: "baz"}

	authinterceptor = auth_interceptor.NewInterceptor(
		func(ctx context.Context, header http.Header, spec connect.Spec) (context.Context, error) {
			// will need to further fill this out as the tests grow
			authuserid, err := utils.GetBearerTokenFromHeader(header, "Authorization")
			if err != nil {
				return nil, err
			}
			if apikey.IsValidV1WorkerKey(authuserid) {
				return auth_apikey.SetTokenData(ctx, &auth_apikey.TokenContextData{
					RawToken:   authuserid,
					ApiKey:     nil,
					ApiKeyType: apikey.WorkerApiKey,
				}), nil
			}
			return auth_jwt.SetTokenData(ctx, &auth_jwt.TokenContextData{
				AuthUserId: authuserid,
				Claims:     &auth_jwt.CustomClaims{Email: &validAuthUser.Email},
			}), nil
		},
	)
)

const (
	// OSS, Unauthenticated, Licensed
	openSourceUnauthenticatedLicensedPostfix = "/oss-unauthenticated-licensed"
	// OSS, Authenticated, Licensed
	openSourceAuthenticatedLicensedPostfix = "/oss-authenticated-licensed"
	// OSS, Unauthenticated, Unlicensed
	openSourceUnauthenticatedUnlicensedPostfix = "/oss-unauthenticated-unlicensed"
	// NeoCloud, Licensed, Authenticated
	neoCloudAuthenticatedLicensedPostfix = "/husonymcloud-authenticated"
)

func (s *HusonymApiTestClient) setupOssUnauthenticatedLicensedMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isLicensed := true
	isAuthEnabled := false
	isHusonymCloud := false
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		isLicensed,
		isHusonymCloud,
		enforcedRbacClient,
		logger,
	)
}

func (s *HusonymApiTestClient) setupOssLicensedAuthMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isLicensed := true
	isAuthEnabled := true
	isHusonymCloud := false
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		isLicensed,
		isHusonymCloud,
		enforcedRbacClient,
		logger,
	)
}

func (s *HusonymApiTestClient) setupOssUnlicensedMux(
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isLicensed := false
	isAuthEnabled := false
	isHusonymCloud := false
	permissiveRbacClient := rbac.NewAllowAllClient()
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		isLicensed,
		isHusonymCloud,
		permissiveRbacClient,
		logger,
	)
}

func (s *HusonymApiTestClient) setupNeoCloudMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isLicensed := true
	isAuthEnabled := true
	isHusonymCloud := true
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		isLicensed,
		isHusonymCloud,
		enforcedRbacClient,
		logger,
	)
}

func (s *HusonymApiTestClient) setupMux(
	pgcontainer *tcpostgres.PostgresTestContainer,
	isAuthEnabled bool,
	isLicensed bool,
	isHusonymCloud bool,
	rbacClient rbac.Interface,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isPresidioEnabled := isLicensed || isHusonymCloud

	maxAllowed := int64(10000)
	var license *testutil.FakeEELicense
	if isLicensed {
		license = testutil.NewFakeEELicense(testutil.WithIsValid())
	} else {
		license = testutil.NewFakeEELicense()
	}

	husonymDb := husonymdb.New(pgcontainer.DB, db_queries.New())

	var billingclient billing.Interface
	if isHusonymCloud {
		billingclient = s.Mocks.Billingclient
	} else {
		billingclient = nil
	}

	userService := v1alpha1_useraccountservice.New(
		&v1alpha1_useraccountservice.Config{
			IsAuthEnabled:            isAuthEnabled,
			IsHusonymCloud:           isHusonymCloud,
			DefaultMaxAllowedRecords: &maxAllowed,
		},
		husonymdb.New(pgcontainer.DB, db_queries.New()),
		s.Mocks.TemporalConfigProvider,
		s.Mocks.Authclient,
		s.Mocks.Authmanagerclient,
		billingclient,
		rbacClient, // rbac client
		license,
	)
	userclient := userdata.NewClient(userService, rbacClient, license)

	transformerService := v1alpha1_transformersservice.New(
		&v1alpha1_transformersservice.Config{
			IsPresidioEnabled: isPresidioEnabled,
		},
		husonymdb.New(pgcontainer.DB, db_queries.New()),
		s.Mocks.Presidio.Entities,
		userclient,
		license,
	)

	sqlmanagerclient := NewTestSqlManagerClient()

	connectionService := v1alpha1_connectionservice.New(
		&v1alpha1_connectionservice.Config{IsHusonymCloud: isHusonymCloud},
		husonymDb,
		userclient,
		mongoconnect.NewConnector(),
		awsmanager.New(),
		sqlmanagerclient,
		&sqlconnect.SqlOpenConnector{},
	)

	var jobhookService *jobhooks.Service
	if isLicensed {
		jobhookService = jobhooks.New(
			husonymDb,
			userclient,
			jobhooks.WithEnabled(),
		)
	} else {
		jobhookService = jobhooks.New(
			husonymDb,
			userclient,
		)
	}

	awsManager := awsmanager.New()
	sqlConnector := &sqlconnect.SqlOpenConnector{}
	pgquerier := pg_queries.New()
	mysqlquerier := mysql_queries.New()
	mongoconnector := mongoconnect.NewConnector()
	sqlmanager := sqlmanagerclient
	gcpmanager := husonym_gcp.NewManager()
	husonymtyperegistry := husonymtypes.NewTypeRegistry(logger)

	connectiondatabuilder := connectiondata.NewConnectionDataBuilder(
		sqlConnector,
		sqlmanager,
		pgquerier,
		mysqlquerier,
		awsManager,
		gcpmanager,
		mongoconnector,
		husonymtyperegistry,
	)

	jobService := v1alpha1_jobservice.New(
		&v1alpha1_jobservice.Config{IsAuthEnabled: isAuthEnabled, IsHusonymCloud: isHusonymCloud},
		husonymDb,
		s.Mocks.TemporalClientManager,
		connectionService,
		sqlmanagerclient,
		jobhookService,
		userclient,
		connectiondatabuilder,
	)

	var presAnalyzeClient presidioapi.AnalyzeInterface
	var presAnonClient presidioapi.AnonymizeInterface

	anonymizationService := v1alpha_anonymizationservice.New(
		&v1alpha_anonymizationservice.Config{
			IsPresidioEnabled: isPresidioEnabled,
			IsAuthEnabled:     isAuthEnabled,
			IsHusonymCloud:    isHusonymCloud,
		},
		nil, // meter
		userclient,
		userService,
		transformerService,
		presAnalyzeClient,
		presAnonClient,
		husonymDb,
		license,
	)

	connectionDataService := v1alpha1_connectiondataservice.New(
		&v1alpha1_connectiondataservice.Config{},
		connectionService,
		connectiondatabuilder,
	)

	accountHookService := v1alpha1_accounthookservice.New(
		accounthooks.New(
			husonymDb,
			userclient,
			accounthooks.WithSlackClient(s.Mocks.Slackclient),
		),
	)

	mux := http.NewServeMux()

	interceptors := []connect.Interceptor{}

	if isAuthEnabled {
		interceptors = append(interceptors, authinterceptor)
	}

	mux.Handle(mgmtv1alpha1connect.NewUserAccountServiceHandler(
		userService,
		connect.WithInterceptors(interceptors...),
	))
	mux.Handle(mgmtv1alpha1connect.NewTransformersServiceHandler(
		transformerService,
		connect.WithInterceptors(interceptors...),
	))
	mux.Handle(mgmtv1alpha1connect.NewConnectionServiceHandler(
		connectionService,
		connect.WithInterceptors(interceptors...),
	))
	mux.Handle(mgmtv1alpha1connect.NewJobServiceHandler(
		jobService,
		connect.WithInterceptors(interceptors...),
	))
	mux.Handle(mgmtv1alpha1connect.NewAnonymizationServiceHandler(
		anonymizationService,
		connect.WithInterceptors(interceptors...),
	))
	mux.Handle(mgmtv1alpha1connect.NewConnectionDataServiceHandler(
		connectionDataService,
		connect.WithInterceptors(interceptors...),
	))

	if isLicensed {
		mux.Handle(mgmtv1alpha1connect.NewAccountHookServiceHandler(
			accountHookService,
			connect.WithInterceptors(interceptors...),
		))
	} else {
		mux.Handle(mgmtv1alpha1connect.NewAccountHookServiceHandler(
			mgmtv1alpha1connect.UnimplementedAccountHookServiceHandler{},
			connect.WithInterceptors(interceptors...),
		))
	}

	return mux, nil
}

func (s *HusonymApiTestClient) getEnforcedRbacClient(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
) (rbac.Interface, error) {
	rbacenforcer, err := enforcer.NewActiveEnforcer(
		ctx,
		stdlib.OpenDBFromPool(pgcontainer.DB),
		"husonym_api.casbin_rule",
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create rbac enforcer: %w", err)
	}
	rbacenforcer.EnableAutoSave(true)
	err = rbacenforcer.LoadPolicy()
	if err != nil {
		return nil, fmt.Errorf("unable to load rbac policies: %w", err)
	}
	return rbac.New(rbacenforcer), nil
}
