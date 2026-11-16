package integrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	tcpostgres "github.com/fishtre-compagnie/husonym/internal/testutil/testcontainers/postgres"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect"
	piidetect_model "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	piidetect_report "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// A PII detection job on a real database, from its run to the report the API returns: the
// tables are listed from the catalogue, the rows are sampled, the rules and a model
// answer, the reports are stored. The model is a local server that answers by column
// name; the job sends it values.
func test_postgres_pii_detect(
	t *testing.T,
	ctx context.Context,
	postgres *tcpostgres.PostgresTestSyncContainer,
	husonymApi *tchusonymapi.HusonymApiTestClient,
	dbManagers *TestDatabaseManagers,
	accountId string,
	sourceConn *mgmtv1alpha1.Connection,
) {
	jobclient := husonymApi.OSSUnauthenticatedLicensedClients.Jobs()
	connclient := husonymApi.OSSUnauthenticatedLicensedClients.Connections()
	schema := "pii_detect_job"

	_, err := postgres.Source.DB.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.clients (
			id integer PRIMARY KEY,
			courriel text,
			c17 text,
			note text,
			photo bytea,
			created_at timestamp NOT NULL DEFAULT now()
		);
		INSERT INTO %[1]s.clients (id, courriel, c17, note, photo) VALUES
			(1, 'camille.martin1@example.org', 'FR7630006000011234567890189', 'NOTEMARKER Mme Durand a appelé', 'PHOTOMARKER-1'),
			(2, 'lucas.bernard2@example.org', 'FR1420041010050500013M02606', 'NOTEMARKER rappeler Hugo Petit', 'PHOTOMARKER-2'),
			(3, 'manon.dubois3@example.org', 'FR8810278073000002056360189', 'NOTEMARKER dossier de Léa Simon', 'PHOTOMARKER-3'),
			(4, 'hugo.thomas4@example.org', 'FR7630006000011234567890189', NULL, NULL);
		CREATE TABLE %[1]s.produits (id integer PRIMARY KEY, libelle text);
		INSERT INTO %[1]s.produits VALUES (1, 'Chaise'), (2, 'Lampe'), (3, 'Table');
	`, schema))
	require.NoError(t, err)

	// The model: says that "note" holds personal data, and nothing of the other columns.
	var mu sync.Mutex
	var requests []string
	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		mu.Unlock()

		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &request)
		var document struct {
			Columns map[string]struct {
				Name string `json:"name"`
			} `json:"columns"`
		}
		_ = json.NewDecoder(strings.NewReader(request.Messages[1].Content)).Decode(&document)
		answers := map[string]any{}
		for id, column := range document.Columns {
			answers[id] = map[string]any{"category": "none", "confidence": 0.9}
			if column.Name == "note" {
				answers[id] = map[string]any{"category": "personal", "confidence": 0.8}
			}
		}
		content, _ := json.Marshal(answers)
		completion, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "local-model",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": string(content)},
			}},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(completion)
	}))
	endpoint := startHTTPServer(t, mux)

	husonymApi.MockTemporalForCreateJob("test-postgres-pii-detect")
	jobResp, err := jobclient.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: accountId,
		JobName:   "pii-detect-end-to-end",
		Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
				Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{ConnectionId: sourceConn.GetId()},
			},
		}},
		JobType: &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
			PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
				DataSampling: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{
					IsEnabled:  true,
					ModelInput: mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_VALUES,
				},
				TableScanFilter: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
					Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{
						Include: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns{Schemas: []string{schema}},
					},
				},
			},
		}},
	}))
	require.NoError(t, err)
	jobId := jobResp.Msg.GetJob().GetId()

	classifier, err := piidetect_model.NewClassifier(&piidetect_model.Config{
		BaseURL: endpoint.URL + "/v1", Model: "local-model", MinConfidence: 0.5,
	})
	require.NoError(t, err)
	data := connectiondata.NewConnectionDataBuilder(
		&sqlconnect.SqlOpenConnector{}, dbManagers.SqlManager, pg_queries.New(), mysql_queries.New(), nil, nil, nil, nil,
	)
	license := testutil.NewFakeEELicense()
	license.SetValid(true)

	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestWorkflowEnvironment()
	config := &piidetect.Config{}
	conndataclient := husonymApi.OSSUnauthenticatedLicensedClients.ConnectionData()
	piidetect.Register(
		env, license,
		piidetect.NewActivities(jobclient, connclient, conndataclient, data, nil, classifier, config),
		runusage.New(husonymApi.OSSUnauthenticatedLicensedClients.Usage()),
		config,
	)
	env.RegisterWorkflow(accounthooks.ProcessAccountHook)
	env.OnWorkflow(accounthooks.ProcessAccountHook, mock.Anything, mock.Anything).
		Return(func(workflow.Context, *accounthooks.ProcessAccountHookRequest) (*accounthooks.ProcessAccountHookResponse, error) {
			return &accounthooks.ProcessAccountHookResponse{}, nil
		})

	env.ExecuteWorkflow(piidetect.JobWorkflowName, &piidetect.JobPiiDetectRequest{JobId: jobId})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var result piidetect.JobPiiDetectResponse
	require.NoError(t, env.GetWorkflowResult(&result))
	runId := result.ReportKey.GetJobRunId()
	require.Equal(t, piidetect_report.JobReportExternalId(jobId), result.ReportKey.GetExternalId())

	// What the API returns for the run, read through its index.
	husonymApi.MockTemporalForDescribeWorkflowExecution(accountId, jobId, runId, piidetect.JobWorkflowName)
	reportResp, err := jobclient.GetPiiDetectionReport(ctx, connect.NewRequest(&mgmtv1alpha1.GetPiiDetectionReportRequest{
		JobRunId: runId, AccountId: accountId,
	}))
	require.NoError(t, err)
	found := map[string]string{}
	for _, table := range reportResp.Msg.GetReport().GetTables() {
		require.Equal(t, schema, table.GetSchema())
		for _, column := range table.GetColumns() {
			found[table.GetTable()+"."+column.GetColumn()] = fmt.Sprintf(
				"regex=%s llm=%s", column.GetRegexReport().GetCategory(), column.GetLlmReport().GetCategory(),
			)
		}
	}
	require.Len(t, reportResp.Msg.GetReport().GetTables(), 2)
	require.Equal(t, map[string]string{
		"clients.courriel": "regex=contact llm=",   // by its name
		"clients.c17":      "regex=financial llm=", // by the format of its values
		"clients.note":     "regex= llm=personal",  // by the model
	}, found)

	// What is stored for the table: how it was scanned.
	stored, err := jobclient.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId: runId, ExternalId: piidetect_report.TableReportExternalId(schema, "clients"), AccountId: accountId,
		},
	}))
	require.NoError(t, err)
	var tableReport piidetect_report.TableReport
	require.NoError(t, json.Unmarshal(stored.Msg.GetValue(), &tableReport))
	require.Equal(t, []string{"id", "courriel", "c17", "note", "photo", "created_at"}, tableReport.ScannedColumns)
	// "note" is free text the rules found nothing in: the API is asked to analyze its
	// content, and has no analyzer here.
	require.Equal(t, &piidetect_report.Scan{
		SampledRows: 4, Input: piidetect_report.InputValues, Model: "local-model",
		ModelStatus:    piidetect_report.ModelAnswered,
		Sources:        []string{piidetect_report.SourceRules, piidetect_report.SourceModel},
		AnalyzerStatus: piidetect_report.AnalyzerNone,
	}, tableReport.Scan)
	for _, column := range tableReport.ColumnReports {
		if column.ColumnName == "c17" {
			require.Equal(t, "values:iban 1", column.Report.Regex.Evidence)
		}
	}

	// What the model was sent: values of the text columns, none of the binary one.
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, requests, 2, "one request per table")
	sent := strings.Join(requests, "\n")
	require.Contains(t, sent, "NOTEMARKER")
	require.Contains(t, sent, "camille.martin1@example.org")
	require.NotContains(t, sent, "PHOTOMARKER")
	require.Contains(t, sent, `\"sample\":`)
}
