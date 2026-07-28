package v1alpha1_useraccountservice

import (
	"context"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

func (s *Service) GetAccountOnboardingConfig(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountOnboardingConfigRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountOnboardingConfigResponse], error) {
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

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	oc, err := s.db.Q.GetAccountOnboardingConfig(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.GetAccountOnboardingConfigResponse{
		Config: oc.ToDto(),
	}), nil
}

func (s *Service) SetAccountOnboardingConfig(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetAccountOnboardingConfigRequest],
) (*connect.Response[mgmtv1alpha1.SetAccountOnboardingConfigResponse], error) {
	userdataclient := s.UserDataClient()
	user, err := userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceAccount(
		ctx,
		userdata.NewIdentifier(req.Msg.GetAccountId()),
		rbac.AccountAction_Edit,
	)
	if err != nil {
		return nil, err
	}

	accountUuid, err := husonymdb.ToUuid(req.Msg.GetAccountId())
	if err != nil {
		return nil, err
	}

	inputCfg := req.Msg.GetConfig()
	if inputCfg == nil {
		inputCfg = &mgmtv1alpha1.AccountOnboardingConfig{}
	}

	onboardingConfigModel := &pg_models.AccountOnboardingConfig{}
	onboardingConfigModel.FromDto(inputCfg)

	account, err := s.db.Q.UpdateAccountOnboardingConfig(
		ctx,
		s.db.Db,
		db_queries.UpdateAccountOnboardingConfigParams{
			OnboardingConfig: onboardingConfigModel,
			AccountId:        accountUuid,
		},
	)
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&mgmtv1alpha1.SetAccountOnboardingConfigResponse{
		Config: account.OnboardingConfig.ToDto(),
	}), nil
}
