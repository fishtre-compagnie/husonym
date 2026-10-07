package integrationtests_test

import (
	"fmt"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// capGround is an account of the expiring mode, whose license a test caps, where jobs can be
// created on as many sources as the test adds.
type capGround struct {
	*gateGround
	connections mgmtv1alpha1connect.ConnectionServiceClient
}

func (s *IntegrationTestSuite) newCapGround(name string) *capGround {
	g := s.newGateGround(name)
	s.allowJobCreation(g.accountId)
	return &capGround{
		gateGround:  g,
		connections: s.OSSAuthenticatedExpiringClients.Connections(integrationtests_test.WithUserId(name)),
	}
}

// allowJobCreation has the orchestrator answer the creation of jobs in the account, however many
// there are: a creation the cap refuses asks for the namespace and the queue but creates no
// schedule, so the calls cannot be counted ahead. The mock is the whole suite's: these answers
// are the account's alone, and are taken back when the test ends.
func (s *IntegrationTestSuite) allowJobCreation(accountId string) {
	temporal := s.Mocks.TemporalClientManager
	calls := []*mock.Call{
		temporal.On("DoesAccountHaveNamespace", mock.Anything, accountId, mock.Anything).Return(true, nil).Maybe(),
		temporal.On("GetSyncJobTaskQueue", mock.Anything, accountId, mock.Anything).Return("sync-job", nil).Maybe(),
		temporal.On("CreateSchedule", mock.Anything, accountId, mock.Anything, mock.Anything).Return("schedule", nil).Maybe(),
	}
	s.T().Cleanup(func() {
		for _, call := range calls {
			call.Unset()
		}
	})
}

// setLimits gives the license of the expiring mode these limits, and puts back the ones it had
// when the test ends.
func (s *IntegrationTestSuite) setLimits(limits *license.Limits) {
	previous := s.Mocks.ExpiringLicense.Limits()
	s.T().Cleanup(func() { s.Mocks.ExpiringLicense.SetLimits(previous) })
	s.Mocks.ExpiringLicense.SetLimits(limits)
}

// capSources caps the sources of the instance.
func (s *IntegrationTestSuite) capSources(maxSources int) {
	s.setLimits(&license.Limits{MaxSources: &maxSources})
}

// anotherSource adds a PostgreSQL connection to the account: a source no job reads yet.
func (s *IntegrationTestSuite) anotherSource(g *capGround, name string) *mgmtv1alpha1.Connection {
	return s.createPostgresConnection(g.connections, g.accountId, name, name)
}

func postgresSource(connection *mgmtv1alpha1.Connection) *mgmtv1alpha1.JobSource {
	return &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
		Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			ConnectionId: connection.GetId(),
		}},
	}}
}

// jobOn asks for a job of the ground that reads the given connection.
func (g *capGround) jobOn(name string, source *mgmtv1alpha1.Connection) *mgmtv1alpha1.CreateJobRequest {
	req := g.jobRequest(name)
	req.Source = postgresSource(source)
	return req
}

func (s *IntegrationTestSuite) createJob(g *capGround, req *mgmtv1alpha1.CreateJobRequest) (*mgmtv1alpha1.Job, error) {
	resp, err := g.jobs.CreateJob(s.ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetJob(), nil
}

func (s *IntegrationTestSuite) mustCreateJob(g *capGround, req *mgmtv1alpha1.CreateJobRequest) *mgmtv1alpha1.Job {
	s.T().Helper()
	job, err := s.createJob(g, req)
	require.NoError(s.T(), err)
	return job
}

// moveJob gives the job another source, and leaves its mappings as the ground's jobs have them.
func (s *IntegrationTestSuite) moveJob(g *capGround, jobId string, source *mgmtv1alpha1.Connection) error {
	_, err := g.jobs.UpdateJobSourceConnection(s.ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
		Id:       jobId,
		Source:   postgresSource(source),
		Mappings: []*mgmtv1alpha1.JobMapping{passthroughMapping("name")},
	}))
	return err
}

// sourceConnectionOf tells which connection the stored job reads.
func (s *IntegrationTestSuite) sourceConnectionOf(g *capGround, jobId string) string {
	return s.storedJob(g.gateGround, jobId).GetSource().GetOptions().GetPostgres().GetConnectionId()
}

func sourceCapRefusal(maxSources, wouldBe int) string {
	return fmt.Sprintf(
		"this license allows %d source(s) and this change would bring the instance to %d; contact us to raise the limit",
		maxSources, wouldBe,
	)
}

// requireSourceCapRefusal is the refusal of a change that adds a source beyond the cap: it gives
// the cap and what the instance would come to.
func requireSourceCapRefusal(t testing.TB, err error, maxSources, wouldBe int) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, sourceCapRefusal(maxSources, wouldBe))
}

// The cap is the instance's: a source read in one account leaves no room for one in another.
func (s *IntegrationTestSuite) Test_CreateJob_RefusesBeyondTheSourceCap() {
	t := s.T()
	first := s.newCapGround("source-cap-first")
	second := s.newCapGround("source-cap-second")
	s.capSources(1)

	s.mustCreateJob(first, first.jobOn("within-the-cap", first.source))

	_, err := s.createJob(second, second.jobOn("beyond-the-cap", second.source))
	requireSourceCapRefusal(t, err, 1, 2)
	// What was refused was not written.
	stored, err := second.jobs.GetJobs(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobsRequest{AccountId: second.accountId}))
	requireNoErrResp(t, stored, err)
	require.Empty(t, stored.Msg.GetJobs())

	// The same job is created once the cap has room for its source.
	s.capSources(2)
	s.mustCreateJob(second, second.jobOn("beyond-the-cap", second.source))
}

// A job on a source another job already reads adds nothing to the count, at the cap and over it.
func (s *IntegrationTestSuite) Test_CreateJob_OnAnAlreadyCountedSourceIsAllowed() {
	g := s.newCapGround("source-cap-counted")
	s.capSources(1)
	s.mustCreateJob(g, g.jobOn("first-reader", g.source))
	s.mustCreateJob(g, g.jobOn("second-reader", g.source))

	// The instance comes to two sources under a key that allows them, then gets one that does not.
	s.capSources(2)
	s.mustCreateJob(g, g.jobOn("other-source", s.anotherSource(g, "other")))
	s.capSources(1)
	s.mustCreateJob(g, g.jobOn("third-reader", g.source))
}

// Giving a job a source no job reads adds one, as long as its old source is still read.
func (s *IntegrationTestSuite) Test_UpdateJobSourceConnection_ToANewSourceIsRefused() {
	t := s.T()
	g := s.newCapGround("source-cap-move")
	other := s.anotherSource(g, "other")
	s.mustCreateJob(g, g.jobOn("stays", g.source))
	moved := s.mustCreateJob(g, g.jobOn("moves", g.source))
	s.capSources(1)

	requireSourceCapRefusal(t, s.moveJob(g, moved.GetId(), other), 1, 2)
	require.Equal(t, g.source.GetId(), s.sourceConnectionOf(g, moved.GetId()), "a refused change writes nothing")

	s.capSources(2)
	require.NoError(t, s.moveJob(g, moved.GetId(), other))
	require.Equal(t, other.GetId(), s.sourceConnectionOf(g, moved.GetId()))
}

// The only job of a source that moves to another frees the one it leaves: at the cap, the
// instance has as many sources after the change as before.
func (s *IntegrationTestSuite) Test_UpdateJobSourceConnection_SwappingTheOnlySourceAtTheCapIsAllowed() {
	t := s.T()
	g := s.newCapGround("source-cap-swap")
	other := s.anotherSource(g, "other")
	job := s.mustCreateJob(g, g.jobOn("swaps", g.source))
	s.capSources(1)

	require.NoError(t, s.moveJob(g, job.GetId(), other))
	require.Equal(t, other.GetId(), s.sourceConnectionOf(g, job.GetId()))
}

// A MySQL connection serves several databases, and each one a job maps is a source: mapping a
// table of another database adds one, mapping more of the same database does not.
func (s *IntegrationTestSuite) Test_UpdateJobSourceConnection_AddingAMysqlSchemaIsRefused() {
	t := s.T()
	g := s.newCapGround("source-cap-mysql")
	mysqlConnection := func(name string) *mgmtv1alpha1.Connection {
		resp, err := g.connections.CreateConnection(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
			AccountId: g.accountId,
			Name:      name,
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{
				MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.MysqlConnectionConfig_Url{Url: "mysql://user:pass@localhost:3306/" + name},
				},
			}},
		}))
		requireNoErrResp(t, resp, err)
		return resp.Msg.GetConnection()
	}
	source, destination := mysqlConnection("mysql-source"), mysqlConnection("mysql-destination")
	mysqlSource := &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
		Config: &mgmtv1alpha1.JobSourceOptions_Mysql{Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
			ConnectionId: source.GetId(),
		}},
	}}
	mapping := func(schema, column string) *mgmtv1alpha1.JobMapping {
		m := passthroughMapping(column)
		m.Schema = schema
		return m
	}
	job := s.mustCreateJob(g, &mgmtv1alpha1.CreateJobRequest{
		AccountId: g.accountId,
		JobName:   "one-database",
		Mappings:  []*mgmtv1alpha1.JobMapping{mapping("shop", "name")},
		Source:    mysqlSource,
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: destination.GetId(),
			Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_MysqlOptions{
				MysqlOptions: &mgmtv1alpha1.MysqlDestinationConnectionOptions{},
			}},
		}},
	})
	update := func(mappings ...*mgmtv1alpha1.JobMapping) error {
		_, err := g.jobs.UpdateJobSourceConnection(s.ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
			Id: job.GetId(), Source: mysqlSource, Mappings: mappings,
		}))
		return err
	}
	schemasOf := func() []string {
		var schemas []string
		for _, m := range s.storedJob(g.gateGround, job.GetId()).GetMappings() {
			schemas = append(schemas, m.GetSchema())
		}
		return schemas
	}
	s.capSources(1)

	requireSourceCapRefusal(t, update(mapping("shop", "name"), mapping("crm", "name")), 1, 2)
	require.Equal(t, []string{"shop"}, schemasOf(), "a refused change writes nothing")

	require.NoError(t, update(mapping("shop", "name"), mapping("shop", "email")))
	require.Equal(t, []string{"shop", "shop"}, schemasOf())

	s.capSources(2)
	require.NoError(t, update(mapping("shop", "name"), mapping("crm", "name")))
	require.Equal(t, []string{"shop", "crm"}, schemasOf())
}

// The cap is only looked at when a source is added. An instance that a newer key leaves over
// its cap keeps running what it has: nothing is refused when a run starts.
func (s *IntegrationTestSuite) Test_SourceCap_NeverRefusesARun() {
	t := s.T()
	g := s.newCapGround("source-cap-run")
	job := s.mustCreateJob(g, g.jobOn("first", g.source))
	s.mustCreateJob(g, g.jobOn("second", s.anotherSource(g, "second")))
	s.mustCreateJob(g, g.jobOn("third", s.anotherSource(g, "third")))
	s.capSources(1)

	jobId := job.GetId()
	jobRunId := jobId + "-2026-10-07T10:00:00Z"
	s.Mocks.TemporalClientManager.EXPECT().
		StartScheduledRun(mock.Anything, g.accountId, jobId, mock.Anything).
		Return(jobRunId, nil).Once()
	s.MockTemporalForDescribeWorkflowExecution(g.accountId, jobId, jobRunId, "Workflow")
	started, err := g.jobs.CreateJobRun(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: jobId}))
	requireNoErrResp(t, started, err)
	require.Equal(t, jobRunId, started.Msg.GetJobRun().GetId())

	status, err := g.users.IsAccountStatusValid(s.ctx, connect.NewRequest(&mgmtv1alpha1.IsAccountStatusValidRequest{
		AccountId: g.accountId, JobId: &jobId,
	}))
	requireNoErrResp(t, status, err)
	require.True(t, status.Msg.GetIsValid(), status.Msg.GetReason())
}

// An instance over its cap still changes its jobs and gives sources up: only what would add a
// source is refused, and the refusal counts the instance as it would be.
func (s *IntegrationTestSuite) Test_SourceCap_AnInstanceOverItStillEditsAndRemoves() {
	t := s.T()
	g := s.newCapGround("source-cap-over")
	second, third := s.anotherSource(g, "second"), s.anotherSource(g, "third")
	first := s.mustCreateJob(g, g.jobOn("first", g.source))
	s.mustCreateJob(g, g.jobOn("second", second))
	last := s.mustCreateJob(g, g.jobOn("third", third))
	s.capSources(1)

	// A change that keeps the source of the job.
	require.NoError(t, s.moveJob(g, first.GetId(), g.source))
	// A change that gives a source up: the third is no longer read.
	require.NoError(t, s.moveJob(g, last.GetId(), g.source))
	require.Equal(t, g.source.GetId(), s.sourceConnectionOf(g, last.GetId()))

	// Reading it again would add it to the two that are left.
	requireSourceCapRefusal(t, s.moveJob(g, last.GetId(), third), 1, 3)
	_, err := s.createJob(g, g.jobOn("third-again", third))
	requireSourceCapRefusal(t, err, 1, 3)
}

// Only a synchronization job reads a source: a job that generates data, or that detects PII,
// is created at the cap on any connection, and becomes a source the day it synchronizes.
func (s *IntegrationTestSuite) Test_SourceCap_GenerationAndPiiDetectJobsAddNoSource() {
	t := s.T()
	g := s.newCapGround("source-cap-types")
	other := s.anotherSource(g, "other")
	s.mustCreateJob(g, g.jobOn("the-source", g.source))
	s.capSources(1)

	generation := g.jobRequest("generation")
	generation.Source = &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
		Config: &mgmtv1alpha1.JobSourceOptions_Generate{Generate: &mgmtv1alpha1.GenerateSourceOptions{
			Schemas: []*mgmtv1alpha1.GenerateSourceSchemaOption{{
				Schema: "public",
				Tables: []*mgmtv1alpha1.GenerateSourceTableOption{{Table: "users", RowCount: 1}},
			}},
		}},
	}}
	generation.Mappings = []*mgmtv1alpha1.JobMapping{generateBoolMapping("name")}
	s.mustCreateJob(g, generation)

	detection := g.jobOn("detection", other)
	detection.JobType = piiDetectJobType()
	detecting := s.mustCreateJob(g, detection)

	// A change that names no type keeps the one the job has: it still reads no source.
	require.NoError(t, s.moveJob(g, detecting.GetId(), other))

	synchronizing := &mgmtv1alpha1.UpdateJobSourceConnectionRequest{
		Id:       detecting.GetId(),
		Source:   postgresSource(other),
		Mappings: []*mgmtv1alpha1.JobMapping{passthroughMapping("name")},
		JobType:  syncJobType(),
	}
	_, err := g.jobs.UpdateJobSourceConnection(s.ctx, connect.NewRequest(synchronizing))
	requireSourceCapRefusal(t, err, 1, 2)
	require.NotNil(t, s.storedJob(g.gateGround, detecting.GetId()).GetJobType().GetPiiDetect())
}

// Two creations that each see room for their source must not both take it: the count and the
// write of one are done before the other counts.
func (s *IntegrationTestSuite) Test_SourceCap_TwoCreationsAtOnceDoNotExceedIt() {
	t := s.T()
	g := s.newCapGround("source-cap-race")
	other := s.anotherSource(g, "other")
	s.capSources(1)

	// The race is not lost every time it is run without its guard: it is run several times, each
	// on an instance that has no source.
	for round := range 10 {
		start := make(chan struct{})
		results := make(chan error, 2)
		for _, source := range []*mgmtv1alpha1.Connection{g.source, other} {
			req := g.jobOn(fmt.Sprintf("race-%d-%s", round, source.GetName()), source)
			go func() {
				<-start
				_, err := s.createJob(g, req)
				results <- err
			}()
		}
		close(start)

		var refusals []error
		for range 2 {
			if err := <-results; err != nil {
				refusals = append(refusals, err)
			}
		}
		require.Len(t, refusals, 1, "round %d: one of the two creations is refused, and only one", round)
		requireSourceCapRefusal(t, refusals[0], 1, 2)

		rows, err := s.HusonymQuerier.ListJobSourcesOfInstance(s.ctx, s.Pgcontainer.DB)
		require.NoError(t, err)
		require.Len(t, rows, 1, "round %d: the instance has the one job that was created, and its source", round)
		for _, row := range rows {
			require.NoError(t, s.HusonymQuerier.RemoveJobById(s.ctx, s.Pgcontainer.DB, row.ID))
		}
	}
}

// A license that caps other things and not the sources does not cap them: no cap is not a cap
// of zero.
func (s *IntegrationTestSuite) Test_SourceCap_NilCapIsUncapped() {
	t := s.T()
	g := s.newCapGround("source-cap-none")
	maxJobs := 10
	s.setLimits(&license.Limits{MaxJobs: &maxJobs})

	job := s.mustCreateJob(g, g.jobOn("first", g.source))
	s.mustCreateJob(g, g.jobOn("second", s.anotherSource(g, "second")))
	require.NoError(t, s.moveJob(g, job.GetId(), s.anotherSource(g, "third")))

	// Neither does a license that carries no limit at all.
	s.setLimits(nil)
	s.mustCreateJob(g, g.jobOn("fourth", s.anotherSource(g, "fourth")))
}
