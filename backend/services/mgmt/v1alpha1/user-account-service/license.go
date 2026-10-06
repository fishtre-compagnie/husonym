package v1alpha1_useraccountservice

import (
	"context"
	"fmt"
	"math"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
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
	switch result.Outcome {
	case licensestore.RefusedInvalid:
		return nil, husonymerrors.NewBadRequest(result.Reason)
	case licensestore.RefusedOlder:
		return nil, husonymerrors.NewFailedPrecondition(result.Reason)
	case licensestore.Accepted:
		// The key is in force in this process when the call answers, without waiting for the
		// background refresh.
		if err := s.refreshLicense(ctx); err != nil {
			return nil, fmt.Errorf("the license key was stored but could not be read back: %w", err)
		}
	case licensestore.Unchanged:
	}

	systemLicense, err := s.systemLicense(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.SetSystemLicenseResponse{License: systemLicense}), nil
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

// systemLicense describes the license of the instance, without the key value.
func (s *Service) systemLicense(ctx context.Context) (*mgmtv1alpha1.SystemLicense, error) {
	desc := s.licensedescriber.Describe()
	dto := &mgmtv1alpha1.SystemLicense{
		IsValid:        s.licenseclient.IsValid(),
		ExpiresAt:      timestamppb.New(s.licenseclient.ExpiresAt()),
		IsHusonymCloud: false,
		State:          string(desc.State),
	}
	if desc.Problem != nil {
		problem := desc.Problem.Error()
		dto.Problem = &problem
	}

	key := desc.Key
	if key == nil {
		return dto, nil
	}
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

	// These two come from the database while the rest comes from what this process holds. A
	// replica that has not refreshed since a key was stored elsewhere tells, for up to a
	// minute, where and when the newer key arrived next to the description of the older one.
	installation, err := s.licenses.Installation(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read how the license key was stored: %w", err)
	}
	if installation != nil {
		dto.Origin = string(installation.Origin)
		dto.InstalledAt = timestamppb.New(installation.At)
	}
	return dto, nil
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
