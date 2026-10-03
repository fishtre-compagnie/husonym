package integrationtests_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	ee_slack "github.com/fishtre-compagnie/husonym/internal/ee/slack"
	"github.com/google/uuid"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const noActiveLicenseMessage = "account does not have an active license"

func requireLicenseRefusal(t require.TestingT, err error) {
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, noActiveLicenseMessage)
}

// A license that lapses leaves an account able to look at and remove what it configured:
// hooks included, which are registered whatever the license says. Creating, changing and
// running are what needs a valid license.
func (s *IntegrationTestSuite) Test_Hooks_UnderAFrozenLicense() {
	t := s.T()
	ctx := s.ctx
	userOpt := integrationtests_test.WithUserId("frozen-license-hooks")
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	jobs := s.OSSAuthenticatedExpiringClients.Jobs(userOpt)
	connections := s.OSSAuthenticatedExpiringClients.Connections(userOpt)
	accountHooks := s.OSSAuthenticatedExpiringClients.AccountHooks(userOpt)
	userId := s.setUser(ctx, users)
	accountId := s.createPersonalAccount(ctx, users)
	t.Cleanup(func() { s.Mocks.ExpiringLicense.SetValid(true) })

	source := s.createPostgresConnection(connections, accountId, "frozen-source", "test")
	destination := s.createPostgresConnection(connections, accountId, "frozen-destination", "test2")
	mapping := &mgmtv1alpha1.JobMapping{
		Schema: "public",
		Table:  "users",
		Column: "name",
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
		}},
	}
	newJobRequest := func(name string) *mgmtv1alpha1.CreateJobRequest {
		return &mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   name,
			Mappings:  []*mgmtv1alpha1.JobMapping{mapping},
			Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
				Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
					ConnectionId: source.GetId(),
				}},
			}},
			Destinations: []*mgmtv1alpha1.CreateJobDestination{{
				ConnectionId: destination.GetId(),
				Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
					PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
				}},
			}},
		}
	}
	s.MockTemporalForCreateJob("frozen-license-job")
	created, err := jobs.CreateJob(ctx, connect.NewRequest(newJobRequest("frozen-license-job")))
	requireNoErrResp(t, created, err)
	job := created.Msg.GetJob()

	jobHook := s.createSqlJobHook(
		ctx, t, jobs, "frozen-job-hook", job.GetId(), source.GetId(), true,
		&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
			Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
		},
	)
	disposableJobHook := s.createSqlJobHook(
		ctx, t, jobs, "frozen-disposable-job-hook", job.GetId(), source.GetId(), true,
		&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
			Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{},
		},
	)
	accountHook := s.createAccountHook_Webhook(
		ctx, t, accountHooks, accountId, "frozen-account-hook",
		[]mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED},
		true,
	)
	disposableAccountHook := s.createAccountHook_Webhook(
		ctx, t, accountHooks, accountId, "frozen-disposable-account-hook",
		[]mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED},
		true,
	)
	disposableJob := s.createJobUnderValidLicense(t, jobs, newJobRequest("frozen-disposable-job"))

	applyMappingChanges := func() (*connect.Response[mgmtv1alpha1.ApplyMappingChangesResponse], error) {
		return jobs.ApplyMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.ApplyMappingChangesRequest{
			AccountId: accountId,
			JobId:     job.GetId(),
			Mappings:  []*mgmtv1alpha1.JobMapping{mapping},
		}))
	}
	// The Slack client is a mock shared by the whole suite: the state names the account, and
	// each callback carries a code of its own, so that what reached Slack can be told apart.
	const acceptedSlackCode, refusedSlackCode = "frozen-license-accepted-code", "frozen-license-refused-code"
	s.Mocks.Slackclient.EXPECT().
		ExchangeCodeForAccessToken(mock.Anything, acceptedSlackCode).
		Return(&slack.OAuthV2Response{AccessToken: "access_token"}, nil).
		Maybe() // another test of the suite may have registered an exchange that answers first
	slackCallback := func(code string) (*connect.Response[mgmtv1alpha1.HandleSlackOAuthCallbackResponse], error) {
		s.Mocks.Slackclient.EXPECT().
			ValidateState(mock.Anything, mock.Anything, userId, mock.Anything).
			Return(&ee_slack.OauthState{
				AccountId: accountId,
				UserId:    userId,
				Timestamp: time.Now().UTC().Unix(),
			}, nil).
			Once()
		return accountHooks.HandleSlackOAuthCallback(ctx, connect.NewRequest(&mgmtv1alpha1.HandleSlackOAuthCallbackRequest{
			State: "state",
			Code:  code,
		}))
	}

	applied, err := applyMappingChanges()
	requireNoErrResp(t, applied, err)
	require.Len(t, applied.Msg.GetMappings(), 1)
	connected, err := slackCallback(acceptedSlackCode)
	requireNoErrResp(t, connected, err)
	s.Mocks.Slackclient.AssertCalled(t, "ExchangeCodeForAccessToken", mock.Anything, acceptedSlackCode)

	s.Mocks.ExpiringLicense.SetValid(false)

	t.Run("what needs a valid license is refused", func(t *testing.T) {
		_, err := jobs.CreateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
			JobId: job.GetId(),
			Hook: &mgmtv1alpha1.NewJobHook{
				Name: "refused",
				Config: &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{
					Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
						Query:        "select 1;",
						ConnectionId: source.GetId(),
						Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
							Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
						},
					},
				}},
			},
		}))
		requireLicenseRefusal(t, err)

		_, err = jobs.UpdateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobHookRequest{
			Id:     jobHook.GetId(),
			Name:   "renamed",
			Config: jobHook.GetConfig(),
		}))
		requireLicenseRefusal(t, err)

		_, err = accountHooks.CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
			AccountId: accountId,
			Hook: &mgmtv1alpha1.NewAccountHook{
				Name:   "refused",
				Events: []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED},
				Config: accountHook.GetConfig(),
			},
		}))
		requireLicenseRefusal(t, err)

		_, err = accountHooks.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id:     accountHook.GetId(),
			Name:   "renamed",
			Events: accountHook.GetEvents(),
			Config: accountHook.GetConfig(),
		}))
		requireLicenseRefusal(t, err)

		_, err = jobs.CreateJob(ctx, connect.NewRequest(newJobRequest("refused-job")))
		requireLicenseRefusal(t, err)

		_, err = jobs.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
		requireLicenseRefusal(t, err)

		_, err = applyMappingChanges()
		requireLicenseRefusal(t, err)

		// Refused before the code is exchanged: nothing is asked of Slack, nothing is stored.
		_, err = slackCallback(refusedSlackCode)
		requireLicenseRefusal(t, err)
		s.Mocks.Slackclient.AssertNotCalled(t, "ExchangeCodeForAccessToken", mock.Anything, refusedSlackCode)

		anonymize := s.OSSAuthenticatedExpiringClients.Anonymize(userOpt)
		_, err = anonymize.AnonymizeSingle(ctx, connect.NewRequest(&mgmtv1alpha1.AnonymizeSingleRequest{
			AccountId: accountId,
			InputData: "foo",
			TransformerMappings: []*mgmtv1alpha1.TransformerMapping{{
				Transformer: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_TransformPiiTextConfig{},
				},
			}},
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	})

	t.Run("a hook can be turned off but not back on", func(t *testing.T) {
		require.True(t, jobHook.GetEnabled())
		require.True(t, accountHook.GetEnabled())

		off, err := jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
			Id: jobHook.GetId(), Enabled: false,
		}))
		requireNoErrResp(t, off, err)
		require.False(t, off.Msg.GetHook().GetEnabled())
		_, err = jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
			Id: jobHook.GetId(), Enabled: true,
		}))
		requireLicenseRefusal(t, err)

		accountOff, err := accountHooks.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
			Id: accountHook.GetId(), Enabled: false,
		}))
		requireNoErrResp(t, accountOff, err)
		require.False(t, accountOff.Msg.GetHook().GetEnabled())
		_, err = accountHooks.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{
			Id: accountHook.GetId(), Enabled: true,
		}))
		requireLicenseRefusal(t, err)
	})

	t.Run("what was configured stays readable", func(t *testing.T) {
		hooksResp, err := jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: job.GetId()}))
		requireNoErrResp(t, hooksResp, err)
		require.Len(t, hooksResp.Msg.GetHooks(), 2)

		hookResp, err := jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: jobHook.GetId()}))
		requireNoErrResp(t, hookResp, err)
		require.Equal(t, jobHook.GetId(), hookResp.Msg.GetHook().GetId())

		availableResp, err := jobs.IsJobHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsJobHookNameAvailableRequest{
			JobId: job.GetId(),
			Name:  uuid.NewString(),
		}))
		requireNoErrResp(t, availableResp, err)

		activeResp, err := jobs.GetActiveJobHooksByTiming(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
			JobId: job.GetId(),
		}))
		requireNoErrResp(t, activeResp, err)

		accountHooksResp, err := accountHooks.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{
			AccountId: accountId,
		}))
		requireNoErrResp(t, accountHooksResp, err)
		require.Len(t, accountHooksResp.Msg.GetHooks(), 2)

		accountHookResp, err := accountHooks.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{
			Id: accountHook.GetId(),
		}))
		requireNoErrResp(t, accountHookResp, err)

		nameResp, err := accountHooks.IsAccountHookNameAvailable(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountHookNameAvailableRequest{
			AccountId: accountId,
			Name:      uuid.NewString(),
		}))
		requireNoErrResp(t, nameResp, err)

		activeAccountResp, err := accountHooks.GetActiveAccountHooksByEvent(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
			AccountId: accountId,
		}))
		requireNoErrResp(t, activeAccountResp, err)

		jobsResp, err := jobs.GetJobs(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: accountId}))
		requireNoErrResp(t, jobsResp, err)
		require.NotEmpty(t, jobsResp.Msg.GetJobs())

		s.Mocks.TemporalClientManager.EXPECT().
			GetWorkflowExecutionsByScheduleIds(mock.Anything, accountId, mock.Anything, mock.Anything).
			Return(nil, nil).Once()
		runsResp, err := jobs.GetJobRuns(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunsRequest{
			Id: &mgmtv1alpha1.GetJobRunsRequest_JobId{JobId: job.GetId()},
		}))
		requireNoErrResp(t, runsResp, err)
	})

	t.Run("what was configured can still be removed", func(t *testing.T) {
		deleteJobHook, err := jobs.DeleteJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{
			Id: disposableJobHook.GetId(),
		}))
		requireNoErrResp(t, deleteJobHook, err)

		deleteAccountHook, err := accountHooks.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{
			Id: disposableAccountHook.GetId(),
		}))
		requireNoErrResp(t, deleteAccountHook, err)

		s.Mocks.TemporalClientManager.EXPECT().
			DeleteSchedule(mock.Anything, accountId, disposableJob.GetId(), mock.Anything).
			Return(nil).Once()
		deleteJob, err := jobs.DeleteJob(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobRequest{
			Id: disposableJob.GetId(),
		}))
		requireNoErrResp(t, deleteJob, err)
	})
}

func (s *IntegrationTestSuite) createJobUnderValidLicense(
	t require.TestingT,
	jobs mgmtv1alpha1connect.JobServiceClient,
	req *mgmtv1alpha1.CreateJobRequest,
) *mgmtv1alpha1.Job {
	s.MockTemporalForCreateJob(req.GetJobName())
	resp, err := jobs.CreateJob(s.ctx, connect.NewRequest(req))
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp.Msg.GetJob()
}
