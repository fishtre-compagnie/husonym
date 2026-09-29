package integrationtest

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// A user-defined transformer of the job's account runs on either engine: each resolves it for
// the job's account, and one of another account would read as absent.
func test_postgres_user_defined_transformer(
	t *testing.T,
	ctx context.Context,
	postgres *tcpostgres.PostgresTestSyncContainer,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	dbManagers *TestDatabaseManagers,
	accountId string,
	sourceConn, destConn *mgmtv1alpha1.Connection,
) {
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	created, err := husonymApi.OSSUnauthenticatedLicensedClients.Transformers().CreateUserDefinedTransformer(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.CreateUserDefinedTransformerRequest{
			AccountId: accountId,
			Name:      "account-rule",
			Source:    mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_TRANSFORM_JAVASCRIPT,
			TransformerConfig: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
					TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: `return "account-rule";`},
				},
			},
		}),
	)
	require.NoError(t, err)
	transformerId := created.Msg.GetTransformer().GetId()

	for _, engine := range []mgmtv1alpha1.JobEngine{
		mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
		mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR,
	} {
		t.Run(engine.String(), func(t *testing.T) {
			schema := fmt.Sprintf("udt_%d", engine)
			_, err := postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
				CREATE SCHEMA %[1]s;
				CREATE TABLE %[1]s.people (id integer PRIMARY KEY, name text NOT NULL);
				INSERT INTO %[1]s.people VALUES (1, 'Ada'), (2, 'Grace');
			`, schema))
			require.NoError(t, err)
			require.NoError(t, postgres.Target.CreateSchemas(ctx, []string{schema}))
			husonymApi.MockTemporalForCreateJob("test-postgres-sync")

			job := createPostgresSyncJob(t, ctx, jobclient, &createJobConfig{
				AccountId:  accountId,
				SourceConn: sourceConn,
				DestConn:   destConn,
				JobName:    schema,
				JobMappings: []*mgmtv1alpha1.JobMapping{
					{Schema: schema, Table: "people", Column: "id", Transformer: &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
							PassthroughConfig: &mgmtv1alpha1.Passthrough{},
						}},
					}},
					{Schema: schema, Table: "people", Column: "name", Transformer: &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_UserDefinedTransformerConfig{
							UserDefinedTransformerConfig: &mgmtv1alpha1.UserDefinedTransformerConfig{Id: transformerId},
						}},
					}},
				},
				JobOptions: &TestJobOptions{Truncate: true, InitSchema: true, Engine: engine},
			})
			testworkflow := NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
			testworkflow.RequireActivitiesCompletedSuccessfully(t)
			testworkflow.ExecuteTestDataSyncWorkflow(job.GetId())
			require.True(t, testworkflow.TestEnv.IsWorkflowCompleted())
			require.NoError(t, testworkflow.TestEnv.GetWorkflowError())

			rows, err := postgres.Target.DB.Query(ctx, fmt.Sprintf("SELECT name FROM %s.people ORDER BY id", schema))
			require.NoError(t, err)
			var names []string
			for rows.Next() {
				var name string
				require.NoError(t, rows.Scan(&name))
				names = append(names, name)
			}
			require.NoError(t, rows.Err())
			require.Equal(t, []string{"account-rule", "account-rule"}, names)

			require.NoError(t, cleanupPostgresSchemas(ctx, postgres, []string{schema}))
		})
	}
}
