package piidetect

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
)

// registered keeps the names a Registry is given.
type registered struct {
	workflows  []string
	activities []string
}

func (r *registered) RegisterWorkflow(w any) { r.workflows = append(r.workflows, functionName(w)) }
func (r *registered) RegisterActivity(a any) { r.activities = append(r.activities, functionName(a)) }

// functionName is the name Temporal registers a function under: its own, without its
// package, its receiver or the suffix of a method value.
func functionName(function any) string {
	name := runtime.FuncForPC(reflect.ValueOf(function).Pointer()).Name()
	name = strings.TrimSuffix(name, "-fm")
	return name[strings.LastIndex(name, ".")+1:]
}

// The names below are in the histories of the runs in flight and in the schedules of the
// jobs: a run recorded under another name could not continue, a schedule could not start
// its workflow. The API reads the two workflow names as well.
func Test_Register_KeepsTheRegisteredNames(t *testing.T) {
	names := &registered{}
	Register(names, testutil.NewFakeEELicense(), NewActivities(nil, nil, nil, nil, nil, Config{}), Config{})

	require.Equal(t, []string{"JobPiiDetect", "TablePiiDetect"}, names.workflows)
	require.Equal(t, []string{
		"GetPiiDetectJobDetails", "GetLastSuccessfulWorkflowId", "GetTablesToPiiScan", "SaveJobPiiDetectReport",
		"GetColumnData", "DetectPiiRegex", "DetectPiiLLM", "SaveTablePiiDetectReport",
	}, names.activities)
	require.Equal(t, "JobPiiDetect", JobWorkflowName)
	require.Equal(t, "TablePiiDetect", TableWorkflowName)
}

// The three change ids are in the histories of the runs that took their branch.
func Test_ChangeIds(t *testing.T) {
	require.Equal(t, "pii-detect-model-failure-tolerated", modelFailureToleratedChangeId)
	require.Equal(t, "pii-detect-incomplete-run-fails", incompleteRunFailsChangeId)
	require.Equal(t, "pii-detect-table-child-id-unique", tableChildIdUniqueChangeId)
}

var (
	contractKey     = &mgmtv1alpha1.RunContextKey{JobRunId: "run-1", ExternalId: "public.users--table-pii-report", AccountId: "account-1"}
	contractKeyJSON = `{"jobRunId":"run-1","externalId":"public.users--table-pii-report","accountId":"account-1"}`
	contractColumn  = `{"Column":"email","DataType":"text","IsNullable":true,"Comment":null}`
)

// The serialized form of the inputs and outputs is in the histories: these are its keys.
// Each type is given twice: without the members that a run may lack, as every run
// recorded so far holds it, and with them.
func Test_SerializedForms(t *testing.T) {
	prompt := "Columns named ref_* hold customer references."
	earlier := "run-0"
	parent := "run-1"
	column := &ColumnData{Column: "email", DataType: "text", IsNullable: true}
	profiled := &ColumnData{Column: "email", DataType: "text", IsNullable: true, Profile: &profile.Profile{Rows: 200, Kind: profile.KindText}}

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"the job workflow's input", &JobPiiDetectRequest{JobId: "job-1"}, `{"JobId":"job-1"}`},
		{"the job workflow's output", &JobPiiDetectResponse{ReportKey: contractKey}, `{"ReportKey":` + contractKeyJSON + `}`},
		{
			"the table workflow's input",
			&TablePiiDetectRequest{
				AccountId: "account-1", JobId: "job-1", ConnectionId: "connection-1",
				TableSchema: "public", TableName: "users", ShouldSampleData: true, UserPrompt: prompt,
				PreviousResultsKey: contractKey, ParentExecutionId: &parent,
			},
			`{"AccountId":"account-1","JobId":"job-1","ConnectionId":"connection-1","TableSchema":"public","TableName":"users",` +
				`"ShouldSampleData":true,"UserPrompt":"Columns named ref_* hold customer references.",` +
				`"PreviousResultsKey":` + contractKeyJSON + `,"ParentExecutionId":"run-1"}`,
		},
		{
			"the table workflow's input, bare, with the input of the model",
			&TablePiiDetectRequest{ModelInput: "values"},
			`{"AccountId":"","JobId":"","ConnectionId":"","TableSchema":"","TableName":"","ShouldSampleData":false,` +
				`"UserPrompt":"","PreviousResultsKey":null,"ParentExecutionId":null,"ModelInput":"values"}`,
		},
		{
			"the table workflow's output",
			&TablePiiDetectResponse{
				PiiColumns: map[string]report.Combined{
					"email": {Regex: &report.RuleFinding{Category: report.Contact}, LLM: &report.ModelFinding{Category: report.Contact, Confidence: 0.95}},
					"phone": {Regex: &report.RuleFinding{Category: report.Contact}},
				},
				ResultKey: contractKey,
			},
			`{"PiiColumns":{"email":{"regex":{"category":"contact"},"llm":{"category":"contact","confidence":0.95}},` +
				`"phone":{"regex":{"category":"contact"},"llm":null}},"ResultKey":` + contractKeyJSON + `}`,
		},
		{
			"the table workflow's output, with the status of the model and an evidence",
			&TablePiiDetectResponse{
				PiiColumns: map[string]report.Combined{"iban": {Regex: &report.RuleFinding{Category: report.Financial, Evidence: "values:iban 1"}}},
				Model:      "failed",
			},
			`{"PiiColumns":{"iban":{"regex":{"category":"financial","evidence":"values:iban 1"},"llm":null}},"ResultKey":null,"Model":"failed"}`,
		},
		{"the job details' input", &GetPiiDetectJobDetailsRequest{JobId: "job-1"}, `{"JobId":"job-1"}`},
		{
			"the job details' output",
			&GetPiiDetectJobDetailsResponse{
				AccountId: "account-1",
				PiiDetectConfig: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{
					DataSampling: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true},
					TableScanFilter: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
						Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{
							Include: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns{Schemas: []string{"public"}},
						},
					},
					UserPrompt:  &prompt,
					Incremental: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_Incremental{IsEnabled: true},
				},
				SourceConnectionId: "connection-1",
			},
			`{"AccountId":"account-1","PiiDetectConfig":{"dataSampling":{"isEnabled":true},"tableScanFilter":{"include":{"schemas":["public"]}},` +
				`"userPrompt":"Columns named ref_* hold customer references.","incremental":{"isEnabled":true}},"SourceConnectionId":"connection-1"}`,
		},
		{
			"the job details' output, with the tables at once and the input of the model",
			&GetPiiDetectJobDetailsResponse{
				AccountId: "account-1", PiiDetectConfig: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
				SourceConnectionId: "connection-1", TablesAtOnce: 3, ModelInput: "values",
			},
			`{"AccountId":"account-1","PiiDetectConfig":{},"SourceConnectionId":"connection-1","TablesAtOnce":3,"ModelInput":"values"}`,
		},
		{
			"the lookup of the earlier run's input",
			&GetLastSuccessfulWorkflowIdRequest{AccountId: "account-1", JobId: "job-1"},
			`{"AccountId":"account-1","JobId":"job-1"}`,
		},
		{"the lookup of the earlier run's output", &GetLastSuccessfulWorkflowIdResponse{WorkflowId: &earlier}, `{"WorkflowId":"run-0"}`},
		{"the lookup of the earlier run's output, without one", &GetLastSuccessfulWorkflowIdResponse{}, `{"WorkflowId":null}`},
		{
			"the table listing's input",
			&GetTablesToPiiScanRequest{
				AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1",
				IncrementalConfig: &IncrementalConfig{LastWorkflowId: "run-0"},
			},
			`{"AccountId":"account-1","JobId":"job-1","SourceConnectionId":"connection-1","Filter":null,"IncrementalConfig":{"LastWorkflowId":"run-0"}}`,
		},
		{
			"the table listing's input, with what enters the fingerprint",
			&GetTablesToPiiScanRequest{
				Filter: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
					Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_IncludeAll{
						IncludeAll: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_IncludeAll{},
					},
				},
				Sampling: true, UserPrompt: "notes", ModelInput: "values",
			},
			`{"AccountId":"","JobId":"","SourceConnectionId":"","Filter":{"includeAll":{}},"IncrementalConfig":null,` +
				`"Sampling":true,"UserPrompt":"notes","ModelInput":"values"}`,
		},
		{
			"the table listing's output",
			&GetTablesToPiiScanResponse{
				Tables: []TableToScan{{Schema: "public", Table: "users", Fingerprint: "abc"}},
				PreviousReports: []*report.TableEntry{
					{TableSchema: "public", TableName: "users", ReportKey: contractKey, ScanFingerprint: "abd"},
				},
			},
			`{"Tables":[{"Schema":"public","Table":"users","Fingerprint":"abc"}],"PreviousReports":[{"tableSchema":"public","tableName":"users",` +
				`"reportKey":` + contractKeyJSON + `,"scanFingerprint":"abd"}]}`,
		},
		{"the table listing's output, empty", &GetTablesToPiiScanResponse{Tables: []TableToScan{}}, `{"Tables":[],"PreviousReports":null}`},
		{
			"the index save's input",
			&SaveJobPiiDetectReportRequest{
				AccountId: "account-1", JobId: "job-1",
				Report: &report.JobReport{SuccessfulTableReports: []*report.TableEntry{
					{TableSchema: "public", TableName: "users", ReportKey: contractKey, ScanFingerprint: "abc"},
				}},
			},
			`{"AccountId":"account-1","JobId":"job-1","Report":{"successfulTableReports":[{"tableSchema":"public","tableName":"users",` +
				`"reportKey":` + contractKeyJSON + `,"scanFingerprint":"abc"}]}}`,
		},
		{"the index save's output", &SaveJobPiiDetectReportResponse{Key: contractKey}, `{"Key":` + contractKeyJSON + `}`},
		{
			"the column read's input",
			&GetColumnDataRequest{ConnectionId: "connection-1", TableSchema: "public", TableName: "users"},
			`{"ConnectionId":"connection-1","TableSchema":"public","TableName":"users"}`,
		},
		{
			"the column read's input, with sampling",
			&GetColumnDataRequest{ConnectionId: "connection-1", TableSchema: "public", TableName: "users", Sample: true},
			`{"ConnectionId":"connection-1","TableSchema":"public","TableName":"users","Sample":true}`,
		},
		{"the column read's output", &GetColumnDataResponse{ColumnData: []*ColumnData{column}}, `{"ColumnData":[` + contractColumn + `]}`},
		{
			"the column read's output, with a profile",
			&GetColumnDataResponse{ColumnData: []*ColumnData{profiled}, SampledRows: 200},
			`{"ColumnData":[{"Column":"email","DataType":"text","IsNullable":true,"Comment":null,"Profile":{"rows":200,"kind":"text"}}],"SampledRows":200}`,
		},
		{"the rules' input", &DetectPiiRegexRequest{ColumnData: []*ColumnData{column}}, `{"ColumnData":[` + contractColumn + `]}`},
		{
			"the rules' output",
			&DetectPiiRegexResponse{PiiColumns: map[string]report.Category{"email": report.Contact}},
			`{"PiiColumns":{"email":"contact"}}`,
		},
		{
			"the rules' output, with evidence",
			&DetectPiiRegexResponse{PiiColumns: map[string]report.Category{"email": report.Contact}, Evidence: map[string]string{"email": "name"}},
			`{"PiiColumns":{"email":"contact"},"Evidence":{"email":"name"}}`,
		},
		{
			"the model's input",
			&DetectPiiLLMRequest{TableSchema: "public", TableName: "users", ColumnData: []*ColumnData{column}, UserPrompt: prompt},
			`{"TableSchema":"public","TableName":"users","ColumnData":[` + contractColumn + `],"ShouldSample":false,"ConnectionId":"",` +
				`"UserPrompt":"Columns named ref_* hold customer references."}`,
		},
		{
			"the model's input, for values",
			&DetectPiiLLMRequest{TableSchema: "public", TableName: "users", ColumnData: []*ColumnData{column}, ConnectionId: "connection-1", Input: "values"},
			`{"TableSchema":"public","TableName":"users","ColumnData":[` + contractColumn + `],"ShouldSample":false,"ConnectionId":"connection-1",` +
				`"UserPrompt":"","Input":"values"}`,
		},
		{
			"the model's output",
			&DetectPiiLLMResponse{PiiColumns: map[string]report.ModelFinding{"email": {Category: report.Contact, Confidence: 0.95}}},
			`{"PiiColumns":{"email":{"category":"contact","confidence":0.95}}}`,
		},
		{
			"the model's output, with how it went",
			&DetectPiiLLMResponse{
				PiiColumns: map[string]report.ModelFinding{}, Input: "profiles", Status: "partial", Model: "local-model",
				Unanswered:     []string{"note"},
				BelowThreshold: []report.Dismissed{{ColumnName: "city", Category: report.Location, Confidence: 0.25}},
			},
			`{"PiiColumns":{},"Input":"profiles","Status":"partial","Model":"local-model","Unanswered":["note"],` +
				`"BelowThreshold":[{"column_name":"city","category":"location","confidence":0.25}]}`,
		},
		{
			"the report save's input",
			&SaveTablePiiDetectReportRequest{
				ParentRunId: &parent, AccountId: "account-1", TableSchema: "public", TableName: "users",
				Report:         map[string]report.Combined{"email": {LLM: &report.ModelFinding{Category: report.Contact, Confidence: 0.5}}},
				ScannedColumns: []string{"id", "email"},
			},
			`{"ParentRunId":"run-1","AccountId":"account-1","TableSchema":"public","TableName":"users",` +
				`"Report":{"email":{"regex":null,"llm":{"category":"contact","confidence":0.5}}},"ScannedColumns":["id","email"]}`,
		},
		{
			"the report save's input, with how the table was scanned",
			&SaveTablePiiDetectReportRequest{
				AccountId: "account-1", TableSchema: "public", TableName: "users",
				Report: map[string]report.Combined{}, ScannedColumns: []string{},
				Scan: &report.Scan{SampledRows: 200, Input: "values", Model: "local-model", ModelStatus: "answered"},
			},
			`{"ParentRunId":null,"AccountId":"account-1","TableSchema":"public","TableName":"users","Report":{},"ScannedColumns":[],` +
				`"Scan":{"sampled_rows":200,"input":"values","model":"local-model","model_status":"answered"}}`,
		},
		{"the report save's output", &SaveTablePiiDetectReportResponse{Key: contractKey}, `{"Key":` + contractKeyJSON + `}`},
	}
	dc := converter.GetDefaultDataConverter()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := dc.ToPayload(tt.value)
			require.NoError(t, err)
			// The messages of the API are written in their proto form, whose spacing is
			// not fixed: the forms are compared as JSON documents.
			require.JSONEq(t, tt.want, string(payload.GetData()))
			require.Equal(t, "json/plain", string(payload.GetMetadata()["encoding"]))

			decoded := reflect.New(reflect.TypeOf(tt.value).Elem()).Interface()
			require.NoError(t, dc.FromPayload(payload, decoded))
			encoded, err := dc.ToPayload(decoded)
			require.NoError(t, err)
			require.JSONEq(t, tt.want, string(encoded.GetData()))
		})
	}
}

// Every input and output recorded in the histories of earlier runs decodes into the types
// of the package, and encodes back to the document that was recorded: no member is lost,
// none appears.
func Test_RecordedPayloads_DecodeAndEncodeBack(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "piidetect_replay", "testdata", "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 30)
	// And those of the runs recorded with the later members.
	later, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, later)
	files = append(files, later...)

	dc := converter.GetDefaultDataConverter()
	roundTrip := func(t *testing.T, payloads *commonpb.Payloads, into any) {
		t.Helper()
		require.Len(t, payloads.GetPayloads(), 1)
		recorded := payloads.GetPayloads()[0]
		require.NoError(t, dc.FromPayload(recorded, into))
		encoded, err := dc.ToPayload(into)
		require.NoError(t, err)
		require.JSONEq(t, string(recorded.GetData()), string(encoded.GetData()))
	}
	requests := map[string]func() (request, response any){
		"GetPiiDetectJobDetails": func() (any, any) {
			return &GetPiiDetectJobDetailsRequest{}, &GetPiiDetectJobDetailsResponse{}
		},
		"GetLastSuccessfulWorkflowId": func() (any, any) {
			return &GetLastSuccessfulWorkflowIdRequest{}, &GetLastSuccessfulWorkflowIdResponse{}
		},
		"GetTablesToPiiScan": func() (any, any) { return &GetTablesToPiiScanRequest{}, &GetTablesToPiiScanResponse{} },
		"SaveJobPiiDetectReport": func() (any, any) {
			return &SaveJobPiiDetectReportRequest{}, &SaveJobPiiDetectReportResponse{}
		},
		"GetColumnData":  func() (any, any) { return &GetColumnDataRequest{}, &GetColumnDataResponse{} },
		"DetectPiiRegex": func() (any, any) { return &DetectPiiRegexRequest{}, &DetectPiiRegexResponse{} },
		"DetectPiiLLM":   func() (any, any) { return &DetectPiiLLMRequest{}, &DetectPiiLLMResponse{} },
		"SaveTablePiiDetectReport": func() (any, any) {
			return &SaveTablePiiDetectReportRequest{}, &SaveTablePiiDetectReportResponse{}
		},
	}

	seen := map[string]int{}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			reader, err := os.Open(file)
			require.NoError(t, err)
			defer reader.Close()
			history, err := client.HistoryFromJSON(reader, client.HistoryJSONOptions{})
			require.NoError(t, err)

			var workflowType string
			results := map[int64]any{}      // by the id of the event that scheduled the activity
			childResults := map[int64]any{} // by the id of the event that started the child
			for _, event := range history.GetEvents() {
				if started := event.GetWorkflowExecutionStartedEventAttributes(); started != nil {
					workflowType = started.GetWorkflowType().GetName()
					switch workflowType {
					case JobWorkflowName:
						roundTrip(t, started.GetInput(), &JobPiiDetectRequest{})
					case TableWorkflowName:
						roundTrip(t, started.GetInput(), &TablePiiDetectRequest{})
					default:
						t.Fatalf("a workflow of another type: %s", workflowType)
					}
					seen[workflowType+" input"]++
				}
				if completed := event.GetWorkflowExecutionCompletedEventAttributes(); completed != nil {
					if workflowType == JobWorkflowName {
						roundTrip(t, completed.GetResult(), &JobPiiDetectResponse{})
					} else {
						roundTrip(t, completed.GetResult(), &TablePiiDetectResponse{})
					}
					seen[workflowType+" output"]++
				}
				if scheduled := event.GetActivityTaskScheduledEventAttributes(); scheduled != nil {
					name := scheduled.GetActivityType().GetName()
					types, known := requests[name]
					require.True(t, known, "an activity of another type: %s", name)
					request, response := types()
					roundTrip(t, scheduled.GetInput(), request)
					results[event.GetEventId()] = response
					seen[name+" input"]++
				}
				if completed := event.GetActivityTaskCompletedEventAttributes(); completed != nil {
					roundTrip(t, completed.GetResult(), results[completed.GetScheduledEventId()])
					seen[reflect.TypeOf(results[completed.GetScheduledEventId()]).Elem().Name()]++
				}
				if initiated := event.GetStartChildWorkflowExecutionInitiatedEventAttributes(); initiated != nil &&
					initiated.GetWorkflowType().GetName() == TableWorkflowName {
					roundTrip(t, initiated.GetInput(), &TablePiiDetectRequest{})
					childResults[event.GetEventId()] = &TablePiiDetectResponse{}
					seen["child input"]++
				}
				if completed := event.GetChildWorkflowExecutionCompletedEventAttributes(); completed != nil &&
					completed.GetWorkflowType().GetName() == TableWorkflowName {
					roundTrip(t, completed.GetResult(), childResults[completed.GetInitiatedEventId()])
					seen["child output"]++
				}
			}
		})
	}

	kinds := []string{
		JobWorkflowName + " input", JobWorkflowName + " output", TableWorkflowName + " input", TableWorkflowName + " output",
		"child input", "child output",
	}
	for name, types := range requests {
		_, response := types()
		kinds = append(kinds, name+" input", reflect.TypeOf(response).Elem().Name())
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		require.Positive(t, seen[kind], kind)
	}
}
