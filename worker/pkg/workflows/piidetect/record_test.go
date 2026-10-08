package piidetect

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/encoding/protojson"
)

// recordDirKey names the directory the histories are recorded into. Nothing is recorded
// without it:
//
//	PII_DETECT_RECORD_DIR=testdata go test ./worker/pkg/workflows/piidetect -run Test_RecordsHistories -count=1
const recordDirKey = "PII_DETECT_RECORD_DIR"

// The ids the stub activities answer with.
const (
	recordedAccount    = "0b6c1f4e-6d0a-4a55-9c1b-7f4f6f0f2a11"
	recordedJob        = "5a0f3d5c-2a59-4a4f-8f0e-0f1f0b8b7c21"
	recordedConnection = "d4b2e6a8-1c3f-4b5d-9e7a-2f6c8d0b1a31"
)

// recording is a run to record: the workflow to start, its input, and what the analyzer
// answers the tables of the run.
type recording struct {
	name     string
	workflow string
	input    any
	analyzer *DetectPiiContentResponse
	// analyzed is how many tables the analyzer is asked about.
	analyzed int32
}

// Test_RecordsHistories runs the workflows of the package, with stub activities, on a
// Temporal dev server started through the SDK testsuite, and writes what the server kept
// of each run. A history that is already there is left as it is: this only ever adds.
func Test_RecordsHistories(t *testing.T) {
	dir := os.Getenv(recordDirKey)
	if dir == "" {
		t.Skip("histories are recorded on request: set " + recordDirKey)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{LogLevel: "error"})
	require.NoError(t, err)
	defer func() { require.NoError(t, server.Stop()) }()

	tableRun := "replay-pii-table-content-analyzed"
	for _, run := range []*recording{
		// A table with a free-text column the rules found nothing in: the analyzer finds
		// personal data in it.
		{
			name:     "table-content-analyzed",
			workflow: TableWorkflowName,
			input: &TablePiiDetectRequest{
				AccountId: recordedAccount, JobId: recordedJob, ConnectionId: recordedConnection,
				TableSchema: "public", TableName: "tickets", ShouldSampleData: true, ParentExecutionId: &tableRun,
			},
			analyzer: &DetectPiiContentResponse{
				PiiColumns: map[string]report.AnalyzerFinding{
					"note": {Category: "free_text_pii", Entity: "PERSON", Matches: 7, Sampled: 50},
				},
				Status: report.AnalyzerAnswered,
			},
			analyzed: 1,
		},
		// Three tables scanned one at a time, by an API without analyzer: the first table
		// learns it, the two others are told.
		{
			name:     "job-analyzer-absent",
			workflow: JobWorkflowName,
			input:    &JobPiiDetectRequest{JobId: recordedJob},
			analyzer: &DetectPiiContentResponse{PiiColumns: map[string]report.AnalyzerFinding{}, Status: report.AnalyzerNone},
			analyzed: 1,
		},
		// Three tables scanned one at a time, whose free-text column the analyzer refuses:
		// each is analyzed in part, and the run ends well.
		{
			name:     "job-analyzer-partial",
			workflow: JobWorkflowName,
			input:    &JobPiiDetectRequest{JobId: recordedJob},
			analyzer: &DetectPiiContentResponse{
				PiiColumns: map[string]report.AnalyzerFinding{}, NotAnalyzed: []string{"note"}, Status: report.AnalyzerPartial,
			},
			analyzed: 3,
		},
	} {
		t.Run(run.name, func(t *testing.T) {
			path := filepath.Join(dir, run.name+".json")
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Skipf("%s is already recorded, or cannot be looked at: %v", path, err)
			}

			license := testutil.NewFakeEELicense()
			license.SetValid(true)
			taskQueue := "replay-recorder-" + run.name
			recorder := worker.New(server.Client(), taskQueue, worker.Options{})
			recorder.RegisterWorkflow(NewJobWorkflow(license, defaultTablesAtOnce).JobPiiDetect)
			recorder.RegisterWorkflow(TablePiiDetect)
			recorder.RegisterWorkflow(accounthooks.ProcessAccountHook)
			analyzed := registerStubActivities(recorder, run.analyzer)
			require.NoError(t, recorder.Start())
			defer recorder.Stop()

			started, err := server.Client().ExecuteWorkflow(
				ctx,
				client.StartWorkflowOptions{ID: "replay-pii-" + run.name, TaskQueue: taskQueue},
				run.workflow,
				run.input,
			)
			require.NoError(t, err)
			require.NoError(t, started.Get(ctx, nil))
			require.Equal(t, run.analyzed, analyzed.Load())

			history := &historypb.History{}
			events := server.Client().GetWorkflowHistory(
				ctx, started.GetID(), started.GetRunID(), false, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT,
			)
			for events.HasNext() {
				event, err := events.Next()
				require.NoError(t, err)
				history.Events = append(history.Events, event)
			}
			encoded, err := protojson.MarshalOptions{Indent: "  "}.Marshal(history)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, encoded, 0o600))
		})
	}
}

// registerStubActivities registers, under the names of the activities of the package and
// of the lookup of the account hooks, activities that answer without calling anything. It
// returns the count of the calls to the content activity.
func registerStubActivities(recorder worker.Worker, analyzer *DetectPiiContentResponse) *atomic.Int32 {
	stub := func(name string, fn any) {
		recorder.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	text := func(words float64) *profile.Profile {
		return &profile.Profile{Rows: 200, Kind: profile.KindText, Words: words}
	}

	stub("GetAccountHooksByEvent", func(
		context.Context, *accounthooks.GetAccountHooksByEventRequest,
	) (*accounthooks.GetAccountHooksByEventResponse, error) {
		return &accounthooks.GetAccountHooksByEventResponse{}, nil
	})
	stub("GetPiiDetectJobDetails", func(context.Context, *GetPiiDetectJobDetailsRequest) (*GetPiiDetectJobDetailsResponse, error) {
		return &GetPiiDetectJobDetailsResponse{
			AccountId: recordedAccount,
			PiiDetectConfig: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
				DataSampling: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true},
			},
			SourceConnectionId: recordedConnection,
			TablesAtOnce:       1,
		}, nil
	})
	stub("GetTablesToPiiScan", func(context.Context, *GetTablesToPiiScanRequest) (*GetTablesToPiiScanResponse, error) {
		return &GetTablesToPiiScanResponse{Tables: []TableToScan{
			{Schema: "public", Table: "t1", Fingerprint: "fingerprint-t1"},
			{Schema: "public", Table: "t2", Fingerprint: "fingerprint-t2"},
			{Schema: "public", Table: "t3", Fingerprint: "fingerprint-t3"},
		}}, nil
	})
	stub("SaveJobPiiDetectReport", func(ctx context.Context, req *SaveJobPiiDetectReportRequest) (*SaveJobPiiDetectReportResponse, error) {
		return &SaveJobPiiDetectReportResponse{Key: &mgmtv1alpha1.RunContextKey{
			AccountId:  req.AccountId,
			JobRunId:   activity.GetInfo(ctx).WorkflowExecution.ID,
			ExternalId: report.JobReportExternalId(req.JobId),
		}}, nil
	})

	stub("GetColumnData", func(context.Context, *GetColumnDataRequest) (*GetColumnDataResponse, error) {
		return &GetColumnDataResponse{
			ColumnData: []*ColumnData{
				{Column: "id", DataType: "uuid"},
				{Column: "email", DataType: "text", IsNullable: true, Profile: text(1)},
				{Column: "note", DataType: "text", IsNullable: true, Profile: text(12)},
			},
			SampledRows: 200,
		}, nil
	})
	stub("DetectPiiRegex", func(context.Context, *DetectPiiRegexRequest) (*DetectPiiRegexResponse, error) {
		return &DetectPiiRegexResponse{
			PiiColumns: map[string]report.Category{"email": report.Contact},
			Evidence:   map[string]string{"email": "name"},
		}, nil
	})
	stub("DetectPiiLLM", func(context.Context, *DetectPiiLLMRequest) (*DetectPiiLLMResponse, error) {
		return &DetectPiiLLMResponse{
			PiiColumns: map[string]report.ModelFinding{"email": {Category: report.Contact, Confidence: 0.95}},
			Input:      report.InputProfiles, Status: report.ModelAnswered, Model: "local-model",
		}, nil
	})
	analyzed := &atomic.Int32{}
	stub("DetectPiiContent", func(context.Context, *DetectPiiContentRequest) (*DetectPiiContentResponse, error) {
		analyzed.Add(1)
		return analyzer, nil
	})
	stub("SaveTablePiiDetectReport", func(ctx context.Context, req *SaveTablePiiDetectReportRequest) (*SaveTablePiiDetectReportResponse, error) {
		runId := activity.GetInfo(ctx).WorkflowExecution.ID
		if req.ParentRunId != nil {
			runId = *req.ParentRunId
		}
		return &SaveTablePiiDetectReportResponse{Key: &mgmtv1alpha1.RunContextKey{
			AccountId:  req.AccountId,
			JobRunId:   runId,
			ExternalId: report.TableReportExternalId(req.TableSchema, req.TableName),
		}}, nil
	})
	stub("RecordRunStarted", func(context.Context, *runusage.RunStartedRequest) error { return nil })
	stub("RecordRunEnded", func(context.Context, *runusage.RunEndedRequest) error { return nil })
	return analyzed
}
