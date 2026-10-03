package integrationtests_test

import (
	"fmt"
	"sync"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const maskedSecret = "********"

// hookGround is an account of its own, with a job between two connections, and the clients
// of the one who administers it.
type hookGround struct {
	accountId    string
	adminId      string
	source       *mgmtv1alpha1.Connection
	destination  *mgmtv1alpha1.Connection
	job          *mgmtv1alpha1.Job
	users        mgmtv1alpha1connect.UserAccountServiceClient
	jobs         mgmtv1alpha1connect.JobServiceClient
	accountHooks mgmtv1alpha1connect.AccountHookServiceClient
}

func (s *IntegrationTestSuite) newHookGround(name string) *hookGround {
	admin := integrationtests_test.WithUserId(name + "-admin")
	clients := s.OSSAuthenticatedLicensedClients
	g := &hookGround{
		users:        clients.Users(admin),
		jobs:         clients.Jobs(admin),
		accountHooks: clients.AccountHooks(admin),
	}
	g.adminId = s.setUser(s.ctx, g.users)
	g.accountId = s.createTeamAccount(s.ctx, g.users, name+"-"+uuid.NewString())
	g.source = s.createPostgresConnection(clients.Connections(admin), g.accountId, name+"-source", "test")
	g.destination = s.createPostgresConnection(clients.Connections(admin), g.accountId, name+"-destination", "test2")

	s.MockTemporalForCreateJob(name + "-job")
	created, err := g.jobs.CreateJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: g.accountId,
		JobName:   name + "-job",
		Mappings: []*mgmtv1alpha1.JobMapping{{
			Schema: "public", Table: "users", Column: "name",
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}},
		}},
		Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
				ConnectionId: g.source.GetId(),
			}},
		}},
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: g.destination.GetId(),
			Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
				PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
			}},
		}},
	}))
	requireNoErrResp(s.T(), created, err)
	g.job = created.Msg.GetJob()
	return g
}

// addMember makes another person a member of the account of the ground, with a role, or
// with none when the role is unspecified.
func (s *IntegrationTestSuite) addMember(g *hookGround, token string, role mgmtv1alpha1.AccountRole) integrationtests_test.ClientConfigOption {
	t := s.T()
	opt := integrationtests_test.WithUserId(token)
	memberId := s.setUser(s.ctx, s.OSSAuthenticatedLicensedClients.Users(opt))
	accountUuid, err := husonymdb.ToUuid(g.accountId)
	require.NoError(t, err)
	memberUuid, err := husonymdb.ToUuid(memberId)
	require.NoError(t, err)
	require.NoError(t, s.HusonymQuerier.CreateAccountUserAssociation(s.ctx, s.Pgcontainer.DB,
		db_queries.CreateAccountUserAssociationParams{AccountID: accountUuid, UserID: memberUuid},
	))
	if role != mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED {
		resp, err := g.users.SetUserRole(s.ctx, connect.NewRequest(&mgmtv1alpha1.SetUserRoleRequest{
			AccountId: g.accountId, UserId: memberId, Role: role,
		}))
		requireNoErrResp(t, resp, err)
	}
	return opt
}

// storeAccountHook writes an account hook row the way the database holds it, without going
// through the API.
func (s *IntegrationTestSuite) storeAccountHook(g *hookGround, name, config, events string, enabled bool) (string, error) {
	var id string
	err := s.Pgcontainer.DB.QueryRow(s.ctx, `
INSERT INTO husonym_api.account_hooks
  (name, description, account_id, events, config, created_by_user_id, updated_by_user_id, enabled)
VALUES ($1, 'stored earlier', $2::uuid, $3::int[], $4::jsonb, $5::uuid, $5::uuid, $6)
RETURNING id::text`, name, g.accountId, events, config, g.adminId, enabled).Scan(&id)
	return id, err
}

func (s *IntegrationTestSuite) storeJobHook(g *hookGround, name, config string, priority int) (string, error) {
	var id string
	err := s.Pgcontainer.DB.QueryRow(s.ctx, `
INSERT INTO husonym_api.job_hooks
  (name, description, job_id, config, created_by_user_id, updated_by_user_id, enabled, priority)
VALUES ($1, 'stored earlier', $2::uuid, $3::jsonb, $4::uuid, $4::uuid, true, $5)
RETURNING id::text`, name, g.job.GetId(), config, g.adminId, priority).Scan(&id)
	return id, err
}

func webhookOf(url, secret string) *mgmtv1alpha1.AccountHookConfig {
	return &mgmtv1alpha1.AccountHookConfig{Config: &mgmtv1alpha1.AccountHookConfig_Webhook{
		Webhook: &mgmtv1alpha1.AccountHookConfig_WebHook{Url: url, Secret: secret},
	}}
}

// What a deployment already holds reads with the same meaning: rows in the stored form of
// each kind of hook are listed, read, turned off and deleted.
func (s *IntegrationTestSuite) Test_Hooks_RowsAlreadyStored() {
	t := s.T()
	ctx := s.ctx
	g := s.newHookGround("stored")

	t.Run("job hooks of each timing", func(t *testing.T) {
		preId, err := s.storeJobHook(g, "stored-pre",
			fmt.Sprintf(`{"sql":{"query":"select 1;","connectionId":%q,"timing":{"preSync":{}}}}`, g.source.GetId()), 2)
		require.NoError(t, err)
		postId, err := s.storeJobHook(g, "stored-post",
			fmt.Sprintf(`{"sql":{"query":"vacuum analyze users;","connectionId":%q,"timing":{"postSync":{}}}}`, g.destination.GetId()), 1)
		require.NoError(t, err)

		list, err := g.jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: g.job.GetId()}))
		requireNoErrResp(t, list, err)
		require.Len(t, list.Msg.GetHooks(), 2)
		require.Equal(t, postId, list.Msg.GetHooks()[0].GetId(), "listed by priority")
		require.Equal(t, preId, list.Msg.GetHooks()[1].GetId())

		pre, err := g.jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: preId}))
		requireNoErrResp(t, pre, err)
		require.Equal(t, "select 1;", pre.Msg.GetHook().GetConfig().GetSql().GetQuery())
		require.Equal(t, g.source.GetId(), pre.Msg.GetHook().GetConfig().GetSql().GetConnectionId())
		require.NotNil(t, pre.Msg.GetHook().GetConfig().GetSql().GetTiming().GetPreSync())
		require.Equal(t, uint32(2), pre.Msg.GetHook().GetPriority())

		post, err := g.jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: postId}))
		requireNoErrResp(t, post, err)
		require.Equal(t, "vacuum analyze users;", post.Msg.GetHook().GetConfig().GetSql().GetQuery())
		require.NotNil(t, post.Msg.GetHook().GetConfig().GetSql().GetTiming().GetPostSync())

		before, err := g.jobs.GetActiveJobHooksByTiming(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
			JobId: g.job.GetId(), Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
		}))
		requireNoErrResp(t, before, err)
		require.Len(t, before.Msg.GetHooks(), 1)
		require.Equal(t, preId, before.Msg.GetHooks()[0].GetId())

		for _, id := range []string{preId, postId} {
			off, err := g.jobs.SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{Id: id, Enabled: false}))
			requireNoErrResp(t, off, err)
			require.False(t, off.Msg.GetHook().GetEnabled())
			deleted, err := g.jobs.DeleteJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: id}))
			requireNoErrResp(t, deleted, err)
		}
		list, err = g.jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: g.job.GetId()}))
		requireNoErrResp(t, list, err)
		require.Empty(t, list.Msg.GetHooks())
	})

	t.Run("account hooks of each kind", func(t *testing.T) {
		plainId, err := s.storeAccountHook(g, "stored-webhook",
			`{"webhook":{"url":"https://example.com/hook","secret":"s3cret"}}`, "{2}", true)
		require.NoError(t, err)
		laxId, err := s.storeAccountHook(g, "stored-webhook-lax",
			`{"webhook":{"url":"https://example.com/hook","secret":"s3cret","disableSslVerification":true}}`, "{1,3}", true)
		require.NoError(t, err)
		slackId, err := s.storeAccountHook(g, "stored-slack", `{"slack":{"channelId":"C0123"}}`, "{0}", true)
		require.NoError(t, err)
		ftpId, err := s.storeAccountHook(g, "stored-other-address",
			`{"webhook":{"url":"ftp://files.example.com/hook","secret":"s3cret"}}`, "{0}", true)
		require.NoError(t, err)

		list, err := g.accountHooks.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: g.accountId}))
		requireNoErrResp(t, list, err)
		listed := []string{}
		for _, hook := range list.Msg.GetHooks() {
			listed = append(listed, hook.GetId())
		}
		require.Equal(t, []string{plainId, laxId, slackId, ftpId}, listed, "listed oldest first")

		get := func(id string) *mgmtv1alpha1.AccountHook {
			resp, err := g.accountHooks.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: id}))
			requireNoErrResp(t, resp, err)
			return resp.Msg.GetHook()
		}
		plain := get(plainId)
		require.Equal(t, "https://example.com/hook", plain.GetConfig().GetWebhook().GetUrl())
		require.Equal(t, "s3cret", plain.GetConfig().GetWebhook().GetSecret())
		require.False(t, plain.GetConfig().GetWebhook().GetDisableSslVerification())
		require.Equal(t, []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED}, plain.GetEvents())
		require.True(t, get(laxId).GetConfig().GetWebhook().GetDisableSslVerification())
		require.Equal(t, "C0123", get(slackId).GetConfig().GetSlack().GetChannelId())
		require.Equal(t, "ftp://files.example.com/hook", get(ftpId).GetConfig().GetWebhook().GetUrl())

		_, err = g.accountHooks.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id: ftpId, Name: "stored-other-address", Description: "unchanged address",
			Events: []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED},
			Config: webhookOf("ftp://files.example.com/hook", "s3cret"),
		}))
		requireConnectError(t, err, connect.CodeInvalidArgument)
		require.ErrorContains(t, err, "webhook url must be an http or https address")

		for _, id := range []string{plainId, laxId, slackId, ftpId} {
			off, err := g.accountHooks.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: id, Enabled: false}))
			requireNoErrResp(t, off, err)
			require.False(t, off.Msg.GetHook().GetEnabled())
			deleted, err := g.accountHooks.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: id}))
			requireNoErrResp(t, deleted, err)
		}
		list, err = g.accountHooks.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: g.accountId}))
		requireNoErrResp(t, list, err)
		require.Empty(t, list.Msg.GetHooks())
	})

	// The database holds no hook without a kind it knows: such a row cannot be written, so
	// there is none to read. How one would read is covered where the rows are in memory.
	t.Run("a configuration of no known kind is not stored", func(t *testing.T) {
		_, err := s.storeAccountHook(g, "stored-empty", `{}`, "{0}", true)
		require.ErrorContains(t, err, "hook_type_not_null")
		_, err = s.storeAccountHook(g, "stored-unknown", `{"discord":{"channelId":"C1"}}`, "{0}", true)
		require.ErrorContains(t, err, "hook_type_not_null")
		_, err = s.storeJobHook(g, "stored-empty", `{}`, 0)
		require.ErrorContains(t, err, "hook_timing_not_null")
	})
}

// Two creations with one name: the database lets one through, and the other is told the name
// is taken.
func (s *IntegrationTestSuite) Test_Hooks_ConcurrentCreationsWithOneName() {
	t := s.T()
	ctx := s.ctx
	g := s.newHookGround("race")
	const rounds = 25

	race := func(create func() error) (created, taken int, others []error) {
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() {
				<-start
				results <- create()
			})
		}
		close(start)
		wg.Wait()
		close(results)
		for err := range results {
			switch {
			case err == nil:
				created++
			case connect.CodeOf(err) == connect.CodeAlreadyExists:
				taken++
			default:
				others = append(others, err)
			}
		}
		return created, taken, others
	}

	for round := range rounds {
		name := fmt.Sprintf("race-%d", round)
		created, taken, others := race(func() error {
			_, err := g.jobs.CreateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
				JobId: g.job.GetId(),
				Hook: &mgmtv1alpha1.NewJobHook{
					Name: name, Description: "raced",
					Config: &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{
						Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
							Query: "select 1;", ConnectionId: g.source.GetId(),
							Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
								Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
							},
						},
					}},
				},
			}))
			return err
		})
		require.Empty(t, others, "job hook, round %d", round)
		require.Equal(t, []int{1, 1}, []int{created, taken}, "job hook, round %d", round)

		created, taken, others = race(func() error {
			_, err := g.accountHooks.CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
				AccountId: g.accountId,
				Hook: &mgmtv1alpha1.NewAccountHook{
					Name: name, Description: "raced",
					Events: []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED},
					Config: webhookOf("https://example.com/hook", "s3cret"),
				},
			}))
			return err
		})
		require.Empty(t, others, "account hook, round %d", round)
		require.Equal(t, []int{1, 1}, []int{created, taken}, "account hook, round %d", round)
	}

	jobHooks, err := g.jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: g.job.GetId()}))
	requireNoErrResp(t, jobHooks, err)
	require.Len(t, jobHooks.Msg.GetHooks(), rounds)
	accountHooks, err := g.accountHooks.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: g.accountId}))
	requireNoErrResp(t, accountHooks, err)
	require.Len(t, accountHooks.Msg.GetHooks(), rounds)
}

// Who reads the secret of a webhook, and what the others read and may do, with the roles as
// the access control holds them.
func (s *IntegrationTestSuite) Test_Hooks_ByCaller() {
	t := s.T()
	ctx := s.ctx
	g := s.newHookGround("callers")
	clients := s.OSSAuthenticatedLicensedClients

	hook := s.createAccountHook_Webhook(ctx, t, g.accountHooks, g.accountId, "signed",
		[]mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED}, true)
	require.Equal(t, "foo", hook.GetConfig().GetWebhook().GetSecret(), "the one who creates reads the secret back")
	jobHook := s.createSqlJobHook(ctx, t, g.jobs, "guarded", g.job.GetId(), g.source.GetId(), true,
		&mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{}})

	secrets := func(client mgmtv1alpha1connect.AccountHookServiceClient) []string {
		got, err := client.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: hook.GetId()}))
		requireNoErrResp(t, got, err)
		list, err := client.GetAccountHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHooksRequest{AccountId: g.accountId}))
		requireNoErrResp(t, list, err)
		require.Len(t, list.Msg.GetHooks(), 1)
		active, err := client.GetActiveAccountHooksByEvent(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
			AccountId: g.accountId, Event: mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED,
		}))
		requireNoErrResp(t, active, err)
		require.Len(t, active.Msg.GetHooks(), 1)
		return []string{
			got.Msg.GetHook().GetConfig().GetWebhook().GetSecret(),
			list.Msg.GetHooks()[0].GetConfig().GetWebhook().GetSecret(),
			active.Msg.GetHooks()[0].GetConfig().GetWebhook().GetSecret(),
		}
	}

	t.Run("an admin reads the secret", func(t *testing.T) {
		require.Equal(t, []string{"foo", "foo", "foo"}, secrets(g.accountHooks))
	})

	t.Run("the worker reads the secret", func(t *testing.T) {
		worker := clients.AccountHooks(integrationtests_test.WithUserId(apikey.NewV1WorkerKey()))
		require.Equal(t, []string{"foo", "foo", "foo"}, secrets(worker))
	})

	for name, role := range map[string]mgmtv1alpha1.AccountRole{
		"a viewer":    mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER,
		"a developer": mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER,
		"an executor": mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_EXECUTOR,
	} {
		t.Run(name+" reads a mask and changes no account hook", func(t *testing.T) {
			member := s.addMember(g, "callers-"+role.String(), role)
			accountHooks := clients.AccountHooks(member)
			require.Equal(t, []string{maskedSecret, maskedSecret, maskedSecret}, secrets(accountHooks))

			_, err := accountHooks.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
				Id: hook.GetId(), Name: "signed", Description: "changed", Events: hook.GetEvents(),
				Config: webhookOf("https://elsewhere.example.com", maskedSecret),
			}))
			requireConnectError(t, err, connect.CodePermissionDenied)
			_, err = accountHooks.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: hook.GetId(), Enabled: false}))
			requireConnectError(t, err, connect.CodePermissionDenied)
			_, err = accountHooks.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: hook.GetId()}))
			requireConnectError(t, err, connect.CodePermissionDenied)
		})
	}

	t.Run("a viewer and an executor change no job hook, a developer does", func(t *testing.T) {
		disable := func(member integrationtests_test.ClientConfigOption) error {
			_, err := clients.Jobs(member).SetJobHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetJobHookEnabledRequest{
				Id: jobHook.GetId(), Enabled: false,
			}))
			return err
		}
		requireConnectError(t, disable(integrationtests_test.WithUserId("callers-"+mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER.String())), connect.CodePermissionDenied)
		requireConnectError(t, disable(integrationtests_test.WithUserId("callers-"+mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_EXECUTOR.String())), connect.CodePermissionDenied)
		require.NoError(t, disable(integrationtests_test.WithUserId("callers-"+mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER.String())))
	})

	t.Run("the mask sent back is refused, on create and on update", func(t *testing.T) {
		_, err := g.accountHooks.CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
			AccountId: g.accountId,
			Hook: &mgmtv1alpha1.NewAccountHook{
				Name: "masked", Description: "masked", Events: hook.GetEvents(), Config: webhookOf("https://example.com", maskedSecret),
			},
		}))
		requireConnectError(t, err, connect.CodeInvalidArgument)
		require.ErrorContains(t, err, "the secret of the webhook is masked")

		_, err = g.accountHooks.UpdateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateAccountHookRequest{
			Id: hook.GetId(), Name: "signed", Description: "changed", Events: hook.GetEvents(),
			Config: webhookOf("https://example.com", maskedSecret),
		}))
		requireConnectError(t, err, connect.CodeInvalidArgument)
		require.ErrorContains(t, err, "the secret of the webhook is masked")
		require.Equal(t, []string{"foo", "foo", "foo"}, secrets(g.accountHooks), "the stored secret stands")
	})

	// Whoever is not of the account, or is of it without a role, is answered as for what
	// does not exist: the same code, the same message, and nothing is changed.
	outsiders := map[string]integrationtests_test.ClientConfigOption{
		"a person of another account": integrationtests_test.WithUserId("callers-outsider"),
		"a member without a role":     s.addMember(g, "callers-no-role", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED),
	}
	s.setUser(ctx, clients.Users(outsiders["a person of another account"]))
	for name, outsider := range outsiders {
		t.Run(name+" learns nothing of a hook", func(t *testing.T) {
			accountHooks := clients.AccountHooks(outsider)
			jobs := clients.Jobs(outsider)
			sameAnswer := func(existing, missing error) {
				t.Helper()
				require.Error(t, existing)
				require.Error(t, missing)
				require.Equal(t, connect.CodeNotFound, connect.CodeOf(existing))
				require.Equal(t, connect.CodeOf(missing), connect.CodeOf(existing))
				require.Equal(t, missing.Error(), existing.Error())
			}

			_, existing := accountHooks.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: hook.GetId()}))
			_, missing := accountHooks.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: uuid.NewString()}))
			sameAnswer(existing, missing)

			_, existing = accountHooks.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: hook.GetId()}))
			_, missing = accountHooks.SetAccountHookEnabled(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountHookEnabledRequest{Id: uuid.NewString()}))
			sameAnswer(existing, missing)

			_, existing = jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: jobHook.GetId()}))
			_, missing = jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: uuid.NewString()}))
			sameAnswer(existing, missing)

			_, existing = jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: g.job.GetId()}))
			_, missing = jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: uuid.NewString()}))
			sameAnswer(existing, missing)

			deletedHook, err := accountHooks.DeleteAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteAccountHookRequest{Id: hook.GetId()}))
			requireNoErrResp(t, deletedHook, err)
			deletedJobHook, err := jobs.DeleteJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.DeleteJobHookRequest{Id: jobHook.GetId()}))
			requireNoErrResp(t, deletedJobHook, err)

			still, err := g.accountHooks.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: hook.GetId()}))
			requireNoErrResp(t, still, err)
			stillJobHook, err := g.jobs.GetJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHookRequest{Id: jobHook.GetId()}))
			requireNoErrResp(t, stillJobHook, err)
		})
	}
}

// What a run asks of the hooks: those that are on, of the timing or the event asked, in
// order; and, without authentication, the worker still reads the secret it signs with.
func (s *IntegrationTestSuite) Test_Hooks_WhatTheWorkerAsks() {
	t := s.T()
	ctx := s.ctx
	g := s.newHookGround("worker")
	worker := integrationtests_test.WithUserId(apikey.NewV1WorkerKey())
	clients := s.OSSAuthenticatedLicensedClients

	t.Run("job hooks by timing, in the order they run", func(t *testing.T) {
		pre := &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{}}
		post := &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{}}
		create := func(name string, priority uint32, enabled bool, timing *mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing) string {
			resp, err := g.jobs.CreateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
				JobId: g.job.GetId(),
				Hook: &mgmtv1alpha1.NewJobHook{
					Name: name, Description: "ordered", Enabled: enabled, Priority: priority,
					Config: &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{
						Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{Query: "select 1;", ConnectionId: g.source.GetId(), Timing: timing},
					}},
				},
			}))
			requireNoErrResp(t, resp, err)
			return resp.Msg.GetHook().GetId()
		}
		last := create("pre-last", 90, true, pre)
		firstOlder := create("pre-first-older", 5, true, pre)
		firstNewer := create("pre-first-newer", 5, true, pre)
		off := create("pre-off", 0, false, pre)
		after := create("post", 0, true, post)

		ids := func(hooks []*mgmtv1alpha1.JobHook) []string {
			out := []string{}
			for _, hook := range hooks {
				out = append(out, hook.GetId())
			}
			return out
		}
		active := func(timing mgmtv1alpha1.GetActiveJobHooksByTimingRequest_Timing) []string {
			resp, err := clients.Jobs(worker).GetActiveJobHooksByTiming(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveJobHooksByTimingRequest{
				JobId: g.job.GetId(), Timing: timing,
			}))
			requireNoErrResp(t, resp, err)
			return ids(resp.Msg.GetHooks())
		}
		require.Equal(t, []string{firstOlder, firstNewer, last}, active(mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC))
		require.Equal(t, []string{after}, active(mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC))
		require.Equal(t, []string{after, firstOlder, firstNewer, last}, active(mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_UNSPECIFIED))

		list, err := g.jobs.GetJobHooks(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobHooksRequest{JobId: g.job.GetId()}))
		requireNoErrResp(t, list, err)
		require.Equal(t, []string{off, after, firstOlder, firstNewer, last}, ids(list.Msg.GetHooks()), "listed in the order they run")
	})

	t.Run("account hooks by event", func(t *testing.T) {
		failed := mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED
		succeeded := mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED
		every := mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED
		onFailed := s.createAccountHook_Webhook(ctx, t, g.accountHooks, g.accountId, "on-failed", []mgmtv1alpha1.AccountHookEvent{failed}, true).GetId()
		onEvery := s.createAccountHook_Webhook(ctx, t, g.accountHooks, g.accountId, "on-every", []mgmtv1alpha1.AccountHookEvent{every}, true).GetId()
		s.createAccountHook_Webhook(ctx, t, g.accountHooks, g.accountId, "on-failed-off", []mgmtv1alpha1.AccountHookEvent{failed}, false)
		onBoth := s.createAccountHook_Webhook(ctx, t, g.accountHooks, g.accountId, "on-both", []mgmtv1alpha1.AccountHookEvent{every, failed}, true).GetId()

		active := func(event mgmtv1alpha1.AccountHookEvent) []string {
			resp, err := clients.AccountHooks(worker).GetActiveAccountHooksByEvent(ctx, connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
				AccountId: g.accountId, Event: event,
			}))
			requireNoErrResp(t, resp, err)
			out := []string{}
			for _, hook := range resp.Msg.GetHooks() {
				require.Equal(t, "foo", hook.GetConfig().GetWebhook().GetSecret())
				out = append(out, hook.GetId())
			}
			return out
		}
		require.Equal(t, []string{onFailed, onEvery, onBoth}, active(failed))
		require.Equal(t, []string{onEvery, onBoth}, active(succeeded))
		require.Equal(t, []string{onEvery, onBoth}, active(every), "no event in particular gives the hooks of every event")

		got, err := clients.AccountHooks(worker).GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: onFailed}))
		requireNoErrResp(t, got, err)
		require.Equal(t, "foo", got.Msg.GetHook().GetConfig().GetWebhook().GetSecret())
	})

	t.Run("without authentication, the worker reads the secret", func(t *testing.T) {
		open := s.OSSUnauthenticatedLicensedClients
		accountId := s.createPersonalAccount(ctx, open.Users())
		hook := s.createAccountHook_Webhook(ctx, t, open.AccountHooks(), accountId, "open",
			[]mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED}, true)
		got, err := open.AccountHooks().GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: hook.GetId()}))
		requireNoErrResp(t, got, err)
		require.Equal(t, "foo", got.Msg.GetHook().GetConfig().GetWebhook().GetSecret())
	})
}
