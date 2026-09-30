package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// CreateJobRun hands back the run it started; with one already going, it starts none and says
// which is going.
func (s *IntegrationTestSuite) Test_CreateJobRun_ReturnsTheRun() {
	t := s.T()
	ctx := s.ctx
	accountId := s.createPersonalAccount(ctx, s.OSSUnauthenticatedLicensedClients.Users())
	connclient := s.OSSUnauthenticatedLicensedClients.Connections()
	jobclient := s.OSSUnauthenticatedLicensedClients.Jobs()
	source := s.createPostgresConnection(connclient, accountId, "source", "test")
	destination := s.createPostgresConnection(connclient, accountId, "destination", "test2")
	s.MockTemporalForCreateJob("create-run-job")
	created, err := jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   "create-run-job",
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
	}))
	requireNoErrResp(t, created, err)
	job := created.Msg.GetJob()
	jobRunId := job.GetId() + "-2026-09-30T10:00:00Z"

	s.Mocks.TemporalClientManager.EXPECT().
		StartScheduledRun(mock.Anything, accountId, job.GetId(), mock.Anything).
		Return(jobRunId, nil).Once()
	// The visibility index, through which runs are found, sees a new one a moment late.
	s.Mocks.TemporalClientManager.EXPECT().
		DescribeWorklowExecution(mock.Anything, accountId, jobRunId, mock.Anything).
		Return(nil, husonymerrors.NewNotFound("workflow not found for "+jobRunId)).Once()
	s.MockTemporalForDescribeWorkflowExecution(accountId, job.GetId(), jobRunId, "Workflow")
	resp, err := jobclient.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
	requireNoErrResp(t, resp, err)
	require.Equal(t, jobRunId, resp.Msg.GetJobRun().GetId())

	// Still not visible when the wait ends: the run started all the same, and what is known of
	// it is returned.
	laterRunId := job.GetId() + "-2026-09-30T10:05:00Z"
	s.Mocks.TemporalClientManager.EXPECT().
		StartScheduledRun(mock.Anything, accountId, job.GetId(), mock.Anything).
		Return(laterRunId, nil).Once()
	s.Mocks.TemporalClientManager.EXPECT().
		DescribeWorklowExecution(mock.Anything, accountId, laterRunId, mock.Anything).
		Return(nil, husonymerrors.NewNotFound("workflow not found for "+laterRunId))
	resp, err = jobclient.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
	requireNoErrResp(t, resp, err)
	require.Equal(t, laterRunId, resp.Msg.GetJobRun().GetId())
	require.Equal(t, job.GetId(), resp.Msg.GetJobRun().GetJobId())

	s.Mocks.TemporalClientManager.EXPECT().
		StartScheduledRun(mock.Anything, accountId, job.GetId(), mock.Anything).
		Return("", &clientmanager.RunInProgressError{WorkflowId: jobRunId}).Once()
	_, err = jobclient.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: job.GetId()}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, jobRunId)
}
