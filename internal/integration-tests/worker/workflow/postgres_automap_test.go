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

// Under AutoMap & Review, a run maps the new columns as the PII detection suggests — or in
// passthrough when it suggests nothing or a key covers the column — writes the mappings to the
// job, and records each change for review. A later run records the columns that disappeared and
// those that changed type.
func test_postgres_automap_review(
	t *testing.T,
	ctx context.Context,
	postgres *tcpostgres.PostgresTestSyncContainer,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	dbManagers *TestDatabaseManagers,
	accountId string,
	sourceConn, destConn *mgmtv1alpha1.Connection,
) {
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	schema := "automap_review"

	_, err := postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.clients (
			id integer PRIMARY KEY,
			email text,
			telephone varchar(20),
			login varchar(50) UNIQUE,
			notes text
		);
		INSERT INTO %[1]s.clients VALUES
			(1, 'ada@example.com', '06 12 34 56 78', 'ada', 'first'),
			(2, 'bob@example.com', '07 98 76 54 32', 'bob', 'second');
	`, schema))
	require.NoError(t, err)
	require.NoError(t, postgres.Target.CreateSchemas(ctx, []string{schema}))
	husonymApi.MockTemporalForCreateJob("test-postgres-sync")

	job := createPostgresSyncJob(t, ctx, jobclient, &createJobConfig{
		AccountId:  accountId,
		SourceConn: sourceConn,
		DestConn:   destConn,
		JobName:    "automap_review",
		JobMappings: []*mgmtv1alpha1.JobMapping{{
			Schema: schema, Table: "clients", Column: "id",
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}},
		}},
		JobOptions: &TestJobOptions{
			Truncate:          true,
			TruncateCascade:   true,
			InitSchema:        true,
			AutoMapNewColumns: true,
		},
	})

	runJob := func() {
		t.Helper()
		testworkflow := NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
		testworkflow.RequireActivitiesCompletedSuccessfully(t)
		testworkflow.ExecuteTestDataSyncWorkflow(job.GetId())
		require.True(t, testworkflow.TestEnv.IsWorkflowCompleted())
		require.NoError(t, testworkflow.TestEnv.GetWorkflowError())
	}
	mappings := func() map[string]*mgmtv1alpha1.TransformerConfig {
		t.Helper()
		resp, err := jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: job.GetId()}))
		require.NoError(t, err)
		out := map[string]*mgmtv1alpha1.TransformerConfig{}
		for _, m := range resp.Msg.GetJob().GetMappings() {
			out[m.GetColumn()] = m.GetTransformer().GetConfig()
		}
		return out
	}
	pending := func() map[string]mgmtv1alpha1.JobMappingChangeKind {
		t.Helper()
		resp, err := jobclient.GetPendingMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.GetPendingMappingChangesRequest{
			AccountId: accountId,
			JobId:     &job.Id,
		}))
		require.NoError(t, err)
		out := map[string]mgmtv1alpha1.JobMappingChangeKind{}
		for _, c := range resp.Msg.GetChanges() {
			out[c.GetColumn().GetColumn()] = c.GetKind()
		}
		return out
	}

	// First run: the four new columns are mapped and written to the job.
	runJob()
	mapped := mappings()
	require.NotNil(t, mapped["email"].GetGenerateEmailConfig())
	require.True(t, mapped["telephone"].GetTransformPhoneNumberConfig().GetPreserveFormat())
	require.NotNil(t, mapped["login"].GetPassthroughConfig(), "a unique column stays in passthrough")
	require.NotNil(t, mapped["notes"].GetPassthroughConfig(), "nothing suggested")

	added := mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_ADDED
	require.Equal(t, map[string]mgmtv1alpha1.JobMappingChangeKind{
		"email": added, "telephone": added, "login": added, "notes": added,
	}, pending())

	var sourcePhone, targetPhone string
	require.NoError(t, postgres.Source.DB.QueryRow(ctx,
		fmt.Sprintf("SELECT telephone FROM %s.clients WHERE id = 1", schema)).Scan(&sourcePhone))
	require.NoError(t, postgres.Target.DB.QueryRow(ctx,
		fmt.Sprintf("SELECT telephone FROM %s.clients WHERE id = 1", schema)).Scan(&targetPhone))
	require.NotEqual(t, sourcePhone, targetPhone)
	require.Len(t, targetPhone, len(sourcePhone))
	require.Equal(t, "06 ", targetPhone[:3])

	// The source loses a column and another one changes type.
	_, err = postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
		ALTER TABLE %[1]s.clients DROP COLUMN notes;
		ALTER TABLE %[1]s.clients ALTER COLUMN email TYPE varchar(255);
	`, schema))
	require.NoError(t, err)

	runJob()
	_, stillMapped := mappings()["notes"]
	require.False(t, stillMapped, "the mapping of a column the source lost stays in the job")
	require.Equal(t, map[string]mgmtv1alpha1.JobMappingChangeKind{
		// The added email change is shadowed by its type change in this view, keyed by column.
		"email":     mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_TYPE_CHANGED,
		"telephone": added,
		"login":     added,
		"notes":     mgmtv1alpha1.JobMappingChangeKind_JOB_MAPPING_CHANGE_KIND_REMOVED,
	}, pending())

	// Reviewing everything empties the list.
	resp, err := jobclient.GetPendingMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.GetPendingMappingChangesRequest{
		AccountId: accountId,
		JobId:     &job.Id,
	}))
	require.NoError(t, err)
	// email, telephone and login added, notes removed, email retyped. The addition of notes is
	// settled: the column is gone, and its removal is what is left to review.
	require.Len(t, resp.Msg.GetChanges(), 5)
	ids := []string{}
	for _, c := range resp.Msg.GetChanges() {
		ids = append(ids, c.GetId())
	}
	_, err = jobclient.ReviewMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.ReviewMappingChangesRequest{
		AccountId: accountId,
		JobId:     job.GetId(),
		ChangeIds: ids,
	}))
	require.NoError(t, err)
	require.Empty(t, pending())

	require.NoError(t, cleanupPostgresSchemas(ctx, postgres, []string{schema}))
}
