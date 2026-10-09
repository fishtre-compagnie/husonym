package integrationtest

import (
	"context"
	"fmt"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/stretchr/testify/require"
)

// A run that really fails, under Athanor, leaves on its usage row the category and the step
// of what failed it, read from the error PostgreSQL itself answered and never from its text:
//
//   - a destination that lacks the table is found by the pre-flight check, before anything is
//     written: an object is missing, at the pre-flight step;
//   - a destination whose table refuses the rows under a CHECK constraint fails the sync of
//     the table: a constraint is violated, at the table sync step.
func test_postgres_run_error_athanor(
	t *testing.T,
	ctx context.Context,
	postgres *tcpostgres.PostgresTestSyncContainer,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	dbManagers *TestDatabaseManagers,
	accountId string,
	sourceConn, destConn *mgmtv1alpha1.Connection,
) {
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	missing, refused := "run_error_missing", "run_error_refused"

	for _, schema := range []string{missing, refused} {
		_, err := postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
			CREATE SCHEMA %[1]s;
			CREATE TABLE %[1]s.people (id integer PRIMARY KEY, age integer);
			INSERT INTO %[1]s.people VALUES (1, 30), (2, 41), (3, 52);
		`, schema))
		require.NoError(t, err)
	}
	// The destination of the first job has the schema and not the table; the one of the second
	// has a table that takes no row of the source.
	require.NoError(t, postgres.Target.CreateSchemas(ctx, []string{missing, refused}))
	_, err := postgres.Target.DB.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE %s.people (id integer PRIMARY KEY, age integer CHECK (age < 18))`, refused))
	require.NoError(t, err)

	passthrough := &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	}}
	// runOf runs a job that copies the table of the schema as it is, into a destination the run
	// does not initialize, and gives the usage row the run left with the error it ended on.
	runOf := func(schema string) (status, category, step, row string, runErr error) {
		t.Helper()
		husonymApi.MockTemporalForCreateJob("test-postgres-sync")
		job := createPostgresSyncJob(t, ctx, jobclient, &createJobConfig{
			AccountId:  accountId,
			SourceConn: sourceConn,
			DestConn:   destConn,
			JobName:    schema,
			JobMappings: []*mgmtv1alpha1.JobMapping{
				{Schema: schema, Table: "people", Column: "id", Transformer: passthrough},
				{Schema: schema, Table: "people", Column: "age", Transformer: passthrough},
			},
			JobOptions: &TestJobOptions{Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR},
		})
		testworkflow := NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
		testworkflow.ExecuteTestDataSyncWorkflow(job.GetId())
		require.True(t, testworkflow.TestEnv.IsWorkflowCompleted())

		require.NoError(t, husonymApi.Pgcontainer.DB.QueryRow(ctx,
			`SELECT status, coalesce(error_category, ''), coalesce(error_step, ''), to_jsonb(r)::text
			 FROM husonym_api.run_usage r WHERE job_id = $1`, job.GetId(),
		).Scan(&status, &category, &step, &row))
		return status, category, step, row, testworkflow.TestEnv.GetWorkflowError()
	}

	t.Run("a destination without the table", func(t *testing.T) {
		status, category, step, _, runErr := runOf(missing)
		require.Error(t, runErr)
		require.ErrorContains(t, runErr, "pre-flight check stopped the run")
		require.Equal(t, "failed", status)
		require.Equal(t, "object_missing", category)
		require.Equal(t, "preflight", step)
	})

	t.Run("a destination that refuses the rows", func(t *testing.T) {
		status, category, step, row, runErr := runOf(refused)
		require.Error(t, runErr)
		// The error of the run names the constraint, as it always did; the row of its usage
		// holds the category and nothing of that text.
		require.ErrorContains(t, runErr, "people_age_check")
		require.NotContains(t, row, "people_age_check")
		require.Equal(t, "failed", status)
		require.Equal(t, "constraint_violated", category)
		require.Equal(t, "table_sync", step)

		var written int
		require.NoError(t, postgres.Target.DB.QueryRow(ctx,
			fmt.Sprintf("SELECT count(*) FROM %s.people", refused)).Scan(&written))
		require.Zero(t, written)
	})

	require.NoError(t, cleanupPostgresSchemas(ctx, postgres, []string{missing, refused}))
}
