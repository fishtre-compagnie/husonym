package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The OpenAI connection of an AI generate job belongs to the job's account: the run spends its
// key. A connection of another account is refused when the job is written, even to a user who is
// a member of both accounts.
func (s *IntegrationTestSuite) Test_AiGenerateJob_AiConnectionOfTheJobsAccount() {
	t := s.T()
	ctx := s.ctx
	userclient := s.OSSAuthenticatedLicensedClients.Users(integrationtests_test.WithUserId(testAuthUserId))
	connclient := s.OSSAuthenticatedLicensedClients.Connections(integrationtests_test.WithUserId(testAuthUserId))
	jobclient := s.OSSAuthenticatedLicensedClients.Jobs(integrationtests_test.WithUserId(testAuthUserId))
	s.setUser(ctx, userclient)

	accountId := s.createTeamAccount(ctx, userclient, uuid.NewString())
	otherAccountId := s.createTeamAccount(ctx, userclient, uuid.NewString())
	fkconn := s.createPostgresConnection(connclient, accountId, "fk", "test")
	destconn := s.createPostgresConnection(connclient, accountId, "dest", "test2")
	ownAi := s.createOpenAiConnection(connclient, accountId, "own-ai")
	foreignAi := s.createOpenAiConnection(connclient, otherAccountId, "foreign-ai")

	source := func(aiConnectionId string) *mgmtv1alpha1.JobSource {
		return &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_AiGenerate{AiGenerate: &mgmtv1alpha1.AiGenerateSourceOptions{
				AiConnectionId:      aiConnectionId,
				FkSourceConnectionId: new(fkconn.GetId()),
				ModelName:           "gpt",
				Schemas: []*mgmtv1alpha1.AiGenerateSourceSchemaOption{{
					Schema: "public",
					Tables: []*mgmtv1alpha1.AiGenerateSourceTableOption{{Table: "users", RowCount: 1}},
				}},
			}},
		}}
	}
	create := func(name, aiConnectionId string) (*connect.Response[mgmtv1alpha1.CreateJobResponse], error) {
		return jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   name,
			Source:    source(aiConnectionId),
			Destinations: []*mgmtv1alpha1.CreateJobDestination{{
				ConnectionId: destconn.GetId(),
				Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
					PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
				}},
			}},
		}))
	}

	_, err := create("foreign-ai-job", foreignAi.GetId())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)

	s.MockTemporalForCreateJob("own-ai-job")
	created, err := create("own-ai-job", ownAi.GetId())
	requireNoErrResp(t, created, err)

	_, err = jobclient.UpdateJobSourceConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
		Id:     created.Msg.GetJob().GetId(),
		Source: source(foreignAi.GetId()),
	}))
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)

	updated, err := jobclient.UpdateJobSourceConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
		Id:     created.Msg.GetJob().GetId(),
		Source: source(ownAi.GetId()),
	}))
	requireNoErrResp(t, updated, err)
}

func (s *IntegrationTestSuite) createOpenAiConnection(
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	accountId, name string,
) *mgmtv1alpha1.Connection {
	resp, err := connclient.CreateConnection(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateConnectionRequest{
		AccountId: accountId,
		Name:      name,
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_OpenaiConfig{OpenaiConfig: &mgmtv1alpha1.OpenAiConnectionConfig{
				ApiKey: "the-key-of-" + name,
				ApiUrl: "https://llm.example.com/v1",
			}},
		},
	}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetConnection()
}
