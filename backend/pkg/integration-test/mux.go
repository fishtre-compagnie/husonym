package integrationtests_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/fishtre-compagnie/husonym/internal/cloudidentity"

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
	v1alpha1_accountsettingservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/account-settings-service"
	v1alpha_anonymizationservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/anonymization-service"
	v1alpha1_connectiondataservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/connection-data-service"
	v1alpha1_connectionservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/connection-service"
	v1alpha1_jobservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/job-service"
	v1alpha1_transformersservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/transformers-service"
	v1alpha1_useraccountservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/user-account-service"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	awsmanager "github.com/fishtre-compagnie/husonym/internal/aws"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	presidioapi "github.com/fishtre-compagnie/husonym/internal/ee/presidio"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac/enforcer"
	sym_encrypt "github.com/fishtre-compagnie/husonym/internal/encrypt/sym"
	husonym_gcp "github.com/fishtre-compagnie/husonym/internal/gcp"
	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/jackc/pgx/v5/stdlib"
)

var (
	// TestIssuer is the issuer the fake tokens below carry, and what the services are
	// configured to expect. A token with no issuer is refused now, as a real one would
	// be: an identity is a pair, and the tests have to exercise the real shape.
	TestIssuer = "https://idp.test.husonym.dev/"

	validAuthUser = &authmgmt.User{Name: "foo", Email: "bar", EmailVerified: true, Picture: "baz"}

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
				AuthIssuer: TestIssuer,
				Claims: &auth_jwt.CustomClaims{
					Email:         &validAuthUser.Email,
					EmailVerified: utils.LenientBool(validAuthUser.EmailVerified),
				},
			}), nil
		},
	)
)

const (
	// OSS, Unauthenticated, Licensed
	openSourceUnauthenticatedLicensedPostfix = "/oss-unauthenticated-licensed"
	// OSS, Authenticated, Licensed
	openSourceAuthenticatedLicensedPostfix = "/oss-authenticated-licensed"
	// OSS, Authenticated, Licensed with a license of its own that a test can make invalid
	openSourceAuthenticatedExpiringPostfix = "/oss-authenticated-expiring"
	// OSS, Unauthenticated, Unlicensed
	openSourceUnauthenticatedUnlicensedPostfix = "/oss-unauthenticated-unlicensed"
	// OSS, Unauthenticated, Licensed with usage caps deliberately small enough for a test
	// to reach them
	openSourceUnauthenticatedLimitedPostfix = "/oss-unauthenticated-limited"
)

func (s *HusonymApiTestClient) setupOssUnauthenticatedLicensedMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isAuthEnabled := false
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		enforcedRbacClient,
		logger,
		testutil.NewFakeEELicense(testutil.WithIsValid()),
	)
}

func (s *HusonymApiTestClient) setupOssLicensedAuthMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isAuthEnabled := true
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		enforcedRbacClient,
		logger,
		testutil.NewFakeEELicense(testutil.WithIsValid()),
	)
}

// Licensed and authenticated like setupOssLicensedAuthMux, but with a license of its own
// so a test can make it invalid without touching the other modes. It starts valid.
func (s *HusonymApiTestClient) setupOssExpiringAuthMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isAuthEnabled := true
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	s.Mocks.ExpiringLicense = testutil.NewFakeEELicense(testutil.WithIsValid())
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		enforcedRbacClient,
		logger,
		s.Mocks.ExpiringLicense,
	)
}

func (s *HusonymApiTestClient) setupOssUnlicensedMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isAuthEnabled := false
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		enforcedRbacClient,
		logger,
		testutil.NewFakeEELicense(),
	)
}

// Licensed, with usage caps small enough that a test can actually reach them: one job,
// two connections (the minimum a job needs, so the job cap stays reachable), and postgres
// only.
func (s *HusonymApiTestClient) setupOssLimitedMux(
	ctx context.Context,
	pgcontainer *tcpostgres.PostgresTestContainer,
	logger *slog.Logger,
) (*http.ServeMux, error) {
	isAuthEnabled := false
	maxJobs := 1
	maxConnections := 2
	enforcedRbacClient, err := s.getEnforcedRbacClient(ctx, pgcontainer)
	if err != nil {
		return nil, fmt.Errorf("unable to get enforced rbac client: %w", err)
	}
	return s.setupMux(
		pgcontainer,
		isAuthEnabled,
		enforcedRbacClient,
		logger,
		testutil.NewFakeEELicense(
			testutil.WithIsValid(),
			testutil.WithLimits(&license.Limits{
				MaxJobs:                &maxJobs,
				MaxConnections:         &maxConnections,
				AllowedConnectionTypes: []string{"postgres"},
			}),
		),
	)
}

func (s *HusonymApiTestClient) setupMux(
	pgcontainer *tcpostgres.PostgresTestContainer,
	isAuthEnabled bool,
	rbacClient rbac.Interface,
	logger *slog.Logger,
	// The license every service of this mux reads. Each mode has its own, so that a test
	// can change one without touching the others.
	eelicense *testutil.FakeEELicense,
) (*http.ServeMux, error) {
	// Presidio is wired the same way in every mode: the license is read per request.
	isPresidioEnabled := true

	maxAllowed := int64(10000)

	husonymDb := husonymdb.New(pgcontainer.DB, db_queries.New())

	userService := v1alpha1_useraccountservice.New(
		&v1alpha1_useraccountservice.Config{
			IsAuthEnabled:            isAuthEnabled,
			DeploymentIssuer:         TestIssuer,
			DefaultMaxAllowedRecords: &maxAllowed,
		},
		husonymdb.New(pgcontainer.DB, db_queries.New()),
		s.Mocks.TemporalConfigProvider,
		s.Mocks.Authclient,
		s.Mocks.Authmanagerclient,
		rbacClient, // rbac client
		eelicense,
	)
	userclient := userdata.NewClient(userService, rbacClient, eelicense)

	transformerService := v1alpha1_transformersservice.New(
		&v1alpha1_transformersservice.Config{
			IsPresidioEnabled: isPresidioEnabled,
		},
		husonymdb.New(pgcontainer.DB, db_queries.New()),
		s.Mocks.Presidio.Entities,
		userclient,
		eelicense,
	)

	sqlmanagerclient := NewTestSqlManagerClient()

	connectionService := v1alpha1_connectionservice.New(
		&v1alpha1_connectionservice.Config{},
		husonymDb,
		userclient,
		mongoconnect.NewConnector(),
		awsmanager.New(cloudidentity.Policy{}),
		sqlmanagerclient,
		&sqlconnect.SqlOpenConnector{},
	)

	jobhookService := jobhooks.New(husonymDb, userclient)

	awsManager := awsmanager.New(cloudidentity.Policy{})
	sqlConnector := &sqlconnect.SqlOpenConnector{}
	pgquerier := pg_queries.New()
	mysqlquerier := mysql_queries.New()
	mongoconnector := mongoconnect.NewConnector()
	sqlmanager := sqlmanagerclient
	gcpmanager := husonym_gcp.NewManager(cloudidentity.Policy{})
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
		&v1alpha1_jobservice.Config{
			IsAuthEnabled: isAuthEnabled,
			WorkerOnly:    userdata.WorkerOnly{IsAuthEnabled: isAuthEnabled},
		},
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
		},
		nil, // meter
		userclient,
		userService,
		transformerService,
		presAnalyzeClient,
		presAnonClient,
		husonymDb,
		eelicense,
	)

	connectionDataService := v1alpha1_connectiondataservice.New(
		&v1alpha1_connectiondataservice.Config{},
		connectionService,
		connectiondatabuilder,
		// Pas d'analyseur Presidio dans les tests d'intégration : IsPresidioEnabled
		// reste faux, le scan de contenu répond donc FailedPrecondition.
		nil,
		v1alpha1_connectiondataservice.Transformers{Client: transformerService},
	)

	accountHookService := v1alpha1_accounthookservice.New(
		accounthooks.New(
			husonymDb,
			userclient,
			accounthooks.WithSlackClient(s.Mocks.Slackclient),
		),
	)

	// The settings of an account, with a password of their own: the tests exercise the
	// service the way a deployment that can keep a secret runs it.
	settingsEncryptor, err := sym_encrypt.NewEncryptor("husonym-integration-tests-encryption-password")
	if err != nil {
		return nil, err
	}
	accountSettingService := v1alpha1_accountsettingservice.New(
		&v1alpha1_accountsettingservice.Config{
			WorkerOnly: userdata.WorkerOnly{IsAuthEnabled: isAuthEnabled},
		},
		husonymDb,
		userclient,
		settingsEncryptor,
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
	mux.Handle(mgmtv1alpha1connect.NewAccountSettingServiceHandler(
		accountSettingService,
		connect.WithInterceptors(interceptors...),
	))

	mux.Handle(mgmtv1alpha1connect.NewAccountHookServiceHandler(
		accountHookService,
		connect.WithInterceptors(interceptors...),
	))

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
	err = rbacenforcer.LoadPolicy()
	if err != nil {
		return nil, fmt.Errorf("unable to load rbac policies: %w", err)
	}
	return rbac.New(rbacenforcer), nil
}
