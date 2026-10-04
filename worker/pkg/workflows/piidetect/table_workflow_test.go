package piidetect

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// versionReads keeps the change ids a run reads, as the search attribute that goes with
// every recorded version names them: "<change id>-<version>".
type versionReads struct {
	mu   sync.Mutex
	read []string
}

func watchVersions(env *testsuite.TestWorkflowEnvironment) *versionReads {
	reads := &versionReads{}
	env.OnUpsertSearchAttributes(mock.Anything).Run(func(args mock.Arguments) {
		attributes, _ := args.Get(0).(map[string]any)
		changes, _ := attributes["TemporalChangeVersion"].([]string)
		reads.mu.Lock()
		defer reads.mu.Unlock()
		// The attribute lists every version of the run so far, the latest first.
		if len(changes) > 0 {
			reads.read = append(reads.read, changes[0])
		}
	}).Return(nil).Maybe()
	return reads
}

// resultJSON returns the result of the run as it is serialized.
func resultJSON(t *testing.T, env *testsuite.TestWorkflowEnvironment) string {
	t.Helper()
	var raw json.RawMessage
	require.NoError(t, env.GetWorkflowResult(&raw))
	return string(raw)
}

func (r *versionReads) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.read...)
}

func newTableEnv(t *testing.T) (*testsuite.TestWorkflowEnvironment, *Activities) {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()
	env.SetTestTimeout(30 * time.Second)
	activities := NewActivities(nil, nil, nil, nil, nil, &Config{})
	env.RegisterActivity(activities.GetColumnData)
	env.RegisterActivity(activities.DetectPiiRegex)
	env.RegisterActivity(activities.DetectPiiLLM)
	env.RegisterActivity(activities.SaveTablePiiDetectReport)
	return env, activities
}

var (
	tableKey = &mgmtv1alpha1.RunContextKey{
		JobRunId: "run-1", ExternalId: "public.customers--table-pii-report", AccountId: "account-1",
	}
	parentRunId = "run-1"
)

func tableRequest() *TablePiiDetectRequest {
	return &TablePiiDetectRequest{
		AccountId:         "account-1",
		JobId:             "job-1",
		ConnectionId:      "connection-1",
		TableSchema:       "public",
		TableName:         "customers",
		ShouldSampleData:  true,
		UserPrompt:        "Columns named ref_* hold customer references.",
		ParentExecutionId: &parentRunId,
	}
}

func sampledColumns() *GetColumnDataResponse {
	return &GetColumnDataResponse{
		ColumnData: []*ColumnData{
			{Column: "id", DataType: "uuid"},
			{Column: "email", DataType: "text", IsNullable: true, Profile: &profile.Profile{Rows: 200, Kind: profile.KindText}},
			{Column: "c17", DataType: "text", IsNullable: true},
			{Column: "note", DataType: "text", IsNullable: true},
		},
		SampledRows: 200,
	}
}

// The four steps of a table, what each is given, and what the run stores and returns.
func Test_TablePiiDetect_ScansATable(t *testing.T) {
	env, activities := newTableEnv(t)
	versions := watchVersions(env)
	columns := sampledColumns()

	env.OnActivity(activities.GetColumnData, mock.Anything, &GetColumnDataRequest{
		ConnectionId: "connection-1", TableSchema: "public", TableName: "customers", Sample: true,
	}).Return(columns, nil).Once()
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, &DetectPiiRegexRequest{ColumnData: columns.ColumnData}).
		Return(&DetectPiiRegexResponse{
			PiiColumns: map[string]report.Category{"email": report.Contact, "c17": report.Financial},
			Evidence:   map[string]string{"email": "name", "c17": "values:iban 0.97"},
		}, nil).Once()
	// The model activity is never told to read rows through ShouldSample, and gets no
	// connection when the job sends no value.
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "customers", ColumnData: columns.ColumnData,
		UserPrompt: "Columns named ref_* hold customer references.",
	}).Return(&DetectPiiLLMResponse{
		PiiColumns:     map[string]report.ModelFinding{"email": {Category: report.Contact, Confidence: 0.98}, "note": {Category: report.Personal, Confidence: 0.6}},
		Input:          report.InputProfiles,
		Status:         report.ModelPartial,
		Model:          "local-model",
		Unanswered:     []string{"id"},
		BelowThreshold: []report.Dismissed{{ColumnName: "c17", Category: report.Financial, Confidence: 0.3}},
	}, nil).Once()

	merged := map[string]report.Combined{
		"email": {Regex: &report.RuleFinding{Category: report.Contact, Evidence: "name"}, LLM: &report.ModelFinding{Category: report.Contact, Confidence: 0.98}},
		"c17":   {Regex: &report.RuleFinding{Category: report.Financial, Evidence: "values:iban 0.97"}},
		"note":  {LLM: &report.ModelFinding{Category: report.Personal, Confidence: 0.6}},
	}
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, &SaveTablePiiDetectReportRequest{
		ParentRunId: &parentRunId, AccountId: "account-1", TableSchema: "public", TableName: "customers",
		Report:         merged,
		ScannedColumns: []string{"id", "email", "c17", "note"},
		Scan: &report.Scan{
			SampledRows: 200, Input: report.InputProfiles, Model: "local-model", ModelStatus: report.ModelPartial,
			Sources:        []string{report.SourceRules, report.SourceModel},
			Unanswered:     []string{"id"},
			BelowThreshold: []report.Dismissed{{ColumnName: "c17", Category: report.Financial, Confidence: 0.3}},
		},
	}).Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil).Once()

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var response *TablePiiDetectResponse
	require.NoError(t, env.GetWorkflowResult(&response))
	require.Equal(t, merged, response.PiiColumns)
	require.Equal(t, tableKey.GetExternalId(), response.ResultKey.GetExternalId())
	require.Equal(t, report.ModelPartial, response.Model)
	require.Empty(t, versions.all(), "a run in which nothing fails reads no version")
	env.AssertExpectations(t)
}

// Activities of a worker that knows neither profiles nor statuses answer with fewer
// members: the run stores what it got and returns the result such runs always returned.
func Test_TablePiiDetect_WithAnswersThatHoldNoneOfTheOptionalMembers(t *testing.T) {
	env, activities := newTableEnv(t)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).
		Return(&GetColumnDataResponse{ColumnData: []*ColumnData{{Column: "phone", DataType: "text"}}}, nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).
		Return(&DetectPiiRegexResponse{PiiColumns: map[string]report.Category{"phone": report.Contact}}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).
		Return(&DetectPiiLLMResponse{PiiColumns: map[string]report.ModelFinding{}}, nil)
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.MatchedBy(func(req *SaveTablePiiDetectReportRequest) bool {
		return req.Scan != nil && req.Scan.ModelStatus == report.ModelAnswered && req.Scan.Input == "" && req.Scan.SampledRows == 0 &&
			slices.Equal(req.Scan.Sources, []string{report.SourceRules, report.SourceModel})
	})).Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil).Once()

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.NoError(t, env.GetWorkflowError())
	require.JSONEq(t, `{
		"PiiColumns": {"phone": {"regex": {"category": "contact"}, "llm": null}},
		"ResultKey": {"jobRunId": "run-1", "externalId": "public.customers--table-pii-report", "accountId": "account-1"}
	}`, resultJSON(t, env))
	env.AssertExpectations(t)
}

// A table without column, or in which nothing is found, has an empty report, not none.
func Test_TablePiiDetect_NothingFound(t *testing.T) {
	env, activities := newTableEnv(t)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).
		Return(&GetColumnDataResponse{ColumnData: []*ColumnData{}}, nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).Return(&DetectPiiLLMResponse{Status: report.ModelNone}, nil)
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.MatchedBy(func(req *SaveTablePiiDetectReportRequest) bool {
		// A table scanned without a model says that its report rests on the rules alone.
		return req.Report != nil && len(req.Report) == 0 && req.Scan.ModelStatus == report.ModelNone &&
			slices.Equal(req.Scan.Sources, []string{report.SourceRules})
	})).Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil).Once()

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.NoError(t, env.GetWorkflowError())
	require.JSONEq(t, `{
		"PiiColumns": {},
		"ResultKey": {"jobRunId": "run-1", "externalId": "public.customers--table-pii-report", "accountId": "account-1"},
		"Model": "none"
	}`, resultJSON(t, env))
	env.AssertExpectations(t)
}

// A job that sends values: the model activity gets the connection and is asked for values
// through the member only it knows, and has a single attempt.
func Test_TablePiiDetect_WithTheValuesInput(t *testing.T) {
	env, activities := newTableEnv(t)
	columns := sampledColumns()
	env.OnActivity(activities.GetColumnData, mock.Anything, &GetColumnDataRequest{
		ConnectionId: "connection-1", TableSchema: "public", TableName: "customers", Sample: true,
	}).Return(columns, nil).Once()
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)

	var attempts atomic.Int32
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "customers", ColumnData: columns.ColumnData,
		ShouldSample: false, ConnectionId: "connection-1",
		UserPrompt: "Columns named ref_* hold customer references.",
		Input:      "values",
	}).Return(func(context.Context, *DetectPiiLLMRequest) (*DetectPiiLLMResponse, error) {
		attempts.Add(1)
		return nil, errors.New("the endpoint is away")
	})
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything).
		Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil)

	request := tableRequest()
	request.ModelInput = "values"
	env.ExecuteWorkflow(TablePiiDetect, request)

	require.NoError(t, env.GetWorkflowError())
	require.EqualValues(t, 1, attempts.Load(), "values picked once are the only ones that leave: no second attempt")
	env.AssertExpectations(t)
}

// Without sampling no row is read, whatever the input the job names.
func Test_TablePiiDetect_WithoutSampling(t *testing.T) {
	env, activities := newTableEnv(t)
	env.OnActivity(activities.GetColumnData, mock.Anything, &GetColumnDataRequest{
		ConnectionId: "connection-1", TableSchema: "public", TableName: "customers",
	}).Return(&GetColumnDataResponse{ColumnData: []*ColumnData{{Column: "id"}}}, nil).Once()
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, &DetectPiiLLMRequest{
		TableSchema: "public", TableName: "customers", ColumnData: []*ColumnData{{Column: "id"}},
	}).Return(&DetectPiiLLMResponse{}, nil).Once()
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything).
		Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil)

	request := tableRequest()
	request.ShouldSampleData = false
	request.UserPrompt = ""
	request.ModelInput = "values"
	env.ExecuteWorkflow(TablePiiDetect, request)

	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

// A model that cannot be asked does not cost the table what the rules found: the report
// is saved and says so. Only then is the version of that change read.
func Test_TablePiiDetect_AFailedModelIsTolerated(t *testing.T) {
	env, activities := newTableEnv(t)
	versions := watchVersions(env)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).Return(sampledColumns(), nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).
		Return(&DetectPiiRegexResponse{PiiColumns: map[string]report.Category{"email": report.Contact}}, nil)
	var attempts atomic.Int32
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).
		Return(func(context.Context, *DetectPiiLLMRequest) (*DetectPiiLLMResponse, error) {
			attempts.Add(1)
			return nil, errors.New("the endpoint is away")
		})
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.MatchedBy(func(req *SaveTablePiiDetectReportRequest) bool {
		return len(req.Report) == 1 && req.Report["email"].Regex != nil && req.Report["email"].LLM == nil &&
			req.Scan.ModelStatus == report.ModelFailed && req.Scan.SampledRows == 200 && req.Scan.Input == "" &&
			slices.Equal(req.Scan.Sources, []string{report.SourceRules})
	})).Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil).Once()

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var response *TablePiiDetectResponse
	require.NoError(t, env.GetWorkflowResult(&response))
	require.Equal(t, report.ModelFailed, response.Model)
	require.Len(t, response.PiiColumns, 1)
	require.EqualValues(t, 3, attempts.Load(), "the model is attempted three times")
	require.Equal(t, []string{"pii-detect-model-failure-tolerated-1"}, versions.all())
	env.AssertExpectations(t)
}

// Runs started before the failure was tolerated replay as they ran: the table fails and
// nothing is saved.
func Test_TablePiiDetect_AFailedModelFailsARunThatStartedBeforeItWasTolerated(t *testing.T) {
	env, activities := newTableEnv(t)
	env.OnGetVersion("pii-detect-model-failure-tolerated", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).Return(sampledColumns(), nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).Return(nil, errors.New("the endpoint is away"))

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "the endpoint is away")
	env.AssertNotCalled(t, "SaveTablePiiDetectReport", mock.Anything, mock.Anything)
}

// Every other step fails the table.
func Test_TablePiiDetect_FailsWhenAStepOtherThanTheModelFails(t *testing.T) {
	for name, tt := range map[string]struct {
		failing  string
		attempts int32
		notRun   []string
	}{
		"the columns cannot be read": {"GetColumnData", 3, []string{"DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport"}},
		"the rules fail":             {"DetectPiiRegex", 3, []string{"DetectPiiLLM", "SaveTablePiiDetectReport"}},
		"the report cannot be saved": {"SaveTablePiiDetectReport", 5, nil},
	} {
		t.Run(name, func(t *testing.T) {
			env, activities := newTableEnv(t)
			versions := watchVersions(env)
			var attempts atomic.Int32
			fail := func(step string) error {
				if step != tt.failing {
					return nil
				}
				attempts.Add(1)
				return errors.New(step + " is failing")
			}
			env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).
				Return(func(context.Context, *GetColumnDataRequest) (*GetColumnDataResponse, error) {
					return sampledColumns(), fail("GetColumnData")
				})
			env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).
				Return(func(context.Context, *DetectPiiRegexRequest) (*DetectPiiRegexResponse, error) {
					return &DetectPiiRegexResponse{}, fail("DetectPiiRegex")
				})
			env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).Return(&DetectPiiLLMResponse{}, nil)
			env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything).
				Return(func(context.Context, *SaveTablePiiDetectReportRequest) (*SaveTablePiiDetectReportResponse, error) {
					return &SaveTablePiiDetectReportResponse{Key: tableKey}, fail("SaveTablePiiDetectReport")
				})

			env.ExecuteWorkflow(TablePiiDetect, tableRequest())

			require.True(t, env.IsWorkflowCompleted())
			require.ErrorContains(t, env.GetWorkflowError(), tt.failing+" is failing")
			require.Equal(t, tt.attempts, attempts.Load())
			for _, step := range tt.notRun {
				env.AssertNotCalled(t, step, mock.Anything, mock.Anything)
			}
			require.Empty(t, versions.all())
		})
	}
}

// A run canceled while the model is asked ends canceled: the failure of the model
// activity is then not one to tolerate, and no version is read.
func Test_TablePiiDetect_CanceledWhileTheModelIsAsked(t *testing.T) {
	env, activities := newTableEnv(t)
	versions := watchVersions(env)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).Return(sampledColumns(), nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).
		Return(func(context.Context, *DetectPiiLLMRequest) (*DetectPiiLLMResponse, error) {
			env.CancelWorkflow()
			return nil, errors.New("interrupted")
		})

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, env.IsWorkflowCompleted())
	require.True(t, temporal.IsCanceledError(env.GetWorkflowError()), "%v", env.GetWorkflowError())
	env.AssertNotCalled(t, "SaveTablePiiDetectReport", mock.Anything, mock.Anything)
	require.Empty(t, versions.all())
}

// A model activity that ends canceled is not a failure of the model to tolerate, whether
// or not the run itself was asked to cancel: the table ends on it, and no version is read.
func Test_TablePiiDetect_AModelActivityThatEndsCanceled(t *testing.T) {
	env, activities := newTableEnv(t)
	versions := watchVersions(env)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).Return(sampledColumns(), nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).
		Return(nil, temporal.NewCanceledError("the worker is stopping"))

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())

	require.True(t, env.IsWorkflowCompleted())
	require.True(t, temporal.IsCanceledError(env.GetWorkflowError()), "%v", env.GetWorkflowError())
	env.AssertNotCalled(t, "SaveTablePiiDetectReport", mock.Anything, mock.Anything)
	require.Empty(t, versions.all())
}

// What the workflow asks of each activity: how long it may run, whether it must report
// that it is alive.
func Test_TablePiiDetect_ActivityOptions(t *testing.T) {
	env, activities := newTableEnv(t)
	env.OnActivity(activities.GetColumnData, mock.Anything, mock.Anything).Return(sampledColumns(), nil)
	env.OnActivity(activities.DetectPiiRegex, mock.Anything, mock.Anything).Return(&DetectPiiRegexResponse{}, nil)
	env.OnActivity(activities.DetectPiiLLM, mock.Anything, mock.Anything).Return(&DetectPiiLLMResponse{}, nil)
	env.OnActivity(activities.SaveTablePiiDetectReport, mock.Anything, mock.Anything).
		Return(&SaveTablePiiDetectReportResponse{Key: tableKey}, nil)

	var mu sync.Mutex
	var order []string
	options := map[string]activity.Info{}
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, _ converter.EncodedValues) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, info.ActivityType.Name)
		options[info.ActivityType.Name] = *info
	})

	env.ExecuteWorkflow(TablePiiDetect, tableRequest())
	require.NoError(t, env.GetWorkflowError())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport"}, order)
	for name, want := range map[string]struct{ startToClose, heartbeat time.Duration }{
		"GetColumnData":            {2 * time.Minute, 0},
		"DetectPiiRegex":           {time.Minute, 0},
		"DetectPiiLLM":             {30 * time.Minute, 3 * time.Minute},
		"SaveTablePiiDetectReport": {time.Minute, 0},
	} {
		require.Equal(t, want.startToClose, options[name].StartToCloseTimeout, name)
		require.Equal(t, want.heartbeat, options[name].HeartbeatTimeout, name)
	}
}
