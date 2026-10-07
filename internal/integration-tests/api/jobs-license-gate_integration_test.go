package integrationtests_test

import (
	"bytes"
	"log"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const subsettingRefusal = "this job uses features the license does not include: subsetting"

// gateGround is an account of the expiring mode, whose license a test restricts, with the two
// connections a job needs.
type gateGround struct {
	accountId   string
	users       mgmtv1alpha1connect.UserAccountServiceClient
	jobs        mgmtv1alpha1connect.JobServiceClient
	source      *mgmtv1alpha1.Connection
	destination *mgmtv1alpha1.Connection
}

// newGateGround makes the ground of a test, and has the license allow every feature again, as a
// key that names no feature list does, when the test ends.
func (s *IntegrationTestSuite) newGateGround(name string) *gateGround {
	userOpt := integrationtests_test.WithUserId(name)
	connections := s.OSSAuthenticatedExpiringClients.Connections(userOpt)
	g := &gateGround{
		users: s.OSSAuthenticatedExpiringClients.Users(userOpt),
		jobs:  s.OSSAuthenticatedExpiringClients.Jobs(userOpt),
	}
	s.T().Cleanup(s.Mocks.ExpiringLicense.ClearFeatures)
	s.setUser(s.ctx, g.users)
	g.accountId = s.createPersonalAccount(s.ctx, g.users)
	g.source = s.createPostgresConnection(connections, g.accountId, name+"-source", "test")
	g.destination = s.createPostgresConnection(connections, g.accountId, name+"-destination", "test2")
	return g
}

// closeFeature leaves the license every feature but one.
func (s *IntegrationTestSuite) closeFeature(closed license.Feature) {
	s.Mocks.ExpiringLicense.SetFeatures(everyFeatureBut(closed)...)
}

// sourceWhere is the source of the ground's jobs: one table, subset by the clause if there is one.
func (g *gateGround) sourceWhere(where *string) *mgmtv1alpha1.JobSource {
	return &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
		Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			ConnectionId: g.source.GetId(),
			Schemas: []*mgmtv1alpha1.PostgresSourceSchemaOption{{
				Schema: "public",
				Tables: []*mgmtv1alpha1.PostgresSourceTableOption{{Table: "users", WhereClause: where}},
			}},
		}},
	}}
}

// jobRequest asks for a job that uses no licensed feature: a test adds the one it is about.
func (g *gateGround) jobRequest(name string) *mgmtv1alpha1.CreateJobRequest {
	return &mgmtv1alpha1.CreateJobRequest{
		AccountId: g.accountId,
		JobName:   name,
		Mappings:  []*mgmtv1alpha1.JobMapping{passthroughMapping("name")},
		Source:    g.sourceWhere(nil),
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: g.destination.GetId(),
			Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
				PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
			}},
		}},
	}
}

// subsettingJobRequest asks for a job that subsets its table.
func (g *gateGround) subsettingJobRequest(name string) *mgmtv1alpha1.CreateJobRequest {
	where := "id > 10"
	req := g.jobRequest(name)
	req.Source = g.sourceWhere(&where)
	return req
}

func passthroughMapping(column string) *mgmtv1alpha1.JobMapping {
	return columnMapping(column, &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	})
}

func columnMapping(column string, config *mgmtv1alpha1.TransformerConfig) *mgmtv1alpha1.JobMapping {
	return &mgmtv1alpha1.JobMapping{
		Schema:      "public",
		Table:       "users",
		Column:      column,
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: config},
	}
}

// requireJobRefusal is the refusal of a job for what it uses: it names the features.
func requireJobRefusal(t testing.TB, err error, features string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, "this job uses features the license does not include: "+features)
}

// requireFeatureRefusal is the refusal of an operation the license does not include.
func requireFeatureRefusal(t testing.TB, err error, feature license.Feature) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, "this license does not include "+string(feature))
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
	g := s.newGateGround("closed-feature-run")
	jobs, accountId := g.jobs, g.accountId
	job := s.createJobUnderValidLicense(t, jobs, g.subsettingJobRequest("closed-feature-run-job"))

	s.closeFeature(license.FeatureSubsetting)

	_, err := jobs.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
	requireJobRefusal(t, err, "subsetting")
	// The mock is the whole suite's: only a run of this job, in this account, is this test's.
	s.Mocks.TemporalClientManager.AssertNotCalled(
		t, "StartScheduledRun", mock.Anything, accountId, job.GetId(), mock.Anything,
	)

	// The same job starts once the license includes the feature.
	s.Mocks.ExpiringLicense.ClearFeatures()
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
	g := s.newGateGround("closed-feature-status")
	users, accountId := g.users, g.accountId
	jobId := s.createJobUnderValidLicense(t, g.jobs, g.subsettingJobRequest("closed-feature-status-job")).GetId()

	forTheJob, err := users.IsAccountStatusValid(ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
		AccountId: accountId, JobId: &jobId,
	}))
	requireNoErrResp(t, forTheJob, err)
	require.True(t, forTheJob.Msg.GetIsValid(), "the license includes every feature")

	s.closeFeature(license.FeatureSubsetting)

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

// A job the API cannot find decides nothing: the run is not held back for it, the answer is the
// one of the account, and a warning says so. (A gate that could not answer, on a database that
// fails, is another matter: the call fails then, which the unit test of the handler covers.)
func (s *IntegrationTestSuite) Test_IsAccountStatusValid_AJobItCannotFindDoesNotRefuse() {
	t := s.T()
	ctx := s.ctx
	userOpt := integrationtests_test.WithUserId("unreadable-job-status")
	users := s.OSSAuthenticatedExpiringClients.Users(userOpt)
	s.setUser(ctx, users)
	accountId := s.createPersonalAccount(ctx, users)
	t.Cleanup(s.Mocks.ExpiringLicense.ClearFeatures)
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

	require.Contains(t, logs.String(), `"level":"WARN"`)
	require.NotContains(t, logs.String(), `"level":"ERROR"`)
	require.Contains(t, logs.String(), unknownJobId)
}
