package integrationtest

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	preflight_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/preflight/workflow"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/encoding/protojson"
)

// The pre-flight check of a job, before any run, tells what the run then tells at its start,
// and writes nothing: not the job, which AutoMap would give a new column, nor the
// destination, whose schema the run would create.
func test_postgres_preflight(
	t *testing.T,
	ctx context.Context,
	postgres *tcpostgres.PostgresTestSyncContainer,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	dbManagers *TestDatabaseManagers,
	accountId string,
	sourceConn, destConn *mgmtv1alpha1.Connection,
) {
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	schema := "preflight_plan"

	_, err := postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.lignes (
			id integer PRIMARY KEY,
			prix integer NOT NULL,
			total integer GENERATED ALWAYS AS (prix * 2) STORED,
			notes text
		);
		CREATE TABLE %[1]s.journal (niveau integer, message text);
		INSERT INTO %[1]s.lignes (id, prix, notes) VALUES (1, 10, 'a'), (2, 20, 'b');
		INSERT INTO %[1]s.journal VALUES (1, 'x'), (1, 'x');
	`, schema))
	require.NoError(t, err)
	require.NoError(t, postgres.Target.CreateSchemas(ctx, []string{schema}))
	husonymApi.MockTemporalForCreateJob("test-postgres-sync")

	passthrough := &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	}}
	var mappings []*mgmtv1alpha1.JobMapping
	for table, columns := range map[string][]string{"lignes": {"id", "prix", "total"}, "journal": {"niveau", "message"}} {
		for _, column := range columns {
			mappings = append(mappings, &mgmtv1alpha1.JobMapping{
				Schema: schema, Table: table, Column: column, Transformer: passthrough,
			})
		}
	}
	job := createPostgresSyncJob(t, ctx, jobclient, &createJobConfig{
		AccountId:   accountId,
		SourceConn:  sourceConn,
		DestConn:    destConn,
		JobName:     "preflight_plan",
		JobMappings: mappings,
		JobOptions:  &TestJobOptions{Truncate: true, TruncateCascade: true, InitSchema: true, AutoMapNewColumns: true},
	})
	getJob := func() *mgmtv1alpha1.Job {
		t.Helper()
		resp, err := jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: job.GetId()}))
		require.NoError(t, err)
		return resp.Msg.GetJob()
	}
	before := getJob()

	checkEnv := NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
	checkEnv.TestEnv.SetStartWorkflowOptions(client.StartWorkflowOptions{ID: preflight_workflow.WorkflowId(job.GetId())})
	checkEnv.TestEnv.ExecuteWorkflow(preflight_workflow.New().JobPreflight, &preflight_workflow.Request{JobId: job.GetId()})
	require.True(t, checkEnv.TestEnv.IsWorkflowCompleted())
	require.NoError(t, checkEnv.TestEnv.GetWorkflowError())
	var checked preflight_workflow.Response
	require.NoError(t, checkEnv.TestEnv.GetWorkflowResult(&checked))

	require.Equal(t, mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS, checked.Report.GetEngine())
	require.ElementsMatch(t, []string{
		"LEVEL_BLOCKING KIND_GENERATED_COLUMN_WRITTEN preflight_plan.lignes",
		"LEVEL_INFORMATION KIND_READ_IN_ONE_STREAM preflight_plan.journal",
	}, summarize(checked.Report))

	// Nothing written: the job, its pending changes, the destination.
	after := getJob()
	require.Equal(t, before.GetUpdatedAt().AsTime(), after.GetUpdatedAt().AsTime())
	require.Len(t, after.GetMappings(), len(before.GetMappings()), "AutoMap maps nothing")
	pending, err := jobclient.GetPendingMappingChanges(ctx, connect.NewRequest(&mgmtv1alpha1.GetPendingMappingChangesRequest{
		AccountId: accountId, JobId: &job.Id,
	}))
	require.NoError(t, err)
	require.Empty(t, pending.Msg.GetChanges())
	_, err = jobclient.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId: preflight_workflow.WorkflowId(job.GetId()), ExternalId: "tablesync-connectionids", AccountId: accountId,
		},
	}))
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "the check keeps no run context")
	var tables int
	require.NoError(t, postgres.Target.DB.QueryRow(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = $1", schema).Scan(&tables))
	require.Zero(t, tables, "the schema of the destination is not created")

	// The run meets the same, keeps it, and stops at its start.
	runEnv := NewTestDataSyncWorkflowEnv(t, husonymApi, dbManagers)
	runEnv.ExecuteTestDataSyncWorkflow(job.GetId())
	require.True(t, runEnv.TestEnv.IsWorkflowCompleted())
	require.ErrorContains(t, runEnv.TestEnv.GetWorkflowError(), "pre-flight check stopped the run")
	kept, err := jobclient.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{JobRunId: job.GetId(), ExternalId: "preflight-report", AccountId: accountId},
	}))
	require.NoError(t, err)
	var report mgmtv1alpha1.PreflightReport
	require.NoError(t, protojson.Unmarshal(kept.Msg.GetValue(), &report))
	require.ElementsMatch(t, summarize(checked.Report), summarize(&report))
	require.NoError(t, postgres.Target.DB.QueryRow(ctx,
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = $1", schema).Scan(&tables))
	require.Zero(t, tables, "the run stopped before creating anything")
}

// summarize names each finding of a report by its level, kind and table.
func summarize(report *mgmtv1alpha1.PreflightReport) []string {
	var lines []string
	for _, f := range report.GetFindings() {
		lines = append(lines, fmt.Sprintf("%s %s %s", f.GetLevel(), f.GetKind(), f.GetTable()))
	}
	return lines
}
