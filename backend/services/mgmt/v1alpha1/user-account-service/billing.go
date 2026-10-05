package v1alpha1_useraccountservice

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/dtomaps"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/jackc/pgx/v5/pgtype"
)

// There is no billing in a self-hosted deployment: the license decides whether jobs may run.
func (s *Service) GetAccountStatus(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountStatusRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountStatusResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceAccount(
		ctx,
		userdata.NewIdentifier(req.Msg.GetAccountId()),
		rbac.AccountAction_View,
	)
	if err != nil {
		return nil, err
	}

	if _, err := husonymdb.ToUuid(req.Msg.GetAccountId()); err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.GetAccountStatusResponse{}), nil
}

func (s *Service) IsAccountStatusValid(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.IsAccountStatusValidRequest],
) (*connect.Response[mgmtv1alpha1.IsAccountStatusValidResponse], error) {
	_, err := s.GetAccountStatus(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetAccountStatusRequest{
			AccountId: req.Msg.GetAccountId(),
		}),
	)
	if err != nil {
		return nil, err
	}

	// Self-hosted: there is no billing to consult, so the license decides.
	//
	// This is the choke point for scheduled work, and the reason the check belongs
	// here rather than only in CreateJobRun. Temporal triggers scheduled workflows
	// directly, never passing through the API, but the datasync workflow calls
	// CheckAccountStatus before doing anything and aborts when this returns false.
	// Gating here therefore freezes manual and scheduled runs alike.
	//
	// IsValid() spans the grace period, so this only bites once grace is over.
	if s.licenseclient != nil && !s.licenseclient.IsValid() {
		reason := "License has expired. Renew it to resume running jobs; existing configuration and run history remain available."
		return connect.NewResponse(&mgmtv1alpha1.IsAccountStatusValidResponse{
			IsValid:       false,
			AccountStatus: mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_ACCOUNT_IN_EXPIRED_STATE,
			Reason:        &reason,
		}), nil
	}
	return connect.NewResponse(&mgmtv1alpha1.IsAccountStatusValidResponse{IsValid: true}), nil
}

func (s *Service) GetAccountBillingCheckoutSession(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountBillingCheckoutSessionRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountBillingCheckoutSessionResponse], error) {
	return nil, husonymerrors.NewNotImplemented(
		fmt.Sprintf(
			"%s is not implemented",
			strings.TrimPrefix(
				mgmtv1alpha1connect.UserAccountServiceGetAccountBillingCheckoutSessionProcedure,
				"/",
			),
		),
	)
}

func (s *Service) GetAccountBillingPortalSession(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountBillingPortalSessionRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountBillingPortalSessionResponse], error) {
	return nil, husonymerrors.NewNotImplemented(
		fmt.Sprintf(
			"%s is not implemented",
			strings.TrimPrefix(
				mgmtv1alpha1connect.UserAccountServiceGetAccountBillingPortalSessionProcedure,
				"/",
			),
		),
	)
}

func (s *Service) GetBillingAccounts(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetBillingAccountsRequest],
) (*connect.Response[mgmtv1alpha1.GetBillingAccountsResponse], error) {
	userdataclient := s.UserDataClient()
	if _, err := userdataclient.GetUser(ctx); err != nil {
		return nil, err
	}

	accountIdsToFilter := []pgtype.UUID{}
	for _, accountId := range req.Msg.GetAccountIds() {
		accountUuid, err := husonymdb.ToUuid(accountId)
		if err != nil {
			return nil, fmt.Errorf("input did not contain entirely valid uuids: %w", err)
		}
		accountIdsToFilter = append(accountIdsToFilter, accountUuid)
	}

	accounts, err := s.db.Q.GetBilledAccounts(ctx, s.db.Db, accountIdsToFilter)
	if err != nil {
		return nil, err
	}

	dtos := make([]*mgmtv1alpha1.UserAccount, 0, len(accounts))
	for idx := range accounts {
		account := accounts[idx]
		dtos = append(dtos, dtomaps.ToUserAccount(&account))
	}
	return connect.NewResponse(&mgmtv1alpha1.GetBillingAccountsResponse{Accounts: dtos}), nil
}

func (s *Service) SetBillingMeterEvent(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetBillingMeterEventRequest],
) (*connect.Response[mgmtv1alpha1.SetBillingMeterEventResponse], error) {
	return nil, husonymerrors.NewUnauthorized("billing is not currently enabled")
}
