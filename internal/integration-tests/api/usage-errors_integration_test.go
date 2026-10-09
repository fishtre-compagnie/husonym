package integrationtests_test

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// The reference document of the usage report, as the tests of the report keep it.
const usageReportReference = "../../../backend/internal/usagereport/testdata/report-full.json"

// From the error of the worker to the report: the category the worker reads in a database
// error leaves with the end of the run, through the activity the workflow runs and the
// procedure of the API, is kept on the row of the run, and is what the errors of the day count.
// The message of the error goes nowhere.
func (s *IntegrationTestSuite) Test_UsageErrors_FromTheErrorOfTheWorkerToTheReport() {
	t := s.T()
	ctx := s.ctx
	clients := s.OSSUnauthenticatedLicensedClients
	accountId := s.createPersonalAccount(ctx, clients.Users())
	source := s.createPostgresConnection(clients.Connections(), accountId, "source", "test")
	destination := s.createPostgresConnection(clients.Connections(), accountId, "destination", "test2")
	s.MockTemporalForCreateJob("usage-errors-job")
	created, err := clients.Jobs().CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   "usage-errors-job",
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
	jobId := created.Msg.GetJob().GetId()

	// 1. The worker reads the category in the error, where it is still typed.
	const message = "never-sent"
	category := runerror.Classify(fmt.Errorf("writing: %w", &pgconn.PgError{
		Code: "23505", Message: message, Detail: message, ConstraintName: message,
	}))
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED, category)

	// 2. The run tells the API, through the activity its workflow runs at its end.
	const runId = "run-e2e"
	startedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	require.NoError(t, runusage.New(clients.Usage()).RecordRunEnded(ctx, &runusage.RunEndedRequest{
		JobId: jobId, RunId: runId, StartedAt: startedAt, EndedAt: startedAt.Add(time.Minute),
		Outcome:       runusage.OutcomeFailed,
		ErrorCategory: category,
		ErrorStep:     mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_TABLE_SYNC,
	}))

	// 3. The row of the run holds the two identifiers, and nothing of the message.
	var status, errorCategory, errorStep, row string
	var recordedAt time.Time
	require.NoError(t, s.Pgcontainer.DB.QueryRow(ctx,
		`SELECT status, error_category, error_step, recorded_at, to_jsonb(r)::text
		 FROM husonym_api.run_usage r WHERE run_id = $1`, runId,
	).Scan(&status, &errorCategory, &errorStep, &recordedAt, &row))
	require.Equal(t, "failed", status)
	require.Equal(t, "constraint_violated", errorCategory)
	require.Equal(t, "table_sync", errorStep)
	require.NotContains(t, row, message)

	// 4. The errors of the day the end was recorded on, as the report of that day counts them,
	// in a document the published schema accepts.
	counted, err := s.UsageErrorsOfDay(ctx, recordedAt)
	require.NoError(t, err)
	reference, err := os.ReadFile(usageReportReference)
	require.NoError(t, err)
	var report telemetry.Report
	require.NoError(t, json.Unmarshal(reference, &report))
	require.NotNil(t, report.Diagnostics)
	report.Diagnostics.Errors = counted
	document, err := json.Marshal(&report)
	require.NoError(t, err)
	require.NoError(t, telemetry.Validate(document))
	require.NotContains(t, string(document), message)

	var read struct {
		Diagnostics struct {
			Errors json.RawMessage `json:"errors"`
		} `json:"diagnostics"`
	}
	require.NoError(t, json.Unmarshal(document, &read))
	require.JSONEq(t, `[{"category":"constraint_violated","step":"table_sync","count":1}]`, string(read.Diagnostics.Errors))
}
