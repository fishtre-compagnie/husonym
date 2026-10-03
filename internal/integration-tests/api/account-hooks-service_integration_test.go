package integrationtests_test

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (s *IntegrationTestSuite) Test_AccountHooksService_GetActiveAccountHooksByEvent() {
	t := s.T()
	ctx := s.ctx

	t.Run("OSS-authenticated-licensed", func(t *testing.T) {
		client := s.OSSAuthenticatedLicensedClients.AccountHooks(
			integrationtests_test.WithUserId(testAuthUserId),
		)
		s.setUser(
			ctx,
			s.OSSAuthenticatedLicensedClients.Users(
				integrationtests_test.WithUserId(testAuthUserId),
			),
		)

		t.Run("GetAccountHooks", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)
			createdHook := s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
				},
				true,
			)

			resp, err := client.GetAccountHooks(
				ctx,
				connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: accountId}),
			)
			requireNoErrResp(t, resp, err)
			require.ElementsMatch(t, []*mgmtv1alpha1.AccountHook{createdHook}, resp.Msg.Hooks)
		})

		t.Run("GetAccountHook", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)
			createdHook := s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
				},
				true,
			)

			resp, err := client.GetAccountHook(
				ctx,
				connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: createdHook.Id}),
			)
			requireNoErrResp(t, resp, err)
			require.Equal(t, createdHook, resp.Msg.Hook)
		})

		t.Run("CreateAccountHook", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)
			s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
				},
				true,
			)
		})

		t.Run("DeleteAccountHook", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)

			t.Run("ok", func(t *testing.T) {
				createdHook := s.createAccountHook_Webhook(
					ctx,
					t,
					client,
					accountId,
					"test-hook",
					[]mgmtv1alpha1.AccountHookEvent{
						mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
					},
					true,
				)

				resp, err := client.DeleteAccountHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: createdHook.Id}),
				)
				requireNoErrResp(t, resp, err)

				getResp, err := client.GetAccountHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: createdHook.Id}),
				)
				requireErrResp(t, getResp, err)
				requireConnectError(t, err, connect.CodeNotFound)
			})

			t.Run("non_existent", func(t *testing.T) {
				resp, err := client.DeleteAccountHook(
					ctx,
					connect.NewRequest(
						&mgmtv1alpha1.DeleteAccountHookRequest{Id: uuid.NewString()},
					),
				)
				requireNoErrResp(t, resp, err)
			})
		})

		t.Run("IsAccountHookNameAvailable", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)

			t.Run("yes", func(t *testing.T) {
				resp, err := client.IsAccountHookNameAvailable(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.IsAccountHookNameAvailableRequest{
						AccountId: accountId,
						Name:      "test-hook",
					}),
				)
				requireNoErrResp(t, resp, err)
				require.True(t, resp.Msg.IsAvailable)
			})
			t.Run("no", func(t *testing.T) {
				createdHook := s.createAccountHook_Webhook(
					ctx,
					t,
					client,
					accountId,
					"test-hook",
					[]mgmtv1alpha1.AccountHookEvent{
						mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
					},
					true,
				)

				resp, err := client.IsAccountHookNameAvailable(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.IsAccountHookNameAvailableRequest{
						AccountId: accountId,
						Name:      createdHook.Name,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.False(t, resp.Msg.IsAvailable)
			})
		})

		t.Run("SetAccountHookEnabled", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)

			t.Run("ok", func(t *testing.T) {
				createdHook := s.createAccountHook_Webhook(
					ctx,
					t,
					client,
					accountId,
					"test-hook",
					[]mgmtv1alpha1.AccountHookEvent{
						mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
					},
					true,
				)

				resp, err := client.SetAccountHookEnabled(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
						Id:      createdHook.Id,
						Enabled: false,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.False(t, resp.Msg.GetHook().GetEnabled())

				resp, err = client.SetAccountHookEnabled(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
						Id:      createdHook.Id,
						Enabled: true,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.True(t, resp.Msg.GetHook().GetEnabled())
			})
		})

		t.Run("GetActiveAccountHooksByEvent", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)
			createdHook := s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
				},
				true,
			)
			createdHook2 := s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook-2",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED,
				},
				true,
			)
			s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook-3",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
				},
				false,
			)
			createdHook4 := s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook-4",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED,
				},
				true,
			)

			t.Run("ok", func(t *testing.T) {

				resp, err := client.GetActiveAccountHooksByEvent(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
						AccountId: accountId,
						Event:     mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.ElementsMatch(
					t,
					[]*mgmtv1alpha1.AccountHook{createdHook, createdHook2, createdHook4},
					resp.Msg.GetHooks(),
				)
			})

			t.Run("wildcard", func(t *testing.T) {
				resp, err := client.GetActiveAccountHooksByEvent(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
						AccountId: accountId,
						Event:     mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED,
					}),
				)
				requireNoErrResp(t, resp, err)
				require.Len(t, resp.Msg.GetHooks(), 2)
				require.ElementsMatch(
					t,
					[]*mgmtv1alpha1.AccountHook{createdHook2, createdHook4},
					resp.Msg.GetHooks(),
				)
			})
		})

		t.Run("UpdateAccountHook", func(t *testing.T) {
			accountId := s.createTeamAccount(
				ctx,
				s.OSSAuthenticatedLicensedClients.Users(
					integrationtests_test.WithUserId(testAuthUserId),
				),
				uuid.NewString(),
			)
			createdHook := s.createAccountHook_Webhook(
				ctx,
				t,
				client,
				accountId,
				"test-hook",
				[]mgmtv1alpha1.AccountHookEvent{
					mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
				},
				true,
			)

			t.Run("ok", func(t *testing.T) {
				resp, err := client.UpdateAccountHook(
					ctx,
					connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
						Id:          createdHook.Id,
						Name:        "test-hook-updated",
						Description: "updated hook",
						Events: []mgmtv1alpha1.AccountHookEvent{
							mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
						},
						Enabled: true,
						Config: &mgmtv1alpha1.AccountHookConfig{
							Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
								Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{
									Url:                    "https://example2.com",
									Secret:                 "foo-updated",
									DisableSslVerification: true,
								},
							},
						},
					}),
				)
				requireNoErrResp(t, resp, err)
				updatedHook := resp.Msg.GetHook()
				require.Equal(t, "test-hook-updated", updatedHook.GetName())
				require.Equal(t, "updated hook", updatedHook.GetDescription())
				require.ElementsMatch(
					t,
					[]mgmtv1alpha1.AccountHookEvent{
						mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED,
					},
					updatedHook.GetEvents(),
				)
				require.True(t, updatedHook.GetEnabled())
				require.Equal(
					t,
					"https://example2.com",
					updatedHook.GetConfig().GetWebhook().GetUrl(),
				)
				require.Equal(t, "foo-updated", updatedHook.GetConfig().GetWebhook().GetSecret())
				require.True(t, updatedHook.GetConfig().GetWebhook().GetDisableSslVerification())
			})
		})
	})
}

func (s *IntegrationTestSuite) createAccountHook_Webhook(
	ctx context.Context,
	t testing.TB,
	client mgmtv1alpha1connect.AccountHookServiceClient,
	accountId string,
	name string,
	events []mgmtv1alpha1.AccountHookEvent,
	enabled bool,

) *mgmtv1alpha1.AccountHook {
	createResp, err := client.CreateAccountHook(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
			AccountId: accountId,
			Hook: &mgmtv1alpha1.NewAccountHook{
				Name:        name,
				Description: "created hook",
				Events:      events,
				Enabled:     enabled,
				Config: &mgmtv1alpha1.AccountHookConfig{
					Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
						Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{
							Url:                    "https://example.com",
							Secret:                 "foo",
							DisableSslVerification: false,
						},
					},
				},
			},
		}),
	)
	requireNoErrResp(t, createResp, err)
	return createResp.Msg.GetHook()
}

// The Slack kind of account hook is retired: none is created or armed, the four Slack
// procedures are not implemented, and a Slack hook that already exists is still listed and
// deleted.
func (s *IntegrationTestSuite) Test_AccountHooksService_SlackKindIsRetired() {
	t := s.T()
	ctx := s.ctx

	userclient := s.OSSAuthenticatedLicensedClients.Users(
		integrationtests_test.WithUserId(testAuthUserId),
	)
	userId := s.setUser(ctx, userclient)
	accountId := s.createTeamAccount(ctx, userclient, uuid.NewString())
	hookclient := s.OSSAuthenticatedLicensedClients.AccountHooks(
		integrationtests_test.WithUserId(testAuthUserId),
	)

	slackConfig := &mgmtv1alpha1.AccountHookConfig{
		Config: &mgmtv1alpha1.AccountHookConfig_Slack{
			Slack: &mgmtv1alpha1.AccountHookConfig_SlackHook{ChannelId: "channel-id"},
		},
	}

	t.Run("creating a Slack hook is refused", func(t *testing.T) {
		resp, err := hookclient.CreateAccountHook(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
				AccountId: accountId,
				Hook: &mgmtv1alpha1.NewAccountHook{
					Name:    "slack-refused",
					Events:  []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED},
					Enabled: true,
					Config:  slackConfig,
				},
			}),
		)
		requireErrResp(t, resp, err)
		requireConnectError(t, err, connect.CodeInvalidArgument)
		require.ErrorContains(t, err, "no longer supported")
	})

	t.Run("the Slack procedures are not implemented", func(t *testing.T) {
		_, err := hookclient.GetSlackConnectionUrl(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.GetSlackConnectionUrlRequest{AccountId: accountId}),
		)
		requireConnectError(t, err, connect.CodeUnimplemented)

		_, err = hookclient.HandleSlackOAuthCallback(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.HandleSlackOAuthCallbackRequest{State: "state", Code: "code"}),
		)
		requireConnectError(t, err, connect.CodeUnimplemented)

		_, err = hookclient.TestSlackConnection(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.TestSlackConnectionRequest{AccountId: accountId}),
		)
		requireConnectError(t, err, connect.CodeUnimplemented)

		_, err = hookclient.SendSlackMessage(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.SendSlackMessageRequest{AccountHookId: uuid.NewString()}),
		)
		requireConnectError(t, err, connect.CodeUnimplemented)
	})

	t.Run("an existing Slack hook is listed, turned off and deleted", func(t *testing.T) {
		accountUuid, err := husonymdb.ToUuid(accountId)
		require.NoError(t, err)
		userUuid, err := husonymdb.ToUuid(userId)
		require.NoError(t, err)
		config, err := json.Marshal(slackConfig)
		require.NoError(t, err)
		stored, err := s.HusonymQuerier.CreateAccountHook(
			ctx,
			s.Pgcontainer.DB,
			db_queries.CreateAccountHookParams{
				Name:            "existing-slack",
				Description:     "stored before the Slack kind was retired",
				AccountID:       accountUuid,
				Events:          []int32{int32(mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED)},
				Config:          config,
				CreatedByUserID: userUuid,
				UpdatedByUserID: userUuid,
				Enabled:         true,
			},
		)
		require.NoError(t, err)
		hookId := husonymdb.UUIDString(stored.ID)

		listResp, err := hookclient.GetAccountHooks(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: accountId}),
		)
		requireNoErrResp(t, listResp, err)
		require.Len(t, listResp.Msg.GetHooks(), 1)
		require.Equal(t, hookId, listResp.Msg.GetHooks()[0].GetId())
		require.Equal(t, "channel-id", listResp.Msg.GetHooks()[0].GetConfig().GetSlack().GetChannelId())

		getResp, err := hookclient.GetAccountHook(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: hookId}),
		)
		requireNoErrResp(t, getResp, err)
		require.NotNil(t, getResp.Msg.GetHook().GetConfig().GetSlack())

		activeResp, err := hookclient.GetActiveAccountHooksByEvent(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
				AccountId: accountId,
				Event:     mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
			}),
		)
		requireNoErrResp(t, activeResp, err)
		require.Len(t, activeResp.Msg.GetHooks(), 1)

		_, err = hookclient.UpdateAccountHook(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
				Id:     hookId,
				Name:   "renamed",
				Events: getResp.Msg.GetHook().GetEvents(),
				Config: slackConfig,
			}),
		)
		requireConnectError(t, err, connect.CodeInvalidArgument)

		_, err = hookclient.SetAccountHookEnabled(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: hookId, Enabled: true}),
		)
		requireConnectError(t, err, connect.CodeInvalidArgument)

		offResp, err := hookclient.SetAccountHookEnabled(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: hookId, Enabled: false}),
		)
		requireNoErrResp(t, offResp, err)
		require.False(t, offResp.Msg.GetHook().GetEnabled())

		_, err = hookclient.SetAccountHookEnabled(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: hookId, Enabled: true}),
		)
		requireConnectError(t, err, connect.CodeInvalidArgument)

		deleteResp, err := hookclient.DeleteAccountHook(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: hookId}),
		)
		requireNoErrResp(t, deleteResp, err)

		listResp, err = hookclient.GetAccountHooks(
			ctx,
			connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: accountId}),
		)
		requireNoErrResp(t, listResp, err)
		require.Empty(t, listResp.Msg.GetHooks())
	})
}
