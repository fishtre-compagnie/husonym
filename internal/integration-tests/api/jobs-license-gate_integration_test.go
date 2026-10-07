package integrationtests_test

import (
	"bytes"
	"log"
	"log/slog"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const subsettingRefusal = "this job uses features the license does not include: subsetting"

// createSubsettingJob gives the account a job that subsets one table, under the license of the
// expiring mode, and restores every feature of that license when the test ends.
func (s *IntegrationTestSuite) createSubsettingJob(
	userOpt integrationtests_test.ClientConfigOption,
	accountId, jobName string,
) *mgmtv1alpha1.Job {
	t := s.T()
	connections := s.OSSAuthenticatedExpiringClients.Connections(userOpt)
	jobs := s.OSSAuthenticatedExpiringClients.Jobs(userOpt)
	t.Cleanup(func() { s.Mocks.ExpiringLicense.SetFeatures(license.AllFeatures()...) })

	source := s.createPostgresConnection(connections, accountId, jobName+"-source", "test")
	destination := s.createPostgresConnection(connections, accountId, jobName+"-destination", "test2")
	where := "id > 10"
	s.MockTemporalForCreateJob(jobName)
	created, err := jobs.CreateJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   jobName,
		Mappings: []*mgmtv1alpha1.JobMapping{{
			Schema: "public",
			Table:  "users",
			Column: "name",
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}},
		}},
		Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
				ConnectionId: source.GetId(),
				Schemas: []*mgmtv1alpha1.PostgresSourceSchemaOption{{
					Schema: "public",
					Tables: []*mgmtv1alpha1.PostgresSourceTableOption{{Table: "users", WhereClause: &where}},
				}},
			}},
		}},
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: destination.GetId(),
			Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
				PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
			}},
		}},
	}))
	requireNoErrResp(t, created, err)
	return created.Msg.GetJob()
}

// everyFeatureBut is the list of the licensed features, one left out.
func everyFeatureBut(closed license.Feature) []license.Feature {
	var features []license.Feature
	for _, feature := range license.AllFeatures() {
		if feature != closed {
			features = append(features, feature)
		}
	}
	return features
}

// A person who starts a job that uses a feature the license does not include is told which,
// and nothing starts: the schedule of the job is not triggered.
func (s *IntegrationTestSuite) Test_CreateJobRun_RefusesAJobUsingAClosedFeature() {
	t := s.T()
	ctx := s.ctx
	userOpt := integrationtests_test.WithUserId("closed-feature-run")
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	jobs := s.OSSAuthenticatedExpiringClients.Jobs(userOpt)
	s.setUser(ctx, users)
	accountId := s.createPersonalAccount(ctx, users)
	job := s.createSubsettingJob(userOpt, accountId, "closed-feature-run-job")

	s.Mocks.ExpiringLicense.SetFeatures(everyFeatureBut(license.FeatureSubsetting)...)

	_, err := jobs.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, subsettingRefusal)
	s.Mocks.TemporalClientManager.AssertNotCalled(
		t, "StartScheduledRun", mock.Anything, mock.Anything, mock.Anything, mock.Anything,
	)

	// The same job starts once the license includes the feature.
	s.Mocks.ExpiringLicense.SetFeatures(license.AllFeatures()...)
	jobRunId := job.GetId() + "-2026-10-07T10:00:00Z"
	s.Mocks.TemporalClientManager.EXPECT().
		StartScheduledRun(mock.Anything, accountId, job.GetId(), mock.Anything).
		Return(jobRunId, nil).Once()
	s.MockTemporalForDescribeWorkflowExecution(accountId, job.GetId(), jobRunId, "Workflow")
	started, err := jobs.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
	requireNoErrResp(t, started, err)
	require.Equal(t, jobRunId, started.Msg.GetJobRun().GetId())
}

// The check every run makes when it starts, scheduled ones included, refuses the job the same
// way when it is told which job is about to run. Asked for no job, it answers for the account
// alone, as it always did.
func (s *IntegrationTestSuite) Test_IsAccountStatusValid_RefusesByJob() {
	t := s.T()
	ctx := s.ctx
	userOpt := integrationtests_test.WithUserId("closed-feature-status")
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	s.setUser(ctx, users)
	accountId := s.createPersonalAccount(ctx, users)
	job := s.createSubsettingJob(userOpt, accountId, "closed-feature-status-job")
	jobId := job.GetId()

	forTheJob, err := users.IsAccountStatusValid(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
		AccountId: accountId, JobId: &jobId,
	}))
	requireNoErrResp(t, forTheJob, err)
	require.True(t, forTheJob.Msg.GetIsValid(), "the license includes every feature")

	s.Mocks.ExpiringLicense.SetFeatures(everyFeatureBut(license.FeatureSubsetting)...)

	forTheJob, err = users.IsAccountStatusValid(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
		AccountId: accountId, JobId: &jobId,
	}))
	requireNoErrResp(t, forTheJob, err)
	require.False(t, forTheJob.Msg.GetIsValid())
	require.Equal(t, subsettingRefusal, forTheJob.Msg.GetReason())
	// The account itself is not in any of the states the status names.
	require.Equal(t, mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_REASON_UNSPECIFIED, forTheJob.Msg.GetAccountStatus())
	require.False(t, forTheJob.Msg.GetShouldPoll())

	forTheAccount, err := users.IsAccountStatusValid(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
		AccountId: accountId,
	}))
	requireNoErrResp(t, forTheAccount, err)
	require.True(t, forTheAccount.Msg.GetIsValid())
	require.Nil(t, forTheAccount.Msg.Reason)
	require.Equal(t, mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_REASON_UNSPECIFIED, forTheAccount.Msg.GetAccountStatus())
}

// A job the check cannot read decides nothing: the run is not held back by a failure of the
// gate itself, the answer is the one of the account, and the failure is logged.
func (s *IntegrationTestSuite) Test_IsAccountStatusValid_AJobItCannotReadDoesNotRefuse() {
	t := s.T()
	ctx := s.ctx
	userOpt := integrationtests_test.WithUserId("unreadable-job-status")
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	s.setUser(ctx, users)
	accountId := s.createPersonalAccount(ctx, users)
	t.Cleanup(func() { s.Mocks.ExpiringLicense.SetFeatures(license.AllFeatures()...) })
	// No feature at all: a job that could be read and used one would be refused.
	s.Mocks.ExpiringLicense.SetFeatures()

	// The handlers of the test server log to the default logger. Setting it also points the
	// standard logger at it, which is put back with it.
	var logs bytes.Buffer
	previous, previousOutput, previousFlags := slog.Default(), log.Writer(), log.Flags()
	restore := func() {
		slog.SetDefault(previous)
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	}
	t.Cleanup(restore)
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{})))

	unknownJobId := uuid.NewString()
	resp, err := users.IsAccountStatusValid(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
		AccountId: accountId, JobId: &unknownJobId,
	}))
	restore()
	requireNoErrResp(t, resp, err)
	require.True(t, resp.Msg.GetIsValid())
	require.Nil(t, resp.Msg.Reason)
	require.Equal(t, mgmtv1alpha1.AccountStatus_ACCOUNT_STATUS_REASON_UNSPECIFIED, resp.Msg.GetAccountStatus())

	require.Contains(t, logs.String(), `"level":"ERROR"`)
	require.Contains(t, logs.String(), unknownJobId)
}
