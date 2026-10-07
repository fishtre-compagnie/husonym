package v1alpha1_useraccountservice

import (
	"context"

	auth_client "github.com/fishtre-compagnie/husonym/backend/internal/auth/client"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensegate"
	"github.com/fishtre-compagnie/husonym/backend/internal/licenserefusal"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/jackc/pgx/v5/pgtype"
)

type Service struct {
	cfg                    *Config
	db                     *husonymdb.HusonymDb
	temporalConfigProvider clientmanager.ConfigProvider
	authclient             auth_client.Interface
	authadminclient        authmgmt.Interface
	rbacClient             rbac.Interface
	licenseclient          license.EEInterface
	licensedescriber       LicenseDescriber
	licenses               LicenseStore
	refreshLicense         func(ctx context.Context) error
	installations          installationMemory
	jobgate                JobGate
	licenseusage           LicenseUsage
	// refusals counts the refusals that are answered as a status and not as an error.
	refusals licenserefusal.Counter
}

// JobGate tells whether a job of an account may start under the license.
// *licensegate.JobGate is one.
type JobGate interface {
	CheckStored(ctx context.Context, accountId, jobId string) error
}

// LicenseUsage tells what an account uses of the license of the instance, whatever the license
// allows. *licensegate.UsageReader is one.
type LicenseUsage interface {
	Of(ctx context.Context, accountId string) (*licensegate.Usage, error)
}

// LicenseDescriber tells what the process holds as its license at one instant. The provider
// of the process is one.
type LicenseDescriber interface {
	Describe() license.Description
}

// LicenseStore is what the service asks of the place the instance keeps its license keys in.
// *licensestore.Store is one.
type LicenseStore interface {
	Offer(
		ctx context.Context,
		value string,
		origin licensestore.Origin,
		userId *pgtype.UUID,
	) (*licensestore.Result, error)
	Current(ctx context.Context) (string, error)
	Installation(ctx context.Context, licenseId string) (*licensestore.Installation, error)
	// DoorProblem tells why the key of the variable or of the file was last refused as invalid,
	// or nothing.
	DoorProblem() string
}

type Config struct {
	IsAuthEnabled            bool
	DefaultMaxAllowedRecords *int64

	// WorkerOnly guards the license key as it was signed, which the worker alone reads.
	WorkerOnly userdata.WorkerOnly

	// DeploymentIssuer is the issuer the deployment is configured with
	// (AUTH_EXPECTED_ISS, falling back to AUTH_BASEURL). It is the only issuer allowed
	// to take over an identity recorded before issuers were, and the only one an
	// invitation created before issuers were may be accepted from -- see
	// husonymdb.Identity.
	//
	// Empty when the deployment has no issuer at all, which is the unauthenticated mode.
	DeploymentIssuer string
}

func New(
	cfg *Config,
	db *husonymdb.HusonymDb,
	temporalConfigProvider clientmanager.ConfigProvider,
	authclient auth_client.Interface,
	authadminclient authmgmt.Interface,
	rbacClient rbac.Interface,
	licenseclient license.EEInterface,
	licensedescriber LicenseDescriber,
	licenses LicenseStore,
	// refreshLicense makes the process read its license key again, so that a key that was
	// just stored is in force when the call that stored it answers.
	refreshLicense func(ctx context.Context) error,
	jobgate JobGate,
	licenseusage LicenseUsage,
	refusals licenserefusal.Counter,
) *Service {
	return &Service{
		cfg:                    cfg,
		db:                     db,
		temporalConfigProvider: temporalConfigProvider,
		authclient:             authclient,
		authadminclient:        authadminclient,
		rbacClient:             rbacClient,
		licenseclient:          licenseclient,
		licensedescriber:       licensedescriber,
		licenses:               licenses,
		refreshLicense:         refreshLicense,
		jobgate:                jobgate,
		licenseusage:           licenseusage,
		refusals:               refusals,
	}
}

func (s *Service) UserDataClient() userdata.Interface {
	return userdata.NewClient(s, s.rbacClient, s.licenseclient)
}
