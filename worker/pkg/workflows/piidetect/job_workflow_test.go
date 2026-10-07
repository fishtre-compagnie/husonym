package piidetect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// The id the test environment gives the run: it is the run id of the reports.
const testRunId = "default-test-workflow-id"

const (
	createdKind   = "ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED"
	failedKind    = "ACCOUNT_HOOK_EVENT_JOB_RUN_FAILED"
	succeededKind = "ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED"
)

// jobRun runs the job workflow with its activities and its children mocked, and keeps
// what the run started.
type jobRun struct {
	env        *testsuite.TestWorkflowEnvironment
	activities *Activities

	mu         sync.Mutex
	events     []string
	tables     []*TablePiiDetectRequest
	running    int
	maxRunning int
}

func newJobRun(t *testing.T, tablesAtOnce int) *jobRun {
	t.Helper()
	license := testutil.NewFakeEELicense()
	license.SetValid(true)
	return newJobRunUnder(t, license, tablesAtOnce)
}

func newJobRunUnder(t *testing.T, license *testutil.FakeEELicense, tablesAtOnce int) *jobRun {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	run := &jobRun{env: ts.NewTestWorkflowEnvironment()}
	run.env.SetTestTimeout(30 * time.Second)
	run.activities = NewActivities(nil, nil, nil, nil, nil, &Config{})
	run.env.RegisterWorkflow(NewJobWorkflow(license, tablesAtOnce).JobPiiDetect)
	run.env.RegisterWorkflow(TablePiiDetect)
	run.env.RegisterWorkflow(accounthooks.ProcessAccountHook)
	run.env.RegisterActivity(run.activities.GetPiiDetectJobDetails)
	run.env.RegisterActivity(run.activities.GetLastSuccessfulWorkflowId)
	run.env.RegisterActivity(run.activities.GetTablesToPiiScan)
	run.env.RegisterActivity(run.activities.SaveJobPiiDetectReport)

	run.env.OnWorkflow(accounthooks.ProcessAccountHook, mock.Anything, mock.Anything).
		Return(func(
			_ workflow.Context,
			req *accounthooks.ProcessAccountHookRequest,
		) (*accounthooks.ProcessAccountHookResponse, error) {
			run.mu.Lock()
			defer run.mu.Unlock()
			run.events = append(run.events, req.Event.Kind().String())
			return &accounthooks.ProcessAccountHookResponse{}, nil
		})
	return run
}

// scanned is the end of a scan by activities that say nothing of the model step.
func scanned(*TablePiiDetectRequest) (string, error) { return "", nil }

// scanTables says how the scan of a table ends: the status of its model step, or a
// failure. A scan lasts a minute of the run's clock.
func (r *jobRun) scanTables(end func(req *TablePiiDetectRequest) (model string, err error)) {
	r.env.OnWorkflow(TablePiiDetect, mock.Anything, mock.Anything).
		Return(func(ctx workflow.Context, req *TablePiiDetectRequest) (*TablePiiDetectResponse, error) {
			r.mu.Lock()
			r.tables = append(r.tables, req)
			r.running++
			r.maxRunning = max(r.maxRunning, r.running)
			r.mu.Unlock()
			defer func() {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.running--
			}()
			if err := workflow.Sleep(ctx, time.Minute); err != nil {
				return nil, err
			}
			model, err := end(req)
			if err != nil {
				return nil, err
			}
			return &TablePiiDetectResponse{
				PiiColumns: map[string]report.Combined{},
				ResultKey:  tableReportKey(testRunId, req.TableSchema, req.TableName),
				Model:      model,
			}, nil
		})
}

func tableReportKey(runId, schema, table string) *mgmtv1alpha1.RunContextKey {
	return &mgmtv1alpha1.RunContextKey{
		JobRunId: runId, ExternalId: report.TableReportExternalId(schema, table), AccountId: "account-1",
	}
}

func (r *jobRun) withDetails(details *GetPiiDetectJobDetailsResponse) {
	r.env.OnActivity(r.activities.GetPiiDetectJobDetails, mock.Anything, &GetPiiDetectJobDetailsRequest{JobId: "job-1"}).
		Return(details, nil).Once()
}

func plainDetails() *GetPiiDetectJobDetailsResponse {
	return &GetPiiDetectJobDetailsResponse{
		AccountId:          "account-1",
		PiiDetectConfig:    &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
		SourceConnectionId: "connection-1",
	}
}

func (r *jobRun) withTables(names ...string) {
	tables := make([]TableToScan, 0, len(names))
	for _, name := range names {
		tables = append(tables, TableToScan{Schema: "public", Table: name, Fingerprint: "fingerprint-" + name})
	}
	r.env.OnActivity(r.activities.GetTablesToPiiScan, mock.Anything, mock.Anything).
		Return(&GetTablesToPiiScanResponse{Tables: tables}, nil).Once()
}

var jobKey = &mgmtv1alpha1.RunContextKey{JobRunId: testRunId, ExternalId: "job-1--job-pii-report", AccountId: "account-1"}

// savesReport expects the index of the run to be saved once, and returns where the
// index that was saved is put.
func (r *jobRun) savesReport() **report.JobReport {
	saved := new(*report.JobReport)
	r.env.OnActivity(r.activities.SaveJobPiiDetectReport, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *SaveJobPiiDetectReportRequest) (*SaveJobPiiDetectReportResponse, error) {
			if req.AccountId != "account-1" || req.JobId != "job-1" {
				return nil, fmt.Errorf("the index is saved for another job: %+v", req)
			}
			*saved = req.Report
			return &SaveJobPiiDetectReportResponse{Key: jobKey}, nil
		}).Once()
	return saved
}

func (r *jobRun) execute() {
	r.env.ExecuteWorkflow(JobWorkflowName, &JobPiiDetectRequest{JobId: "job-1"})
}

func (r *jobRun) started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.events...)
}

func entry(table string) *report.TableEntry {
	return &report.TableEntry{
		TableSchema: "public", TableName: table,
		ReportKey:       tableReportKey(testRunId, "public", table),
		ScanFingerprint: "fingerprint-" + table,
	}
}

// requireReport compares an index with what is expected of it, keys included.
func requireReport(t *testing.T, want, got *report.JobReport) {
	t.Helper()
	require.NotNil(t, got)
	require.Len(t, got.SuccessfulTableReports, len(want.SuccessfulTableReports))
	for i, wantEntry := range want.SuccessfulTableReports {
		gotEntry := got.SuccessfulTableReports[i]
		require.Equal(t, wantEntry.TableSchema+"."+wantEntry.TableName, gotEntry.TableSchema+"."+gotEntry.TableName)
		require.Equal(t, wantEntry.ScanFingerprint, gotEntry.ScanFingerprint, wantEntry.TableName)
		require.Equal(t, wantEntry.Incomplete, gotEntry.Incomplete, wantEntry.TableName)
		require.Equal(t, wantEntry.ReportKey.GetJobRunId(), gotEntry.ReportKey.GetJobRunId(), wantEntry.TableName)
		require.Equal(t, wantEntry.ReportKey.GetExternalId(), gotEntry.ReportKey.GetExternalId(), wantEntry.TableName)
		require.Equal(t, wantEntry.ReportKey.GetAccountId(), gotEntry.ReportKey.GetAccountId(), wantEntry.TableName)
	}
	require.Equal(t, want.FailedTables, got.FailedTables)
}

// A run of a job with every setting: what each activity and each table is given, what is
// saved, what the run returns, and that it reads no version beyond the license's.
func Test_JobPiiDetect_ScansTheTablesOfTheJob(t *testing.T) {
	run := newJobRun(t, 3)
	run.scanTables(scanned)
	versions := watchVersions(run.env)
	filter := &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
		Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{
			Include: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns{Schemas: []string{"public"}},
		},
	}
	prompt := "Columns named ref_* hold customer references."
	run.withDetails(&GetPiiDetectJobDetailsResponse{
		AccountId: "account-1",
		PiiDetectConfig: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
			DataSampling:    &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true},
			TableScanFilter: filter,
			UserPrompt:      &prompt,
		},
		SourceConnectionId: "connection-1",
		ModelInput:         "values",
	})
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.MatchedBy(func(req *GetTablesToPiiScanRequest) bool {
		return req.AccountId == "account-1" && req.JobId == "job-1" && req.SourceConnectionId == "connection-1" &&
			req.Filter.GetInclude().GetSchemas()[0] == "public" && req.IncrementalConfig == nil &&
			req.Sampling && req.UserPrompt == prompt && req.ModelInput == "values" && req.MarksIncomplete
	})).Return(&GetTablesToPiiScanResponse{Tables: []TableToScan{
		{Schema: "public", Table: "orders", Fingerprint: "fingerprint-orders"},
		{Schema: "public", Table: "customers", Fingerprint: "fingerprint-customers"},
	}}, nil).Once()
	saved := run.savesReport()

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	require.NoError(t, run.env.GetWorkflowError())
	require.JSONEq(t,
		`{"ReportKey":{"jobRunId":"default-test-workflow-id","externalId":"job-1--job-pii-report","accountId":"account-1"}}`,
		resultJSON(t, run.env),
	)

	runId := testRunId
	require.ElementsMatch(t, []*TablePiiDetectRequest{
		{
			AccountId: "account-1", JobId: "job-1", ConnectionId: "connection-1",
			TableSchema: "public", TableName: "orders",
			ShouldSampleData: true, UserPrompt: prompt, ParentExecutionId: &runId, ModelInput: "values",
		},
		{
			AccountId: "account-1", JobId: "job-1", ConnectionId: "connection-1",
			TableSchema: "public", TableName: "customers",
			ShouldSampleData: true, UserPrompt: prompt, ParentExecutionId: &runId, ModelInput: "values",
		},
	}, run.tables)

	// The entries of the index are in the order of their names.
	requireReport(t, &report.JobReport{SuccessfulTableReports: []*report.TableEntry{entry("customers"), entry("orders")}}, *saved)
	require.Equal(t, []string{createdKind, succeededKind}, run.started())
	require.Equal(t, []string{"license-read-recorded-1", "license-feature-read-recorded-1"}, versions.all())
	run.env.AssertExpectations(t)
}

func Test_JobPiiDetect_WithoutALicense(t *testing.T) {
	run := newJobRunUnder(t, testutil.NewFakeEELicense(), 3)

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	require.ErrorContains(t, run.env.GetWorkflowError(), "ee license is not valid, unable to run pii detect")
	run.env.AssertNotCalled(t, "GetPiiDetectJobDetails", mock.Anything, mock.Anything)
	require.Empty(t, run.started())
}

// PII detection is a feature of its own. A run under a valid license that lacks it fails
// before it asks for anything, and names the feature: this workflow never asks the API
// whether the job may run, so that a run a schedule starts is held here and nowhere else.
func Test_PiiDetect_FailsWithoutTheFeature(t *testing.T) {
	run := newJobRunUnder(t, testutil.NewFakeEELicense(
		testutil.WithIsValid(),
		testutil.WithFeatures(license.FeatureAccountHooks, license.FeaturePiiText),
	), 3)

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, run.env.GetWorkflowError(), &appErr)
	require.Equal(t, "this license does not include pii_detection", appErr.Message())
	run.env.AssertNotCalled(t, "GetPiiDetectJobDetails", mock.Anything, mock.Anything)
	run.env.AssertNotCalled(t, "GetLastSuccessfulWorkflowId", mock.Anything, mock.Anything)
	run.env.AssertNotCalled(t, "GetTablesToPiiScan", mock.Anything, mock.Anything)
	run.env.AssertNotCalled(t, "SaveJobPiiDetectReport", mock.Anything, mock.Anything)
	require.Empty(t, run.tables, "no table is scanned")
	require.Empty(t, run.started(), "no event is announced")
}

// A run under a license that includes PII detection scans its tables. Its events are
// announced when the license includes the account hooks too, which are another feature.
func Test_PiiDetect_RunsWithTheFeature(t *testing.T) {
	for _, tt := range []struct {
		name     string
		features []license.Feature
		// earlier says the run started before the features were asked.
		earlier bool
		events  []string
	}{
		{
			name:     "PII detection without the account hooks",
			features: []license.Feature{license.FeaturePiiDetection},
			events:   []string{},
		},
		{
			name:     "PII detection and the account hooks",
			features: []license.Feature{license.FeaturePiiDetection, license.FeatureAccountHooks},
			events:   []string{createdKind, succeededKind},
		},
		{
			// It ran and announced its events under a valid license, whatever the license
			// included: it goes on as it started.
			name:     "neither, the run started before they were asked",
			features: []license.Feature{},
			earlier:  true,
			events:   []string{createdKind, succeededKind},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run := newJobRunUnder(t, testutil.NewFakeEELicense(
				testutil.WithIsValid(), testutil.WithFeatures(tt.features...),
			), 3)
			if tt.earlier {
				run.env.OnGetVersion("license-feature-read-recorded", workflow.DefaultVersion, 1).
					Return(workflow.DefaultVersion)
			}
			run.scanTables(scanned)
			run.withDetails(plainDetails())
			run.withTables("orders")
			saved := run.savesReport()

			run.execute()

			require.True(t, run.env.IsWorkflowCompleted())
			require.NoError(t, run.env.GetWorkflowError())
			requireReport(t, &report.JobReport{SuccessfulTableReports: []*report.TableEntry{entry("orders")}}, *saved)
			require.Equal(t, tt.events, run.started())
		})
	}
}

// A job whose details cannot be read has no account to tell: the run fails before any
// event.
func Test_JobPiiDetect_FailsWhenTheDetailsCannotBeRead(t *testing.T) {
	run := newJobRun(t, 3)
	var attempts atomic.Int32
	run.env.OnActivity(run.activities.GetPiiDetectJobDetails, mock.Anything, mock.Anything).
		Return(func(context.Context, *GetPiiDetectJobDetailsRequest) (*GetPiiDetectJobDetailsResponse, error) {
			attempts.Add(1)
			return nil, errors.New("the API is away")
		})

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	require.ErrorContains(t, run.env.GetWorkflowError(), "the API is away")
	require.EqualValues(t, 3, attempts.Load())
	require.Empty(t, run.started())
	run.env.AssertNotCalled(t, "GetTablesToPiiScan", mock.Anything, mock.Anything)
}

func Test_JobPiiDetect_NoTable(t *testing.T) {
	run := newJobRun(t, 3)
	run.withDetails(plainDetails())
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, &GetTablesToPiiScanRequest{
		AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1", MarksIncomplete: true,
	}).Return(&GetTablesToPiiScanResponse{Tables: []TableToScan{}}, nil).Once()
	saved := run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.NotNil(t, (*saved).SuccessfulTableReports, "an empty index lists no table: it does not lack its list")
	require.Empty(t, (*saved).SuccessfulTableReports)
	require.Empty(t, run.tables)
	require.Equal(t, []string{createdKind, succeededKind}, run.started())
	run.env.AssertNotCalled(t, "GetLastSuccessfulWorkflowId", mock.Anything, mock.Anything)
}

func incrementalDetails() *GetPiiDetectJobDetailsResponse {
	details := plainDetails()
	details.PiiDetectConfig.Incremental = &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_Incremental{IsEnabled: true}
	return details
}

// An incremental run scans the tables the listing returns and keeps the entries of the
// earlier run for the others.
func Test_JobPiiDetect_Incremental(t *testing.T) {
	run := newJobRun(t, 3)
	run.withDetails(incrementalDetails())
	earlier := "earlier-run"
	run.env.OnActivity(run.activities.GetLastSuccessfulWorkflowId, mock.Anything, &GetLastSuccessfulWorkflowIdRequest{
		AccountId: "account-1", JobId: "job-1",
	}).Return(&GetLastSuccessfulWorkflowIdResponse{WorkflowId: &earlier}, nil).Once()

	earlierEntry := func(table string) *report.TableEntry {
		return &report.TableEntry{
			TableSchema: "public", TableName: table,
			ReportKey:       tableReportKey("earlier-run", "public", table),
			ScanFingerprint: "earlier-fingerprint-" + table,
		}
	}
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.MatchedBy(func(req *GetTablesToPiiScanRequest) bool {
		return req.IncrementalConfig != nil && req.IncrementalConfig.LastWorkflowId == "earlier-run"
	})).Return(&GetTablesToPiiScanResponse{
		Tables: []TableToScan{
			{Schema: "public", Table: "changed", Fingerprint: "fingerprint-changed"},
			{Schema: "public", Table: "new", Fingerprint: "fingerprint-new"},
			{Schema: "public", Table: "changed_and_failing", Fingerprint: "fingerprint-changed_and_failing"},
		},
		PreviousReports: []*report.TableEntry{
			earlierEntry("unchanged"), earlierEntry("changed"), earlierEntry("changed_and_failing"),
		},
	}, nil).Once()
	run.scanTables(func(req *TablePiiDetectRequest) (string, error) {
		if req.TableName == "changed_and_failing" {
			return "", errors.New("the columns cannot be read")
		}
		return report.ModelAnswered, nil
	})
	run.env.OnGetVersion("pii-detect-incomplete-run-fails", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	saved := run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	previous := map[string]string{}
	for _, table := range run.tables {
		previous[table.TableName] = table.PreviousResultsKey.GetJobRunId()
	}
	require.Equal(t, map[string]string{"changed": "earlier-run", "new": "", "changed_and_failing": "earlier-run"}, previous)

	// A table scanned by this run replaces its earlier entry; one that could not be
	// scanned keeps it; one that was not listed keeps it too.
	requireReport(t, &report.JobReport{
		SuccessfulTableReports: []*report.TableEntry{
			entry("changed"), earlierEntry("changed_and_failing"), entry("new"), earlierEntry("unchanged"),
		},
		FailedTables: []*report.FailedTable{
			{TableSchema: "public", TableName: "changed_and_failing", Reason: "the columns cannot be read"},
		},
	}, *saved)
}

// No earlier run to start from: every table is scanned.
func Test_JobPiiDetect_IncrementalWithoutAnEarlierRun(t *testing.T) {
	run := newJobRun(t, 3)
	run.scanTables(scanned)
	run.withDetails(incrementalDetails())
	run.env.OnActivity(run.activities.GetLastSuccessfulWorkflowId, mock.Anything, mock.Anything).
		Return(&GetLastSuccessfulWorkflowIdResponse{}, nil).Once()
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.MatchedBy(func(req *GetTablesToPiiScanRequest) bool {
		return req.IncrementalConfig == nil
	})).Return(&GetTablesToPiiScanResponse{Tables: []TableToScan{{Schema: "public", Table: "t1"}}}, nil).Once()
	run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	run.env.AssertExpectations(t)
}

// The steps that fail the run, each after the event of its start: the event of its
// failure follows.
func Test_JobPiiDetect_FailsWhenAStepFails(t *testing.T) {
	for name, tt := range map[string]struct {
		failing string
		message string
	}{
		"the earlier run cannot be looked up": {"GetLastSuccessfulWorkflowId", "the previous successful run of the job was not found out: "},
		"the tables cannot be listed":         {"GetTablesToPiiScan", ""},
		"the index cannot be saved":           {"SaveJobPiiDetectReport", "the index of the table reports was not saved: "},
	} {
		t.Run(name, func(t *testing.T) {
			run := newJobRun(t, 3)
			run.scanTables(scanned)
			run.withDetails(incrementalDetails())
			fail := func(step string) error {
				if step == tt.failing {
					return errors.New(step + " is failing")
				}
				return nil
			}
			run.env.OnActivity(run.activities.GetLastSuccessfulWorkflowId, mock.Anything, mock.Anything).
				Return(func(context.Context, *GetLastSuccessfulWorkflowIdRequest) (*GetLastSuccessfulWorkflowIdResponse, error) {
					return &GetLastSuccessfulWorkflowIdResponse{}, fail("GetLastSuccessfulWorkflowId")
				})
			run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.Anything).
				Return(func(context.Context, *GetTablesToPiiScanRequest) (*GetTablesToPiiScanResponse, error) {
					return &GetTablesToPiiScanResponse{Tables: []TableToScan{{Schema: "public", Table: "t1"}}}, fail("GetTablesToPiiScan")
				})
			run.env.OnActivity(run.activities.SaveJobPiiDetectReport, mock.Anything, mock.Anything).
				Return(func(context.Context, *SaveJobPiiDetectReportRequest) (*SaveJobPiiDetectReportResponse, error) {
					return &SaveJobPiiDetectReportResponse{Key: jobKey}, fail("SaveJobPiiDetectReport")
				})

			run.execute()

			require.True(t, run.env.IsWorkflowCompleted())
			require.ErrorContains(t, run.env.GetWorkflowError(), tt.message)
			require.ErrorContains(t, run.env.GetWorkflowError(), tt.failing+" is failing")
			require.Equal(t, []string{createdKind, failedKind}, run.started())
		})
	}
}

// A table that cannot be scanned does not stop the others. The run saves what it has,
// names the table in its index, then ends failed: a run that ends well says that every
// table was scanned.
func Test_JobPiiDetect_ATableThatFailsFailsTheRunOnceTheIndexIsSaved(t *testing.T) {
	run := newJobRun(t, 3)
	versions := watchVersions(run.env)
	run.withDetails(plainDetails())
	run.withTables("t1", "broken", "t3", "silent_model")
	run.scanTables(func(req *TablePiiDetectRequest) (string, error) {
		switch req.TableName {
		case "broken":
			return "", errors.New("the columns cannot be read")
		case "silent_model":
			return report.ModelFailed, nil
		}
		return report.ModelAnswered, nil
	})
	saved := run.savesReport()

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, run.env.GetWorkflowError(), &appErr)
	require.Equal(t, "ScanIncomplete", appErr.Type())
	require.Equal(t,
		"2 of 4 tables were not fully scanned: public.broken (failed), public.silent_model (the model did not answer)",
		appErr.Message(),
	)

	silent := entry("silent_model")
	silent.Incomplete = true
	requireReport(t, &report.JobReport{
		SuccessfulTableReports: []*report.TableEntry{silent, entry("t1"), entry("t3")},
		FailedTables: []*report.FailedTable{
			{TableSchema: "public", TableName: "broken", Reason: "the columns cannot be read"},
		},
	}, *saved)
	require.Len(t, run.tables, 4, "every table is attempted")
	require.Equal(t, []string{createdKind, failedKind}, run.started())
	require.Equal(t, []string{"license-read-recorded-1", "license-feature-read-recorded-1", "pii-detect-incomplete-run-fails-1"}, versions.all())
}

// Runs started before an incomplete scan failed the run replay as they ran: they
// complete, on the index of what was scanned.
func Test_JobPiiDetect_ATableThatFailsLeavesARunThatStartedEarlierComplete(t *testing.T) {
	run := newJobRun(t, 3)
	run.env.OnGetVersion("pii-detect-incomplete-run-fails", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	run.withDetails(plainDetails())
	run.withTables("t1", "broken")
	run.scanTables(func(req *TablePiiDetectRequest) (string, error) {
		if req.TableName == "broken" {
			return "", errors.New("the columns cannot be read")
		}
		return "", nil
	})
	saved := run.savesReport()

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	require.NoError(t, run.env.GetWorkflowError())
	require.Len(t, (*saved).SuccessfulTableReports, 1)
	require.Len(t, (*saved).FailedTables, 1)
	require.Equal(t, []string{createdKind, succeededKind}, run.started())
}

// The failed tables are listed in the order of their names, whatever the order they
// failed in.
func Test_JobPiiDetect_TheFailedTablesAreInTheOrderOfTheirNames(t *testing.T) {
	run := newJobRun(t, 1)
	run.env.OnGetVersion("pii-detect-incomplete-run-fails", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	run.withDetails(plainDetails())
	run.withTables("zebra", "mango", "apple")
	run.scanTables(func(*TablePiiDetectRequest) (string, error) { return "", errors.New("the columns cannot be read") })
	saved := run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	names := []string{}
	for _, failed := range (*saved).FailedTables {
		names = append(names, failed.TableName)
	}
	require.Equal(t, []string{"apple", "mango", "zebra"}, names)
}

// The reason of a failure is cut, so that a long one does not weigh on the index.
func Test_JobPiiDetect_TheReasonOfAFailedTableIsCut(t *testing.T) {
	run := newJobRun(t, 3)
	run.withDetails(plainDetails())
	run.withTables("broken")
	long := ""
	for range 100 {
		long += "0123456789"
	}
	run.scanTables(func(*TablePiiDetectRequest) (string, error) { return "", errors.New(long) })
	saved := run.savesReport()

	run.execute()

	require.Len(t, (*saved).FailedTables, 1)
	require.Len(t, (*saved).FailedTables[0].Reason, 300)
}

// A reason is cut on a character, never inside one.
func Test_CutReason(t *testing.T) {
	require.Equal(t, "short", cutReason("short"))
	cut := cutReason(strings.Repeat("é", 200))
	require.Equal(t, strings.Repeat("é", 150), cut)
	require.LessOrEqual(t, len(cutReason("a"+strings.Repeat("é", 200))), 300)
	require.True(t, utf8.ValidString(cutReason("a"+strings.Repeat("é", 200))))
}

func Test_IncompleteMessage(t *testing.T) {
	var tables []string
	for i := range 12 {
		tables = append(tables, fmt.Sprintf("public.t%02d (failed)", i))
	}
	require.Equal(t,
		"12 of 20 tables were not fully scanned: public.t00 (failed), public.t01 (failed), public.t02 (failed), "+
			"public.t03 (failed), public.t04 (failed), public.t05 (failed), public.t06 (failed), public.t07 (failed), "+
			"public.t08 (failed), public.t09 (failed), …",
		incompleteMessage(tables, 20),
	)
	require.Equal(t, "1 of 1 tables were not fully scanned: public.a (failed)", incompleteMessage([]string{"public.a (failed)"}, 1))
}

// A run canceled while its tables are scanned ends canceled: no other table starts, no
// index is stored, no event of its end is sent.
func Test_JobPiiDetect_Canceled(t *testing.T) {
	run := newJobRun(t, 3)
	run.scanTables(scanned)
	versions := watchVersions(run.env)
	run.withDetails(plainDetails())
	run.withTables("t1", "t2", "t3", "t4")
	var saves atomic.Int32
	run.env.OnActivity(run.activities.SaveJobPiiDetectReport, mock.Anything, mock.Anything).
		Return(func(context.Context, *SaveJobPiiDetectReportRequest) (*SaveJobPiiDetectReportResponse, error) {
			saves.Add(1)
			return &SaveJobPiiDetectReportResponse{Key: jobKey}, nil
		})
	run.env.RegisterDelayedCallback(run.env.CancelWorkflow, 30*time.Second)

	run.execute()

	require.True(t, run.env.IsWorkflowCompleted())
	require.True(t, temporal.IsCanceledError(run.env.GetWorkflowError()), "%v", run.env.GetWorkflowError())
	require.Len(t, run.tables, 3, "the fourth table never starts")
	require.Zero(t, saves.Load())
	require.Equal(t, []string{createdKind}, run.started())
	require.Equal(t, []string{"license-read-recorded-1", "license-feature-read-recorded-1"}, versions.all())
}
