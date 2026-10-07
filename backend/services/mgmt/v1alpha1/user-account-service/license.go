package v1alpha1_useraccountservice

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SetSystemLicense offers a license key to the instance, which takes it when it is newer than
// the one in force.
//
// The license belongs to the instance, not to an account: the account of the request only
// says whose permissions let the caller do this. It is not gated by the license itself, since
// it is how an instance without a valid one gets one.
//
// The caller chooses that account, and everyone administers their own personal account, so
// this check does not single out who may set the license of the instance. That is accepted:
// a key is only taken when its issuer signed it and it is newer than the one in force, so no
// caller can widen the license or bring an older one back. It gets narrower once an instance
// belongs to a single organization.
//
// The key value is a secret of the customer: it never goes into an answer, an error or a log.
func (s *Service) SetSystemLicense(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetSystemLicenseRequest],
) (*connect.Response[mgmtv1alpha1.SetSystemLicenseResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}

	// A key given with an API key was given by no person.
	var userId *pgtype.UUID
	if !user.IsApiKey() {
		id := user.PgId()
		userId = &id
	}

	result, err := s.licenses.Offer(ctx, req.Msg.GetKey(), licensestore.OriginInterface, userId)
	if err != nil {
		return nil, fmt.Errorf("unable to store the license key: %w", err)
	}
	systemLicense, err := s.licenseAfterOffer(ctx, result)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.SetSystemLicenseResponse{License: systemLicense}), nil
}

// licenseAfterOffer turns what the store decided about an offered key into the answer: an
// error for a refusal, the description of the license otherwise.
func (s *Service) licenseAfterOffer(
	ctx context.Context,
	result *licensestore.Result,
) (*mgmtv1alpha1.SystemLicense, error) {
	switch result.Outcome {
	case licensestore.RefusedInvalid:
		return nil, husonymerrors.NewBadRequest(result.Reason)
	case licensestore.RefusedOlder:
		return nil, husonymerrors.NewFailedPrecondition(result.Reason)
	case licensestore.Accepted:
		// The key is in force in this process when the call answers, without waiting for the
		// background refresh. When it cannot be read back the key is stored all the same, so
		// the call does not fail: giving the key again would then be answered as unchanged,
		// against a first answer that said it was not taken.
		if err := s.refreshLicense(ctx); err != nil {
			logger_interceptor.GetLoggerFromContextOrDefault(ctx).ErrorContext(
				ctx,
				"the license key is stored, and is in force on this instance once it is read again",
				"error", err,
			)
		}
	case licensestore.Unchanged:
	}
	return s.systemLicense(ctx), nil
}

// GetSystemLicenseKey returns the license key in force as it was signed, so that the worker
// verifies it itself and enforces what it says.
//
// Only the worker reads it: whoever else holds the value could install it elsewhere, so it is
// guarded as the worker's alone (userdata.WorkerOnly).
func (s *Service) GetSystemLicenseKey(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetSystemLicenseKeyRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemLicenseKeyResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.cfg.WorkerOnly.Allow(user); err != nil {
		return nil, err
	}

	key, err := s.licenses.Current(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read the license key: %w", err)
	}
	return connect.NewResponse(&mgmtv1alpha1.GetSystemLicenseKeyResponse{Key: key}), nil
}

// GetLicenseUsage tells what the instance uses of its license, for an account to see next to
// what the license allows: how many sources the instance counts, which of them are the
// account's, and the licensed features the account uses.
//
// It is a read, so it asks for no license in force and for no feature: an instance whose
// license lapsed, or lacks what it uses, is the one that most needs to see this. Of the other
// accounts it tells the count of sources and nothing else.
func (s *Service) GetLicenseUsage(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetLicenseUsageRequest],
) (*connect.Response[mgmtv1alpha1.GetLicenseUsageResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(req.Msg.GetAccountId()), rbac.AccountAction_View); err != nil {
		return nil, err
	}

	usage, err := s.licenseusage.Of(ctx, req.Msg.GetAccountId())
	if err != nil {
		return nil, fmt.Errorf("unable to read what the account uses of the license: %w", err)
	}

	dto := &mgmtv1alpha1.GetLicenseUsageResponse{
		// A count beyond what the message holds is as good as the largest one it holds.
		SourcesInInstance: int32(max(min(usage.SourcesInInstance, math.MaxInt32), 0)),
	}
	for _, source := range usage.SourcesInAccount {
		dto.SourcesInAccount = append(dto.SourcesInAccount, &mgmtv1alpha1.LicenseSource{
			ConnectionId:   source.ConnectionId,
			ConnectionName: source.ConnectionName,
			Database:       source.Database,
		})
	}
	for _, feature := range usage.FeaturesInUse {
		dto.FeaturesInUse = append(dto.FeaturesInUse, string(feature))
	}
	return connect.NewResponse(dto), nil
}

// systemLicense describes the license of the instance, without the key value.
//
// Everything comes from one description of what the process holds, so that the answer never
// mixes two keys or two instants. It never fails: the system information that carries it is
// asked by every page, license or not.
func (s *Service) systemLicense(ctx context.Context) *mgmtv1alpha1.SystemLicense {
	desc := s.licensedescriber.Describe()
	dto := &mgmtv1alpha1.SystemLicense{
		IsValid:        desc.InForce(),
		IsHusonymCloud: false,
		State:          string(desc.State),
	}
	if desc.Problem != nil {
		problem := desc.Problem.Error()
		// What failed while loading may name a host, a user or a database, and this is told
		// to callers that hold no permission: they get the fact, the log has the detail.
		if errors.Is(desc.Problem, license.ErrKeyNotLoaded) {
			problem = license.ErrKeyNotLoaded.Error()
		}
		dto.Problem = &problem
	}

	key := desc.Key
	if key == nil {
		// Without a key the expiry is the present instant, as it always was.
		dto.ExpiresAt = timestamppb.Now()
		return dto
	}
	dto.ExpiresAt = timestamppb.New(key.ExpiresAt)
	dto.Plan = key.Plan
	dto.IssuedTo = key.IssuedTo
	dto.Telemetry = string(key.TelemetryMode())
	dto.GraceEndsAt = timestamppb.New(key.GraceEndsAt())
	dto.Limits = toLicenseLimitsDto(key.Limits)
	// What the key says, whether or not it is in force: is_valid tells that.
	dto.AllFeatures = key.AllowsEveryFeature()
	for _, feature := range license.AllFeatures() {
		if key.HasFeature(feature) {
			dto.Features = append(dto.Features, string(feature))
		}
	}

	if installation := s.installationOf(ctx, key.Id); installation != nil {
		dto.Origin = string(installation.Origin)
		dto.InstalledAt = timestamppb.New(installation.At)
	}
	return dto
}

// installationMemory remembers how the key of one license was stored. It is safe for
// concurrent use.
type installationMemory struct {
	mu sync.Mutex
	// known is false until an answer of the store was remembered.
	known     bool
	licenseId string
	// installation is nil when the store holds no key of that license.
	installation *licensestore.Installation
}

// installationOf tells how the key of the license with this id was stored, or nothing when
// that is not known.
//
// The store is asked once per key the process holds, not once per call: the answer is
// remembered for as long as the id stays the same. A store that does not answer is logged and
// leaves the answer unknown for this call; the next call asks again.
func (s *Service) installationOf(ctx context.Context, licenseId string) *licensestore.Installation {
	memory := &s.installations
	memory.mu.Lock()
	if memory.known && memory.licenseId == licenseId {
		defer memory.mu.Unlock()
		return memory.installation
	}
	memory.mu.Unlock()

	// Asked outside the lock: calls that arrive together each ask, and none waits behind a
	// slow database for an answer it could do without.
	installation, err := s.licenses.Installation(ctx, licenseId)
	if err != nil {
		logger_interceptor.GetLoggerFromContextOrDefault(ctx).ErrorContext(
			ctx,
			"unable to read how the license key was stored",
			"licenseId", licenseId, "error", err,
		)
		return nil
	}

	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.known, memory.licenseId, memory.installation = true, licenseId, installation
	return installation
}

func toLicenseLimitsDto(limits *license.Limits) *mgmtv1alpha1.LicenseLimits {
	if limits == nil {
		return nil
	}
	return &mgmtv1alpha1.LicenseLimits{
		MaxSources:             toLimitDto(limits.MaxSources),
		MaxJobs:                toLimitDto(limits.MaxJobs),
		MaxConnections:         toLimitDto(limits.MaxConnections),
		AllowedConnectionTypes: limits.AllowedConnectionTypes,
	}
}

// toLimitDto keeps an absent cap absent. A cap beyond what the message holds is as good as the
// largest one it holds.
func toLimitDto(limit *int) *int32 {
	if limit == nil {
		return nil
	}
	capped := int32(max(min(*limit, math.MaxInt32), math.MinInt32))
	return &capped
}
