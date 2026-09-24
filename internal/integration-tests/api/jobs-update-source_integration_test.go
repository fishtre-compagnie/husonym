package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// A write that expects the job as its caller read it is refused once the job has changed:
// a change made meanwhile — in the UI, or by a run mapping a new column — is not overwritten.
// It rests on updated_at moving on every change of a job.
func (s *IntegrationTestSuite) Test_UpdateJobSourceConnection_ExpectedUpdatedAt() {
	t := s.T()
	clients := s.OSSUnauthenticatedLicensedClients
	accountId := s.createPersonalAccount(s.ctx, clients.Users())
	srcconn := s.createPostgresConnection(clients.Connections(), accountId, "source", "test")
	destconn := s.createPostgresConnection(clients.Connections(), accountId, "dest", "test2")
	s.MockTemporalForCreateJob("test-id")

	source := &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
		Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			ConnectionId: srcconn.GetId(),
		}},
	}}
	mapping := func(column string) []*mgmtv1alpha1.JobMapping {
		return []*mgmtv1alpha1.JobMapping{{
			Schema: "public", Table: "users", Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}},
		}}
	}
	created, err := clients.Jobs().CreateJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   "expected-version",
		Mappings:  mapping("id"),
		Source:    source,
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: destconn.GetId(),
			Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
				PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
			}},
		}},
	}))
	requireNoErrResp(t, created, err)
	read := created.Msg.GetJob()

	update := func(column string, expected *mgmtv1alpha1.Job) (*connect.Response[mgmtv1alpha1.UpdateJobSourceConnectionResponse], error) {
		req := &mgmtv1alpha1.UpdateJobSourceConnectionRequest{Id: read.GetId(), Source: source, Mappings: mapping(column)}
		if expected != nil {
			req.ExpectedUpdatedAt = expected.GetUpdatedAt()
		}
		return clients.Jobs().UpdateJobSourceConnection(s.ctx, connect.NewRequest(req))
	}

	// The job as it was read: the write goes through, and moves updated_at on.
	first, err := update("email", read)
	requireNoErrResp(t, first, err)
	require.True(t, first.Msg.GetJob().GetUpdatedAt().AsTime().After(read.GetUpdatedAt().AsTime()),
		"updated_at moves on when a job's mappings change")

	// The job as it was before that write: refused, and nothing written.
	_, err = update("name", read)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	job, err := clients.Jobs().GetJob(s.ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: read.GetId()}))
	requireNoErrResp(t, job, err)
	require.Equal(t, "email", job.Msg.GetJob().GetMappings()[0].GetColumn())

	// Without an expectation, a write goes through as it always did.
	again, err := update("name", nil)
	requireNoErrResp(t, again, err)
}
