package integrationtests_test

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	preflight_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/preflight/workflow"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	temporalclient "go.temporal.io/sdk/client"
)

// The pre-flight check of a job runs as a workflow of its own on the queue of the account,
// and its report comes back as the workflow returns it. Without a worker, the call says so.
func (s *IntegrationTestSuite) Test_PreflightJob() {
	t := s.T()
	clients := s.OSSUnauthenticatedLicensedClients
	accountId := s.createPersonalAccount(s.ctx, clients.Users())
	srcconn := s.createPostgresConnection(clients.Connections(), accountId, "source", "test")
	destconn := s.createPostgresConnection(clients.Connections(), accountId, "dest", "test2")
	s.MockTemporalForCreateJob("test-id")
	created, err := clients.Jobs().CreateJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   "preflight",
		Mappings: []*mgmtv1alpha1.JobMapping{{
			Schema: "public", Table: "users", Column: "id",
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}},
		}},
		Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
				ConnectionId: srcconn.GetId(),
			}},
		}},
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: destconn.GetId(),
			Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
				PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
			}},
		}},
	}))
	requireNoErrResp(t, created, err)
	jobId := created.Msg.GetJob().GetId()

	report := &mgmtv1alpha1.PreflightReport{
		Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR,
		Findings: []*mgmtv1alpha1.PreflightFinding{{
			Kind:    mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
			Level:   mgmtv1alpha1.PreflightFinding_LEVEL_INFORMATION,
			Table:   "public.users",
			Message: "public.users is read in one stream",
		}},
	}
	s.Mocks.TemporalClientManager.EXPECT().
		RunWorkflow(mock.Anything, accountId,
			mock.MatchedBy(func(opts *temporalclient.StartWorkflowOptions) bool {
				return opts.WorkflowExecutionTimeout > 0 && len(opts.ID) > len("preflight-"+jobId)
			}),
			mock.Anything, &preflight_workflow.Request{JobId: jobId}, mock.Anything, mock.Anything).
		Run(func(_ context.Context, _ string, _ *temporalclient.StartWorkflowOptions, _ any, _ any, valuePtr any, _ *slog.Logger) {
			result, ok := valuePtr.(*preflight_workflow.Response)
			require.True(t, ok)
			result.Report = report
		}).
		Return(nil).Once()
	resp, err := clients.Jobs().PreflightJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.PreflightJobRequest{JobId: jobId}))
	requireNoErrResp(t, resp, err)
	require.Equal(t, mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR, resp.Msg.GetReport().GetEngine())
	require.Len(t, resp.Msg.GetReport().GetFindings(), 1)
	require.Equal(t, "public.users", resp.Msg.GetReport().GetFindings()[0].GetTable())
	require.NotNil(t, resp.Msg.GetCheckedAt())

	s.Mocks.TemporalClientManager.EXPECT().
		RunWorkflow(mock.Anything, accountId, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(clientmanager.ErrNoWorker).Once()
	_, err = clients.Jobs().PreflightJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.PreflightJobRequest{JobId: jobId}))
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.ErrorContains(t, err, "no worker serves this account")
}
