package integrationtests_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// usageGround is a team account of the expiring mode, where jobs can be created, with the
// clients of its administrator for everything a licensed feature is configured through.
type usageGround struct {
	*capGround
	adminId      string
	accountHooks mgmtv1alpha1connect.AccountHookServiceClient
	settings     mgmtv1alpha1connect.AccountSettingServiceClient
	roles        *rolesGround
}

// newUsageGround makes the ground of a test, and leaves the license of the expiring mode valid
// and with every feature when the test ends.
func (s *IntegrationTestSuite) newUsageGround(name string) *usageGround {
	admin := integrationtests_test.WithUserId(name + "-admin")
	clients := s.OSSAuthenticatedExpiringClients
	s.T().Cleanup(func() {
		s.Mocks.ExpiringLicense.SetValid(true)
		s.Mocks.ExpiringLicense.ClearFeatures()
	})

	users := clients.Users(admin)
	adminId := s.setUser(s.ctx, users)
	accountId := s.createTeamAccount(s.ctx, users, name+"-"+uuid.NewString())
	s.allowJobCreation(accountId)
	connections := clients.Connections(admin)
	return &usageGround{
		capGround: &capGround{
			gateGround: &gateGround{
				accountId:   accountId,
				users:       users,
				jobs:        clients.Jobs(admin),
				source:      s.createPostgresConnection(connections, accountId, name+"-source", "test"),
				destination: s.createPostgresConnection(connections, accountId, name+"-destination", "test2"),
			},
			connections: connections,
		},
		adminId:      adminId,
		accountHooks: clients.AccountHooks(admin),
		settings:     clients.AccountSettings(admin),
		// addMember needs the account and the client of its administrator, and nothing else.
		roles: &rolesGround{accountId: accountId, admin: users, hooks: &hookGround{accountId: accountId, users: users}},
	}
}

// usage asks, as the administrator of the ground, what its account uses of the license.
func (g *usageGround) usage(s *IntegrationTestSuite) *mgmtv1alpha1.GetLicenseUsageResponse {
	s.T().Helper()
	resp, err := g.users.GetLicenseUsage(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetLicenseUsageRequest{AccountId: g.accountId}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg
}

// The count is the instance's and the list is the account's: each account sees the total, and
// its own source only, under the name of its connection.
func (s *IntegrationTestSuite) Test_GetLicenseUsage_CountsTheInstanceButListsOnlyTheAccount() {
	t := s.T()
	first := s.newUsageGround("usage-count-first")
	second := s.newUsageGround("usage-count-second")

	s.mustCreateJob(first.capGround, first.jobOn("first-reader", first.source))
	// A second job on a source already counted adds nothing.
	s.mustCreateJob(first.capGround, first.jobOn("first-other-reader", first.source))
	s.mustCreateJob(second.capGround, second.jobOn("second-reader", second.source))

	for _, g := range []*usageGround{first, second} {
		usage := g.usage(s)
		require.Equal(t, int32(2), usage.GetSourcesInInstance())
		require.Len(t, usage.GetSourcesInAccount(), 1)
		source := usage.GetSourcesInAccount()[0]
		require.Equal(t, g.source.GetId(), source.GetConnectionId())
		require.Equal(t, g.source.GetName(), source.GetConnectionName())
		require.Empty(t, source.GetDatabase(), "PostgreSQL is counted per connection")
	}
}

// Each feature whose use is stored is listed once the account has the thing that uses it, and
// not before. What another account uses is never listed.
func (s *IntegrationTestSuite) Test_GetLicenseUsage_FeaturesInUse() {
	t := s.T()
	ctx := s.ctx
	quiet := s.newUsageGround("usage-features-quiet")
	busy := s.newUsageGround("usage-features-busy")
	where := "id > 10"
	cron := everyNight

	// A job that uses no licensed feature, in each account.
	s.mustCreateJob(quiet.capGround, quiet.jobRequest("quiet-plain"))
	plain := s.mustCreateJob(busy.capGround, busy.jobRequest("busy-plain"))
	require.Empty(t, quiet.usage(s).GetFeaturesInUse())
	require.Empty(t, busy.usage(s).GetFeaturesInUse())

	jobWith := func(name string, change func(req *mgmtv1alpha1.CreateJobRequest)) func(t *testing.T) {
		return func(*testing.T) {
			req := busy.jobRequest(name)
			change(req)
			s.mustCreateJob(busy.capGround, req)
		}
	}
	steps := []struct {
		feature license.Feature
		add     func(t *testing.T)
	}{
		{license.FeatureJobHooks, func(t *testing.T) {
			_, err := busy.jobs.CreateJobHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobHookRequest{
				JobId: plain.GetId(),
				Hook: &mgmtv1alpha1.NewJobHook{
					Name:    "usage-job-hook",
					Enabled: true,
					Config: &mgmtv1alpha1.JobHookConfig{Config: &mgmtv1alpha1.JobHookConfig_Sql{
						Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
							Query:        "select 1;",
							ConnectionId: busy.source.GetId(),
							Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
								Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
							},
						},
					}},
				},
			}))
			require.NoError(t, err)
		}},
		{license.FeatureAccountHooks, func(t *testing.T) {
			_, err := busy.accountHooks.CreateAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.CreateAccountHookRequest{
				AccountId: busy.accountId,
				Hook: &mgmtv1alpha1.NewAccountHook{
					Name:    "usage-account-hook",
					Enabled: true,
					Events:  []mgmtv1alpha1.AccountHookEvent{mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED},
					Config:  webhookOf("https://example.com", "foo"),
				},
			}))
			require.NoError(t, err)
		}},
		{license.FeaturePiiText, jobWith("busy-pii-text", func(req *mgmtv1alpha1.CreateJobRequest) {
			req.Mappings = []*mgmtv1alpha1.JobMapping{columnMapping("name", piiTextTransformerConfig())}
		})},
		{license.FeaturePiiDetection, jobWith("busy-pii-detection", func(req *mgmtv1alpha1.CreateJobRequest) {
			req.JobType = piiDetectJobType()
		})},
		{license.FeatureCustomTransformers, jobWith("busy-javascript", func(req *mgmtv1alpha1.CreateJobRequest) {
			req.Mappings = []*mgmtv1alpha1.JobMapping{javascriptMapping("name")}
		})},
		{license.FeatureSubsetting, jobWith("busy-subset", func(req *mgmtv1alpha1.CreateJobRequest) {
			req.Source = busy.sourceWhere(&where)
		})},
		{license.FeatureScheduling, jobWith("busy-scheduled", func(req *mgmtv1alpha1.CreateJobRequest) {
			req.CronSchedule = &cron
		})},
		{license.FeatureApiKeys, func(t *testing.T) {
			// The integration mux serves no API-key service: the key is stored as that service
			// stores it.
			accountUuid, err := husonymdb.ToUuid(busy.accountId)
			require.NoError(t, err)
			adminUuid, err := husonymdb.ToUuid(busy.adminId)
			require.NoError(t, err)
			_, err = husonymdb.New(s.Pgcontainer.DB, s.HusonymQuerier).CreateAccountApikey(ctx, &husonymdb.CreateAccountApiKeyRequest{
				KeyName: "usage-key", KeyValue: uuid.NewString(), AccountUuid: accountUuid, CreatedByUserUuid: adminUuid,
				ExpiresAt: pgtype.Timestamp{Time: time.Now().Add(time.Hour), Valid: true},
			})
			require.NoError(t, err)
		}},
		{license.FeatureRbac, func(*testing.T) {
			busy.roles.member(s, "usage-features-viewer", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
		}},
		{license.FeatureSso, func(t *testing.T) {
			_, err := busy.settings.SetAccountSetting(ctx, connect.NewRequest(&mgmtv1alpha1.SetAccountSettingRequest{
				AccountId: busy.accountId, Config: oidcSetting(publicIssuer(), "a-client"),
			}))
			require.NoError(t, err)
		}},
	}

	var want []string
	for _, step := range steps {
		t.Run(string(step.feature), func(t *testing.T) {
			require.NotContains(t, busy.usage(s).GetFeaturesInUse(), string(step.feature), "not listed before the account uses it")
			step.add(t)
			want = append(want, string(step.feature))
			// What was listed stays, and the order is the one of the features.
			require.Equal(t, want, busy.usage(s).GetFeaturesInUse())
		})
	}

	t.Run("every feature whose use is stored was shown", func(t *testing.T) {
		var deducible []string
		for _, feature := range license.AllFeatures() {
			switch feature {
			case license.FeatureMcp, license.FeatureMappingReview, license.FeatureRunLogs:
			default:
				deducible = append(deducible, string(feature))
			}
		}
		require.Equal(t, deducible, want)
	})

	t.Run("an account that uses everything says nothing of another", func(t *testing.T) {
		require.Empty(t, quiet.usage(s).GetFeaturesInUse())
	})

	t.Run("an administrator who is the only member, and a member made one, are not rbac", func(t *testing.T) {
		quiet.roles.member(s, "usage-features-second-admin", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN)
		quiet.roles.member(s, "usage-features-no-role", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED)
		require.Empty(t, quiet.usage(s).GetFeaturesInUse())
	})
}

// The usage is what the account can see: nobody outside the account is told it, and a member
// who may only view is.
func (s *IntegrationTestSuite) Test_GetLicenseUsage_NeedsToViewTheAccount() {
	t := s.T()
	g := s.newUsageGround("usage-view")
	s.mustCreateJob(g.capGround, g.jobOn("reader", g.source))
	ask := func(as integrationtests_test.ClientConfigOption) (*connect.Response[mgmtv1alpha1.GetLicenseUsageResponse], error) {
		return s.OSSAuthenticatedExpiringClients.Users(as).GetLicenseUsage(s.ctx, connect.NewRequest(
			&mgmtv1alpha1.GetLicenseUsageRequest{AccountId: g.accountId},
		))
	}

	stranger := integrationtests_test.WithUserId("usage-view-stranger")
	s.setUser(s.ctx, s.OSSAuthenticatedExpiringClients.Users(stranger))
	resp, err := ask(stranger)
	requireErrResp(t, resp, err)
	requireConnectError(t, err, connect.CodePermissionDenied)

	viewer := g.roles.member(s, "usage-view-viewer", mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER)
	resp, err = ask(viewer)
	requireNoErrResp(t, resp, err)
	require.Len(t, resp.Msg.GetSourcesInAccount(), 1)
}

// The usage is told whatever the license: one that is not in force, and one that includes no
// feature, leave the answer what it was. It is the page that shows what the license lacks.
func (s *IntegrationTestSuite) Test_GetLicenseUsage_AnswersWhateverTheLicense() {
	t := s.T()
	g := s.newUsageGround("usage-any-license")
	where := "id > 10"
	req := g.jobRequest("subsets")
	req.Source = g.sourceWhere(&where)
	s.mustCreateJob(g.capGround, req)

	requireUsage := func(t *testing.T) {
		t.Helper()
		usage := g.usage(s)
		require.Equal(t, int32(1), usage.GetSourcesInInstance())
		require.Len(t, usage.GetSourcesInAccount(), 1)
		require.Equal(t, g.source.GetId(), usage.GetSourcesInAccount()[0].GetConnectionId())
		require.Equal(t, []string{string(license.FeatureSubsetting)}, usage.GetFeaturesInUse())
	}
	requireUsage(t)

	t.Run("under a license that includes no feature", func(t *testing.T) {
		s.Mocks.ExpiringLicense.SetFeatures()
		t.Cleanup(s.Mocks.ExpiringLicense.ClearFeatures)
		requireUsage(t)
	})

	t.Run("under a license that is not in force", func(t *testing.T) {
		s.Mocks.ExpiringLicense.SetValid(false)
		t.Cleanup(func() { s.Mocks.ExpiringLicense.SetValid(true) })
		requireUsage(t)
	})
}
