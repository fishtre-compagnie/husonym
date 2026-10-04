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

// Under Athanor, the mappings AutoMap writes for new sensitive columns carry a run to its
// end, and the runs after it: a NULL stays a NULL under the character scramble, equal
// values come out equal in every row and table, a generated number fits its column, and
// a column under a CHECK constraint is left as it is.
func test_postgres_automap_athanor(
	t *testing.T,
	ctx context.Context,
	postgres *tcpostgres.PostgresTestSyncContainer,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	dbManagers *TestDatabaseManagers,
	accountId string,
	sourceConn, destConn *mgmtv1alpha1.Connection,
) {
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	schema := "automap_athanor"

	_, err := postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.people (
			id integer PRIMARY KEY,
			tax_id varchar(20),
			salary smallint,
			age numeric(3,2),
			dob varchar(10) CHECK (dob ~ '^\d{4}-\d{2}-\d{2}$')
		);
		CREATE TABLE %[1]s.declarations (
			id integer PRIMARY KEY,
			tax_id varchar(20)
		);
		INSERT INTO %[1]s.people VALUES
			(1, 'AB-123456-x', 30000, 3.50, '1985-03-12'),
			(2, 'CD-987654-y', 31000, 4.25, '1990-11-02'),
			(3, NULL, NULL, NULL, NULL),
			(4, 'AB-123456-x', 12000, 9.99, '1985-03-12');
		INSERT INTO %[1]s.declarations VALUES
			(1, 'AB-123456-x'),
			(2, 'CD-987654-y'),
			(3, NULL);
	`, schema))
	require.NoError(t, err)
	require.NoError(t, postgres.Target.CreateSchemas(ctx, []string{schema}))
	husonymApi.MockTemporalForCreateJob("test-postgres-sync")

	passthrough := func(table string) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: table, Column: "id",
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
			}},
		}
	}
	job := createPostgresSyncJob(t, ctx, jobclient, &createJobConfig{
		AccountId:   accountId,
		SourceConn:  sourceConn,
		DestConn:    destConn,
		JobName:     "automap_athanor",
		JobMappings: []*mgmtv1alpha1.JobMapping{passthrough("people"), passthrough("declarations")},
		JobOptions: &TestJobOptions{
			Truncate:          true,
			TruncateCascade:   true,
			InitSchema:        true,
			AutoMapNewColumns: true,
			Engine:            mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR,
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
	taxIds := func(table string) map[int]*string {
		t.Helper()
		rows, err := postgres.Target.DB.Query(ctx, fmt.Sprintf("SELECT id, tax_id FROM %s.%s", schema, table))
		require.NoError(t, err)
		defer rows.Close()
		out := map[int]*string{}
		for rows.Next() {
			var id int
			var taxId *string
			require.NoError(t, rows.Scan(&id, &taxId))
			out[id] = taxId
		}
		require.NoError(t, rows.Err())
		return out
	}

	runJob()

	resp, err := jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: job.GetId()}))
	require.NoError(t, err)
	mapped := map[string]*mgmtv1alpha1.TransformerConfig{}
	for _, m := range resp.Msg.GetJob().GetMappings() {
		mapped[m.GetTable()+"."+m.GetColumn()] = m.GetTransformer().GetConfig()
	}
	require.NotNil(t, mapped["people.tax_id"].GetTransformCharacterScrambleConfig())
	require.NotNil(t, mapped["declarations.tax_id"].GetTransformCharacterScrambleConfig())
	require.EqualValues(t, 20000, mapped["people.salary"].GetGenerateInt64Config().GetMin())
	require.EqualValues(t, 32767, mapped["people.salary"].GetGenerateInt64Config().GetMax())
	require.InDelta(t, 0, mapped["people.age"].GetGenerateFloat64Config().GetMin(), 0)
	require.InDelta(t, 9, mapped["people.age"].GetGenerateFloat64Config().GetMax(), 0)
	require.NotNil(t, mapped["people.dob"].GetPassthroughConfig(), "a column under a CHECK constraint stays in passthrough")

	check := func() {
		t.Helper()
		people, declarations := taxIds("people"), taxIds("declarations")
		require.Len(t, people, 4)
		require.Len(t, declarations, 3)

		require.Nil(t, people[3], "a NULL stays a NULL")
		require.Nil(t, declarations[3], "a NULL stays a NULL")
		require.NotNil(t, people[1])
		require.NotEqual(t, "AB-123456-x", *people[1])
		require.Len(t, *people[1], len("AB-123456-x"))
		require.Regexp(t, `^[A-Z]{2}.[0-9]{6}.[a-z]$`, *people[1], "each character stays in its class")
		require.Equal(t, people[1], people[4], "the same value in two rows")
		require.Equal(t, people[1], declarations[1], "the same value in two tables")
		require.Equal(t, people[2], declarations[2], "the same value in two tables")
		require.NotEqual(t, *people[1], *people[2])

		var salaries, ages, dates int
		require.NoError(t, postgres.Target.DB.QueryRow(ctx, fmt.Sprintf(
			"SELECT count(*) FROM %s.people WHERE salary BETWEEN 20000 AND 32767", schema)).Scan(&salaries))
		require.Equal(t, 4, salaries, "a generator writes a value in every row, within what the column holds")
		require.NoError(t, postgres.Target.DB.QueryRow(ctx, fmt.Sprintf(
			"SELECT count(*) FROM %s.people WHERE age BETWEEN 0 AND 9", schema)).Scan(&ages))
		require.Equal(t, 4, ages)
		require.NoError(t, postgres.Target.DB.QueryRow(ctx, fmt.Sprintf(
			"SELECT count(*) FROM %s.people WHERE dob IN ('1985-03-12', '1990-11-02') OR (id = 3 AND dob IS NULL)", schema)).Scan(&dates))
		require.Equal(t, 4, dates, "the column under a CHECK constraint is copied as it is")
	}
	check()

	// The mappings are in the job: the next run carries them out as well.
	runJob()
	check()

	require.NoError(t, cleanupPostgresSchemas(ctx, postgres, []string{schema}))
}
