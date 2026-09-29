package integrationtests_test

import (
	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	integrationtests_test "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A user-defined transformer belongs to the job's account: the run executes its code. One of
// another account is refused when the job is written, even to a user who is a member of both
// accounts.
func (s *IntegrationTestSuite) Test_Job_UserDefinedTransformerOfTheJobsAccount() {
	t := s.T()
	ctx := s.ctx
	userclient := s.OSSAuthenticatedLicensedClients.Users(integrationtests_test.WithUserId(testAuthUserId))
	connclient := s.OSSAuthenticatedLicensedClients.Connections(integrationtests_test.WithUserId(testAuthUserId))
	jobclient := s.OSSAuthenticatedLicensedClients.Jobs(integrationtests_test.WithUserId(testAuthUserId))
	transformerclient := s.OSSAuthenticatedLicensedClients.Transformers(integrationtests_test.WithUserId(testAuthUserId))
	s.setUser(ctx, userclient)

	accountId := s.createTeamAccount(ctx, userclient, uuid.NewString())
	otherAccountId := s.createTeamAccount(ctx, userclient, uuid.NewString())
	srcconn := s.createPostgresConnection(connclient, accountId, "src", "test")
	destconn := s.createPostgresConnection(connclient, accountId, "dest", "test2")
	own := s.createJavascriptTransformer(transformerclient, accountId, "own-rule")
	foreign := s.createJavascriptTransformer(transformerclient, otherAccountId, "foreign-rule")

	source := &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
		Config: &mgmtv1alpha1.JobSourceOptions_Postgres{Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{
			ConnectionId: srcconn.GetId(),
		}},
	}}
	create := func(name, transformerId string) (*connect.Response[mgmtv1alpha1.CreateJobResponse], error) {
		return jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: accountId,
			JobName:   name,
			Source:    source,
			Destinations: []*mgmtv1alpha1.CreateJobDestination{{
				ConnectionId: destconn.GetId(),
				Options: &mgmtv1alpha1.JobDestinationOptions{Config: &mgmtv1alpha1.JobDestinationOptions_PostgresOptions{
					PostgresOptions: &mgmtv1alpha1.PostgresDestinationConnectionOptions{},
				}},
			}},
			Mappings: []*mgmtv1alpha1.JobMapping{userDefinedMapping(transformerId)},
		}))
	}

	_, err := create("foreign-rule-job", foreign.GetId())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)

	s.MockTemporalForCreateJob("own-rule-job")
	created, err := create("own-rule-job", own.GetId())
	requireNoErrResp(t, created, err)
	jobId := created.Msg.GetJob().GetId()

	update := func(transformerId string) (*connect.Response[mgmtv1alpha1.UpdateJobSourceConnectionResponse], error) {
		return jobclient.UpdateJobSourceConnection(ctx, connect.NewRequest(&mgmtv1alpha1.UpdateJobSourceConnectionRequest{
			Id:       jobId,
			Source:   source,
			Mappings: []*mgmtv1alpha1.JobMapping{userDefinedMapping(transformerId)},
		}))
	}
	_, err = update(foreign.GetId())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	updated, err := update(own.GetId())
	requireNoErrResp(t, updated, err)

	apply := func(transformerId string) (*connect.Response[mgmtv1alpha1.ApplyMappingChangesResponse], error) {
		return jobclient.ApplyMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.ApplyMappingChangesRequest{
			AccountId: accountId,
			JobId:     jobId,
			Mappings:  []*mgmtv1alpha1.JobMapping{userDefinedMapping(transformerId)},
		}))
	}
	_, err = apply(foreign.GetId())
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	applied, err := apply(own.GetId())
	requireNoErrResp(t, applied, err)
}

func userDefinedMapping(transformerId string) *mgmtv1alpha1.JobMapping {
	return &mgmtv1alpha1.JobMapping{
		Schema: "public",
		Table:  "users",
		Column: "name",
		Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
				UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: transformerId},
			},
		}},
	}
}

func (s *IntegrationTestSuite) createJavascriptTransformer(
	transformerclient mgmtv1alpha1connect.TransformersServiceClient,
	accountId, name string,
) *mgmtv1alpha1.UserDefinedTransformer {
	resp, err := transformerclient.CreateUserDefinedTransformer(s.ctx, connect.NewRequest(&mgmtv1alpha1.CreateUserDefinedTransformerRequest{
		AccountId: accountId,
		Name:      name,
		Source:    mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT,
		TransformerConfig: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
				TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return "` + name + `";`},
			},
		},
	}))
	requireNoErrResp(s.T(), resp, err)
	return resp.Msg.GetTransformer()
}
