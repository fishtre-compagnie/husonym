package integrationtests_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	testAuthUserId  = "test-user"
	validAuthUser   = &authmgmt.User{Name: "foo", Email: "bar", Picture: "baz"}
	testAuthUserId2 = "test-user2"
)

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountOnboardingConfig() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountOnboardingConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountOnboardingConfigRequest{AccountId: accountId}))
	requireNoErrResp(s.T(), resp, err)
	onboardingConfig := resp.Msg.GetConfig()
	require.NotNil(s.T(), onboardingConfig)

	require.False(s.T(), onboardingConfig.GetHasCompletedOnboarding())
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountOnboardingConfig_NoAccount() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountOnboardingConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountOnboardingConfigRequest{AccountId: uuid.NewString()}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetAccountOnboardingConfig_NoAccount() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetAccountOnboardingConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountOnboardingConfigRequest{AccountId: uuid.NewString(), Config: &mgmtv1alpha1.AccountOnboardingConfig{}}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetAccountOnboardingConfig_NoConfig() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetAccountOnboardingConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountOnboardingConfigRequest{AccountId: accountId, Config: nil}))
	requireNoErrResp(s.T(), resp, err)
	onboardingConfig := resp.Msg.GetConfig()
	require.NotNil(s.T(), onboardingConfig)

	require.False(s.T(), onboardingConfig.GetHasCompletedOnboarding())
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetAccountOnboardingConfig() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetAccountOnboardingConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountOnboardingConfigRequest{
			AccountId: accountId, Config: &mgmtv1alpha1.AccountOnboardingConfig{
				HasCompletedOnboarding: true,
			}},
		))
	requireNoErrResp(s.T(), resp, err)

	onboardingConfig := resp.Msg.GetConfig()
	require.NotNil(s.T(), onboardingConfig)

	require.True(s.T(), onboardingConfig.GetHasCompletedOnboarding())
}

var (
	validTemporalConfigModel = &pg_models.TemporalConfig{
		Namespace:        "foo",
		SyncJobQueueName: "bar",
		Url:              "http://localhost:7070",
	}
	validTemporalConfig = &clientmanager.TemporalConfig{
		Url:              validTemporalConfigModel.Url,
		Namespace:        validTemporalConfigModel.Namespace,
		SyncJobQueueName: validTemporalConfigModel.SyncJobQueueName,
	}
)

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountTemporalConfig() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	s.Mocks.TemporalConfigProvider.On("GetConfig", mock.Anything, mock.Anything).
		Return(validTemporalConfig, nil)

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountTemporalConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountTemporalConfigRequest{AccountId: accountId}))
	requireNoErrResp(s.T(), resp, err)

	tc := resp.Msg.GetConfig()
	require.NotNil(s.T(), tc)

	require.Equal(s.T(), validTemporalConfig.Namespace, tc.GetNamespace())
	require.Equal(s.T(), validTemporalConfig.SyncJobQueueName, tc.GetSyncJobQueueName())
	require.Equal(s.T(), validTemporalConfig.Url, tc.GetUrl())
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountTemporalConfig_NoAccount() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountTemporalConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountTemporalConfigRequest{AccountId: uuid.NewString()}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetAccountTemporalConfig_NoAccount() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetAccountTemporalConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountTemporalConfigRequest{AccountId: uuid.NewString()}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetAccountTemporalConfig_NoConfig() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	s.Mocks.TemporalConfigProvider.On("GetConfig", mock.Anything, mock.Anything).
		Return(validTemporalConfig, nil)

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetAccountTemporalConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountTemporalConfigRequest{AccountId: accountId, Config: nil}))
	requireNoErrResp(s.T(), resp, err)

	tc := resp.Msg.GetConfig()
	require.NotNil(s.T(), tc)

	require.Equal(s.T(), validTemporalConfig.Namespace, tc.GetNamespace())
	require.Equal(s.T(), validTemporalConfig.SyncJobQueueName, tc.GetSyncJobQueueName())
	require.Equal(s.T(), validTemporalConfig.Url, tc.GetUrl())
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetAccountTemporalConfig() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	// kind of a bad test since we are mocking this client wholesale, but it at least verifies we can write the config
	s.Mocks.TemporalConfigProvider.On("GetConfig", mock.Anything, mock.Anything).
		Return(validTemporalConfig, nil)

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetAccountTemporalConfig(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountTemporalConfigRequest{
			AccountId: accountId, Config: &mgmtv1alpha1.AccountTemporalConfig{
				Url:              "test",
				Namespace:        "test",
				SyncJobQueueName: "test",
			}}))
	requireNoErrResp(s.T(), resp, err)

	tc := resp.Msg.GetConfig()
	require.NotNil(s.T(), tc)

	require.Equal(s.T(), validTemporalConfig.Namespace, tc.GetNamespace())
	require.Equal(s.T(), validTemporalConfig.SyncJobQueueName, tc.GetSyncJobQueueName())
	require.Equal(s.T(), validTemporalConfig.Url, tc.GetUrl())
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetUser_Auth() {
	client := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId("test-user1"),
	)
	userId := s.setUser(s.ctx, client)

	resp, err := client.GetUser(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	requireNoErrResp(s.T(), resp, err)
	require.Equal(s.T(), userId, resp.Msg.GetUserId())
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetUser_Auth_NotFound() {
	client := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId("test-user1"),
	)
	resp, err := client.GetUser(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodeNotFound)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetUser_Auth() {
	client := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	userId := s.setUser(s.ctx, client)
	require.NotEmpty(s.T(), userId)
	require.NotEqual(s.T(), "00000000-0000-0000-0000-000000000000", userId)
}

func (s *IntegrationTestSuite) Test_UserAccountService_CreateTeamAccount_Auth() {
	client := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	s.setUser(s.ctx, client)

	resp, err := client.CreateTeamAccount(
		s.ctx,
		connect.NewRequest(&mgmtv1alpha1.CreateTeamAccountRequest{Name: "test-name"}),
	)
	requireNoErrResp(s.T(), resp, err)
	require.NotEmpty(s.T(), resp.Msg.GetAccountId())
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetTeamAccountMembers_Auth() {
	client := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	userId := s.setUser(s.ctx, client)
	accountId := s.createTeamAccount(s.ctx, client, "test-team")

	s.Mocks.Authmanagerclient.On("GetUserBySub", mock.Anything, testAuthUserId).
		Return(validAuthUser, nil)

	resp, err := client.GetTeamAccountMembers(
		s.ctx,
		connect.NewRequest(&mgmtv1alpha1.GetTeamAccountMembersRequest{AccountId: accountId}),
	)
	requireNoErrResp(s.T(), resp, err)

	members := resp.Msg.GetUsers()
	require.Len(s.T(), members, 1)
	member := members[0]
	require.Equal(s.T(), userId, member.GetId())
	require.Equal(s.T(), validAuthUser.Email, member.GetEmail())
	require.Equal(s.T(), validAuthUser.Picture, member.GetImage())
	require.Equal(s.T(), validAuthUser.Name, member.GetName())
}

func (s *IntegrationTestSuite) setUser(
	ctx context.Context,
	client mgmtv1alpha1connect.UserAccountServiceClient,
) string {
	s.T().Helper()
	resp, err := client.SetUser(ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRequest{}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetUserId()
}

func (s *IntegrationTestSuite) createTeamAccount(
	ctx context.Context,
	client mgmtv1alpha1connect.UserAccountServiceClient,
	name string,
) string {
	s.T().Helper()
	resp, err := client.CreateTeamAccount(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.CreateTeamAccountRequest{Name: name}),
	)
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.AccountId
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetUser() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetUser(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
	requireNoErrResp(s.T(), resp, err)

	userId := resp.Msg.GetUserId()
	require.NotEmpty(s.T(), userId)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetUser() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetUser(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRequest{}))
	requireNoErrResp(s.T(), resp, err)

	userId := resp.Msg.UserId
	require.NotEmpty(s.T(), userId)
	require.Equal(s.T(), "00000000-0000-0000-0000-000000000000", userId)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccounts_Empty() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetUserAccounts(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserAccountsRequest{}))
	requireNoErrResp(s.T(), resp, err)

	accounts := resp.Msg.GetAccounts()
	require.Empty(s.T(), accounts)
}

func (s *IntegrationTestSuite) Test_UserAccountService_SetPersonalAccount() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetPersonalAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetPersonalAccountRequest{}))
	requireNoErrResp(s.T(), resp, err)

	accountId := resp.Msg.GetAccountId()
	require.NotEmpty(s.T(), accountId)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccounts_NotEmpty() {
	s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	accResp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetUserAccounts(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetUserAccountsRequest{}))
	requireNoErrResp(s.T(), accResp, err)

	accounts := accResp.Msg.GetAccounts()
	require.NotEmpty(s.T(), accounts)
	require.Len(s.T(), accounts, 1)
}

func (s *IntegrationTestSuite) Test_UserAccountService_IsUserInAccount() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		IsUserInAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.IsUserInAccountRequest{
			AccountId: accountId,
		}))
	requireNoErrResp(s.T(), resp, err)
	require.True(s.T(), resp.Msg.GetOk())

	resp, err = s.OSSUnauthenticatedLicensedClients.Users().
		IsUserInAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.IsUserInAccountRequest{
			AccountId: uuid.NewString(),
		}))
	requireNoErrResp(s.T(), resp, err)
	require.False(s.T(), resp.Msg.GetOk())
}

func (s *IntegrationTestSuite) Test_UserAccountService_CreateTeamAccount_NoAuth() {
	s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		CreateTeamAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateTeamAccountRequest{Name: "test-name"}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetTeamAccountMembers_NoAuth_Personal() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetTeamAccountMembers(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetTeamAccountMembersRequest{AccountId: accountId}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_RemoveTeamAccountMember_NoAuth_Personal() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		RemoveTeamAccountMember(s.ctx, connect.NewRequest(&mgmtv1alpha1.RemoveTeamAccountMemberRequest{AccountId: accountId, UserId: uuid.NewString()}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_InviteUserToTeamAccount_NoAuth_Personal() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		InviteUserToTeamAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.InviteUserToTeamAccountRequest{AccountId: accountId, Email: "test@example.com"}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetTeamAccountInvites_NoAuth_Personal() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetTeamAccountInvites(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetTeamAccountInvitesRequest{AccountId: accountId}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_RemoveTeamAccountInvite_NoAuth_Personal() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		RemoveTeamAccountInvite(s.ctx, connect.NewRequest(&mgmtv1alpha1.RemoveTeamAccountInviteRequest{Id: uuid.NewString()}))
	requireNoErrResp(s.T(), resp, err)
}

func (s *IntegrationTestSuite) Test_UserAccountService_AcceptTeamAccountInvite_NoAuth_Personal() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		AcceptTeamAccountInvite(s.ctx, connect.NewRequest(&mgmtv1alpha1.AcceptTeamAccountInviteRequest{Token: uuid.NewString()}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodeUnauthenticated)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetSystemInformation() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetSystemInformation(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemInformationRequest{}))
	requireNoErrResp(s.T(), resp, err)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountStatus_OSS_Personal() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountStatus(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountStatusRequest{
			AccountId: accountId,
		}))
	requireNoErrResp(s.T(), resp, err)

	require.Equal(
		s.T(),
		mgmtv1alpha1.BillingStatus_BILLING_STATUS_UNSPECIFIED,
		resp.Msg.GetSubscriptionStatus(),
	)
}

func (s *IntegrationTestSuite) Test_UserAccountService_IsAccountStatusValid_OSS_Personal() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())

	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		IsAccountStatusValid(s.ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
			AccountId: accountId,
		}))
	requireNoErrResp(s.T(), resp, err)

	require.True(s.T(), resp.Msg.GetIsValid())
	require.Empty(s.T(), resp.Msg.GetReason())
	require.Equal(
		s.T(),
		mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_REASON_UNSPECIFIED,
		resp.Msg.GetAccountStatus(),
	)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountBillingCheckoutSession() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountBillingCheckoutSession(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountBillingCheckoutSessionRequest{
			AccountId: accountId,
		}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodeUnimplemented)
}

func (s *IntegrationTestSuite) Test_UserAccountService_GetAccountBillingPortalSession() {
	accountId := s.createPersonalAccount(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		GetAccountBillingPortalSession(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountBillingPortalSessionRequest{
			AccountId: accountId,
		}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodeUnimplemented)
}

func (s *IntegrationTestSuite) Test_ConvertPersonalToTeamAccount() {
	t := s.T()

	t.Run("OSS unauth", func(t *testing.T) {
		s.setUser(s.ctx, s.OSSUnauthenticatedLicensedClients.Users())
		resp, err := s.OSSUnauthenticatedLicensedClients.Users().
			ConvertPersonalToTeamAccount(s.ctx, connect.NewRequest(&mgmtv1alpha1.ConvertPersonalToTeamAccountRequest{
				Name: "unauthteamname",
			}))
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodePermissionDenied)
	})

	t.Run("OSS auth success", func(t *testing.T) {
		userclient := s.OSSAuthenticatedLicensedClients.Users(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		s.setUser(s.ctx, userclient)
		accountId := s.createPersonalAccount(s.ctx, userclient)

		resp, err := userclient.ConvertPersonalToTeamAccount(
			s.ctx,
			connect.NewRequest(&mgmtv1alpha1.ConvertPersonalToTeamAccountRequest{
				Name:      "newname",
				AccountId: &accountId,
			}),
		)
		requireNoErrResp(t, resp, err)
		require.Empty(t, resp.Msg.GetCheckoutSessionUrl())
	})
}

func (s *IntegrationTestSuite) Test_SetBillingMeterEvent() {
	resp, err := s.OSSUnauthenticatedLicensedClients.Users().
		SetBillingMeterEvent(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetBillingMeterEventRequest{}))
	requireErrResp(s.T(), resp, err)
	requireConnectError(s.T(), err, connect.CodePermissionDenied)
}

func (s *IntegrationTestSuite) Test_UserAccountService_HasPermission() {
	t := s.T()

	t.Run("oss auth", func(t *testing.T) {
		userclient := s.OSSAuthenticatedLicensedClients.Users(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		s.setUser(s.ctx, userclient)
		s.createPersonalAccount(s.ctx, userclient)

		t.Run("ok", func(t *testing.T) {
			accountId := s.createTeamAccount(s.ctx, userclient, uuid.NewString())

			resp, err := userclient.HasPermission(
				s.ctx,
				connect.NewRequest(&mgmtv1alpha1.HasPermissionRequest{
					AccountId: accountId,
					Resource: &mgmtv1alpha1.ResourcePermission{
						Type:   mgmtv1alpha1.ResourcePermission_TYPE_CONNECTION,
						Id:     uuid.NewString(),
						Action: mgmtv1alpha1.ResourcePermission_ACTION_CREATE,
					},
				}),
			)
			requireNoErrResp(t, resp, err)
			require.True(t, resp.Msg.GetHasPermission())
		})
	})
}

func (s *IntegrationTestSuite) Test_UserAccountService_HasPermissions() {
	t := s.T()

	t.Run("oss auth", func(t *testing.T) {
		userclient := s.OSSAuthenticatedLicensedClients.Users(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		s.setUser(s.ctx, userclient)
		s.createPersonalAccount(s.ctx, userclient)

		t.Run("ok", func(t *testing.T) {
			accountId := s.createTeamAccount(s.ctx, userclient, uuid.NewString())

			resp, err := userclient.HasPermissions(
				s.ctx,
				connect.NewRequest(&mgmtv1alpha1.HasPermissionsRequest{
					AccountId: accountId,
					Resources: []*mgmtv1alpha1.ResourcePermission{
						{
							Type:   mgmtv1alpha1.ResourcePermission_TYPE_CONNECTION,
							Id:     uuid.NewString(),
							Action: mgmtv1alpha1.ResourcePermission_ACTION_CREATE,
						},
						{
							Type:   mgmtv1alpha1.ResourcePermission_TYPE_JOB,
							Id:     uuid.NewString(),
							Action: mgmtv1alpha1.ResourcePermission_ACTION_CREATE,
						},
					},
				}),
			)
			requireNoErrResp(t, resp, err)
			require.Equal(t, resp.Msg.GetAssertions(), []bool{true, true})
		})
	})
}
