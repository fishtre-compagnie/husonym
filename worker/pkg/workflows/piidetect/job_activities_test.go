package piidetect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/connectiondata"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/model"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
)

func piiJob(source *mgmtv1alpha1.JobSourceOptions, config *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect) *mgmtv1alpha1.Job {
	return &mgmtv1alpha1.Job{
		Id:        "job-1",
		AccountId: "account-1",
		Source:    &mgmtv1alpha1.JobSource{Options: source},
		JobType:   &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{PiiDetect: config}},
	}
}

func postgresSource() *mgmtv1alpha1.JobSourceOptions {
	return &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
		Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{ConnectionId: "connection-1"},
	}}
}

// requireNotRetried holds an error to be one that Temporal does not retry, of a type.
func requireNotRetried(t *testing.T, err error, errorType string) *temporal.ApplicationError {
	t.Helper()
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, errorType, appErr.Type())
	require.True(t, appErr.NonRetryable())
	return appErr
}

func Test_GetPiiDetectJobDetails_SourceKinds(t *testing.T) {
	fkSource := "connection-1"
	for name, tt := range map[string]struct {
		source  *mgmtv1alpha1.JobSourceOptions
		want    string // the connection id
		refused string // what the error says of the source
	}{
		"PostgreSQL": {source: postgresSource(), want: "connection-1"},
		"MySQL": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
				Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{ConnectionId: "connection-2"},
			}},
			want: "connection-2",
		},
		"SQL Server": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mssql{
				Mssql: &mgmtv1alpha1.MssqlSourceConnectionOptions{ConnectionId: "connection-3"},
			}},
			want: "connection-3",
		},
		"a generation that takes its foreign keys from a connection": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Generate{
				Generate: &mgmtv1alpha1.GenerateSourceOptions{FkSourceConnectionId: &fkSource},
			}},
			want: "connection-1",
		},
		"a generation without connection": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Generate{
				Generate: &mgmtv1alpha1.GenerateSourceOptions{},
			}},
			refused: "a data generation without a source connection",
		},
		"MongoDB": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mongodb{
				Mongodb: &mgmtv1alpha1.MongoDBSourceConnectionOptions{ConnectionId: "connection-4"},
			}},
			refused: "a MongoDB database",
		},
		"DynamoDB": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Dynamodb{
				Dynamodb: &mgmtv1alpha1.DynamoDBSourceConnectionOptions{ConnectionId: "connection-5"},
			}},
			refused: "a DynamoDB database",
		},
		"a generation by a model": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_AiGenerate{
				AiGenerate: &mgmtv1alpha1.AiGenerateSourceOptions{AiConnectionId: "connection-6", FkSourceConnectionId: &fkSource},
			}},
			refused: "a generation by a language model",
		},
		"an object storage": {
			source: &mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_AwsS3{
				AwsS3: &mgmtv1alpha1.AwsS3SourceConnectionOptions{ConnectionId: "connection-7"},
			}},
			refused: "an object storage",
		},
		"no source": {source: nil, refused: "of a kind that has no table to read"},
	} {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobs{job: piiJob(tt.source, &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{})}
			run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{TablesAtOnce: 5}))

			details, _, err := execute[GetPiiDetectJobDetailsResponse](
				t, run, "GetPiiDetectJobDetails", &GetPiiDetectJobDetailsRequest{JobId: "job-1"},
			)
			if tt.refused != "" {
				appErr := requireNotRetried(t, err, "UnsupportedSource")
				require.Contains(t, appErr.Message(), tt.refused)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "account-1", details.AccountId)
			require.Equal(t, tt.want, details.SourceConnectionId)
			require.Equal(t, 5, details.TablesAtOnce)
			require.Empty(t, details.ModelInput)
		})
	}
}

// How many tables a run scans at once is what the worker that ran the activity is set to,
// three when it is set to nothing that can be worked with.
func Test_GetPiiDetectJobDetails_TablesAtOnce(t *testing.T) {
	for configured, want := range map[int]int{0: 3, -1: 3, 1: 1, 8: 8} {
		jobs := &fakeJobs{job: piiJob(postgresSource(), &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{})}
		run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{TablesAtOnce: configured}))
		details, _, err := execute[GetPiiDetectJobDetailsResponse](
			t, run, "GetPiiDetectJobDetails", &GetPiiDetectJobDetailsRequest{JobId: "job-1"},
		)
		require.NoError(t, err)
		require.Equal(t, want, details.TablesAtOnce, configured)
	}
}

// What the model receives is returned beside the config, and the config is returned
// without it: a workflow that does not know that member of the config could not decode
// a config that holds it.
func Test_GetPiiDetectJobDetails_ModelInput(t *testing.T) {
	values := mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_VALUES
	profiles := mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling_MODEL_INPUT_PROFILES
	prompt := "notes"
	for name, tt := range map[string]struct {
		sampling *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling
		want     string
	}{
		"values, with sampling": {
			&mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true, ModelInput: values}, "values",
		},
		"values, without sampling: no row is read": {
			&mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{ModelInput: values}, "",
		},
		"profiles": {
			&mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true, ModelInput: profiles}, "",
		},
		"not said":          {&mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_DataSampling{IsEnabled: true}, ""},
		"no sampling block": {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			config := &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{DataSampling: tt.sampling, UserPrompt: &prompt}
			jobs := &fakeJobs{job: piiJob(postgresSource(), config)}
			run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{}))

			details, payload, err := execute[GetPiiDetectJobDetailsResponse](
				t, run, "GetPiiDetectJobDetails", &GetPiiDetectJobDetailsRequest{JobId: "job-1"},
			)
			require.NoError(t, err)
			require.Equal(t, tt.want, details.ModelInput)
			require.Equal(t, tt.sampling.GetIsEnabled(), details.PiiDetectConfig.GetDataSampling().GetIsEnabled())
			require.Equal(t, "notes", details.PiiDetectConfig.GetUserPrompt())
			require.NotContains(t, payload, "modelInput")
			require.NotContains(t, payload, "MODEL_INPUT")
			require.Equal(t, tt.sampling.GetModelInput(), config.GetDataSampling().GetModelInput(), "the job itself is left as it is")
		})
	}
}

func Test_GetPiiDetectJobDetails_RefusesAJobOfAnotherType(t *testing.T) {
	job := piiJob(postgresSource(), nil)
	job.JobType = &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_Sync{}}
	run := newActivityRun(t, NewActivities(&fakeJobs{job: job}, nil, nil, nil, nil, &Config{}))

	_, _, err := execute[GetPiiDetectJobDetailsResponse](t, run, "GetPiiDetectJobDetails", &GetPiiDetectJobDetailsRequest{JobId: "job-1"})
	appErr := requireNotRetried(t, err, "NotPiiDetectJob")
	require.Equal(t, "unsupported job type, must be PiiDetect", appErr.Message())
}

// A job that cannot be read may be read at the next attempt.
func Test_GetPiiDetectJobDetails_AJobThatCannotBeReadIsRetried(t *testing.T) {
	jobs := &fakeJobs{getJobErr: connect.NewError(connect.CodeUnavailable, errors.New("the API is away"))}
	run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{}))

	_, _, err := execute[GetPiiDetectJobDetailsResponse](t, run, "GetPiiDetectJobDetails", &GetPiiDetectJobDetailsRequest{JobId: "job-1"})
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.False(t, appErr.NonRetryable())
	require.ErrorContains(t, err, "the API is away")
}

// schedule returns a schedule client whose schedule of the job has these recent runs.
func schedule(t *testing.T, describeErr error, actions ...client.ScheduleActionResult) client.ScheduleClient {
	t.Helper()
	handle := mocks.NewScheduleHandle(t)
	if describeErr != nil {
		handle.On("Describe", mock.Anything).Return(nil, describeErr)
	} else {
		handle.On("Describe", mock.Anything).Return(&client.ScheduleDescription{
			Info: client.ScheduleInfo{RecentActions: actions},
		}, nil)
	}
	schedules := mocks.NewScheduleClient(t)
	schedules.On("GetHandle", mock.Anything, "job-1").Return(handle)
	return schedules
}

func ranAt(workflowId string, minute int) client.ScheduleActionResult {
	return client.ScheduleActionResult{
		ActualTime:          time.Date(2026, time.October, 4, 8, minute, 0, 0, time.UTC),
		StartWorkflowResult: &client.ScheduleWorkflowExecution{WorkflowID: workflowId},
	}
}

func indexOf(tables ...string) []byte {
	index := &report.JobReport{SuccessfulTableReports: []*report.TableEntry{}}
	for _, table := range tables {
		index.SuccessfulTableReports = append(index.SuccessfulTableReports, &report.TableEntry{
			TableSchema: "public", TableName: table, ScanFingerprint: "fingerprint-" + table,
			ReportKey: tableReportKey("run-0", "public", table),
		})
	}
	encoded, _ := json.Marshal(index)
	return encoded
}

func indexKey(runId string) string {
	return "account-1|" + runId + "|job-1--job-pii-report"
}

func Test_GetLastSuccessfulWorkflowId(t *testing.T) {
	for name, tt := range map[string]struct {
		describeErr error
		actions     []client.ScheduleActionResult
		contexts    map[string][]byte
		getErr      error
		want        string
		warns       bool
	}{
		"the latest run that stored an index wins, whatever the order of the list": {
			actions:  []client.ScheduleActionResult{ranAt("run-2", 2), ranAt("run-3", 3), ranAt("run-1", 1)},
			contexts: map[string][]byte{indexKey("run-1"): indexOf("a"), indexKey("run-2"): indexOf("a"), indexKey("run-3"): indexOf("a")},
			want:     "run-3",
		},
		"a run that stored no index is passed over: it is the run in progress, or it was canceled": {
			actions:  []client.ScheduleActionResult{ranAt("run-1", 1), ranAt("run-2", 2)},
			contexts: map[string][]byte{indexKey("run-1"): indexOf("a")},
			want:     "run-1",
		},
		"an index that names no table is passed over": {
			actions:  []client.ScheduleActionResult{ranAt("run-1", 1), ranAt("run-2", 2)},
			contexts: map[string][]byte{indexKey("run-1"): indexOf("a"), indexKey("run-2"): indexOf()},
			want:     "run-1",
		},
		"an action that started no workflow is passed over": {
			actions: []client.ScheduleActionResult{
				ranAt("run-1", 1),
				{ActualTime: time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)},
			},
			contexts: map[string][]byte{indexKey("run-1"): indexOf("a")},
			want:     "run-1",
		},
		"no run":                           {},
		"no run stored an index":           {actions: []client.ScheduleActionResult{ranAt("run-1", 1)}},
		"the schedule cannot be described": {describeErr: errors.New("the server is away"), warns: true},
		"a run context cannot be read":     {actions: []client.ScheduleActionResult{ranAt("run-1", 1)}, getErr: errors.New("the API is away"), warns: true},
		"an index that cannot be decoded":  {actions: []client.ScheduleActionResult{ranAt("run-1", 1)}, contexts: map[string][]byte{indexKey("run-1"): []byte("not JSON")}, warns: true},
		"an index of another account is not read": {
			actions:  []client.ScheduleActionResult{ranAt("run-1", 1)},
			contexts: map[string][]byte{"account-2|run-1|job-1--job-pii-report": indexOf("a")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobs{contexts: tt.contexts, getErr: tt.getErr}
			schedules := schedule(t, tt.describeErr, tt.actions...)
			run := newActivityRun(t, NewActivities(jobs, nil, nil, schedules, nil, &Config{}))

			response, _, err := execute[GetLastSuccessfulWorkflowIdResponse](
				t, run, "GetLastSuccessfulWorkflowId", &GetLastSuccessfulWorkflowIdRequest{AccountId: "account-1", JobId: "job-1"},
			)
			require.NoError(t, err, "what cannot be looked up is not a failure: every table is scanned")
			if tt.want == "" {
				require.Nil(t, response.WorkflowId)
			} else {
				require.NotNil(t, response.WorkflowId)
				require.Equal(t, tt.want, *response.WorkflowId)
			}
			if tt.warns {
				require.Contains(t, run.logs.all(), "WARN the earlier run of the job could not be looked up: every table will be scanned")
			} else {
				require.NotContains(t, run.logs.all(), "WARN")
			}
		})
	}
}

// The catalogue of the source of the listing tests: three tables in two schemas, and a
// row that names no table.
func catalogue() []*mgmtv1alpha1.DatabaseColumn {
	return []*mgmtv1alpha1.DatabaseColumn{
		column("sales", "items", "sku", "text"),
		column("public", "users", "id", "uuid"),
		column("public", "users", "email", "text"),
		column("public", "orders", "id", "uuid"),
		{Schema: "empty_schema"},
		{},
	}
}

func listing(t *testing.T, jobs *fakeJobs, classifier *model.Classifier) *activityRun {
	t.Helper()
	builder, data := source(t)
	data.EXPECT().GetSchema(mock.Anything, mock.Anything).Return(catalogue(), nil).Maybe()
	return newActivityRun(t, NewActivities(jobs, connections, builder, nil, classifier, &Config{}))
}

func listed(response *GetTablesToPiiScanResponse) []string {
	names := make([]string, 0, len(response.Tables))
	for _, table := range response.Tables {
		names = append(names, table.Schema+"."+table.Table)
	}
	return names
}

func Test_GetTablesToPiiScan_Filter(t *testing.T) {
	type filter = mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter
	patterns := func(schemas []string, tables ...string) *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns {
		p := &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns{Schemas: schemas}
		for i := 0; i+1 < len(tables); i += 2 {
			p.Tables = append(p.Tables, &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableIdentifier{Schema: tables[i], Table: tables[i+1]})
		}
		return p
	}
	all := []string{"public.orders", "public.users", "sales.items"}
	for name, tt := range map[string]struct {
		filter *filter
		want   []string
	}{
		"no filter": {nil, all},
		"every table": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_IncludeAll{}}, all,
		},
		"the tables of a schema": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{Include: patterns([]string{"public"})}},
			[]string{"public.orders", "public.users"},
		},
		"one table": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{Include: patterns(nil, "public", "users")}},
			[]string{"public.users"},
		},
		"a schema and a table of another": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{Include: patterns([]string{"sales"}, "public", "users")}},
			[]string{"public.users", "sales.items"},
		},
		"names are compared as they are written": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{Include: patterns([]string{"Public"}, "public", "Users")}},
			[]string{},
		},
		"all but a schema": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Exclude{Exclude: patterns([]string{"public"})}},
			[]string{"sales.items"},
		},
		"all but a table": {
			&filter{Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Exclude{Exclude: patterns(nil, "public", "users")}},
			[]string{"public.orders", "sales.items"},
		},
		"a filter that sets no mode keeps nothing": {&filter{}, []string{}},
	} {
		t.Run(name, func(t *testing.T) {
			run := listing(t, &fakeJobs{}, nil)
			response, payload, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", &GetTablesToPiiScanRequest{
				AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1", Filter: tt.filter,
			})
			require.NoError(t, err)
			// In the order of their names, and never an entry without a table name.
			require.Equal(t, tt.want, listed(response))
			require.Nil(t, response.PreviousReports)
			require.Contains(t, payload, `"Tables":[`)
		})
	}
}

// fingerprintOf computes a fingerprint the way it is specified: every part followed by a
// zero byte.
func fingerprintOf(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// The fingerprint of a table says how it is scanned: a table is scanned again when any of
// its parts changes.
func Test_GetTablesToPiiScan_Fingerprint(t *testing.T) {
	classifier, err := model.NewClassifier(&model.Config{Model: "local-model", BaseURL: "http://localhost:1/v1"})
	require.NoError(t, err)

	fingerprint := func(req *GetTablesToPiiScanRequest, classifier *model.Classifier) string {
		req.AccountId, req.JobId, req.SourceConnectionId = "account-1", "job-1", "connection-1"
		run := listing(t, &fakeJobs{}, classifier)
		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", req)
		require.NoError(t, err)
		for _, table := range response.Tables {
			if table.Schema == "public" && table.Table == "users" {
				return table.Fingerprint
			}
		}
		t.Fatal("public.users is not listed")
		return ""
	}

	// The columns in the order of their names, each with its type.
	plain := fingerprint(&GetTablesToPiiScanRequest{}, nil)
	require.Equal(t, fingerprintOf("v2", "public", "users", "email", "text", "id", "uuid", "false", "", "", "", "2", "false"), plain)

	full := fingerprint(
		&GetTablesToPiiScanRequest{Sampling: true, ModelInput: "values", UserPrompt: "notes", MarksIncomplete: true}, classifier,
	)
	require.Equal(t,
		fingerprintOf("v2", "public", "users", "email", "text", "id", "uuid", "true", "values", "notes", "local-model", "2", "true"),
		full,
	)

	seen := map[string]string{plain: "nothing set", full: "everything set"}
	for name, got := range map[string]string{
		"sampling":   fingerprint(&GetTablesToPiiScanRequest{Sampling: true}, nil),
		"the input":  fingerprint(&GetTablesToPiiScanRequest{ModelInput: "values"}, nil),
		"the prompt": fingerprint(&GetTablesToPiiScanRequest{UserPrompt: "notes"}, nil),
		"the model":  fingerprint(&GetTablesToPiiScanRequest{}, classifier),
		// A caller that records the tables scanned without the model never takes for
		// scanned a table that a caller which does not record them left in an index.
		"a caller that marks incomplete tables": fingerprint(&GetTablesToPiiScanRequest{MarksIncomplete: true}, nil),
	} {
		require.NotContains(t, seen, got, "%s gives the fingerprint of %s", name, seen[got])
		seen[got] = name
	}
}

func Test_GetTablesToPiiScan_FingerprintFollowsTheColumns(t *testing.T) {
	fingerprint := func(columns ...*mgmtv1alpha1.DatabaseColumn) string {
		builder, data := source(t)
		data.EXPECT().GetSchema(mock.Anything, mock.Anything).Return(columns, nil)
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", &GetTablesToPiiScanRequest{
			AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1",
		})
		require.NoError(t, err)
		require.Len(t, response.Tables, 1)
		return response.Tables[0].Fingerprint
	}
	id, email := column("public", "users", "id", "uuid"), column("public", "users", "email", "text")

	base := fingerprint(id, email)
	require.Equal(t, base, fingerprint(email, id), "the order of the columns does not matter")
	require.NotEqual(t, base, fingerprint(id), "a column less")
	require.NotEqual(t, base, fingerprint(id, email, column("public", "users", "phone", "text")), "a column more")
	require.NotEqual(t, base, fingerprint(id, column("public", "users", "email", "varchar")), "another type")
	require.NotEqual(t, base, fingerprint(id, column("public", "users", "mail", "text")), "another name")
	require.NotEqual(t, base, fingerprint(column("public", "user", "id", "uuid"), column("public", "user", "email", "text")), "another table")
	require.NotEqual(t,
		fingerprint(column("public", "t", "ab", "c")), fingerprint(column("public", "t", "a", "bc")),
		"the parts do not run into each other",
	)
}

// An incremental run leaves out the tables the earlier run scanned whole the same way,
// and returns the entries of the earlier run for the tables that are still scanned.
func Test_GetTablesToPiiScan_Incremental(t *testing.T) {
	// The fingerprints of today, to build the index of the earlier run from.
	current := map[string]string{}
	{
		run := listing(t, &fakeJobs{}, nil)
		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", &GetTablesToPiiScanRequest{
			AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1",
		})
		require.NoError(t, err)
		for _, table := range response.Tables {
			current[table.Schema+"."+table.Table] = table.Fingerprint
		}
	}
	earlierEntry := func(schema, table, fingerprint string, incomplete bool) *report.TableEntry {
		return &report.TableEntry{
			TableSchema: schema, TableName: table, ScanFingerprint: fingerprint, Incomplete: incomplete,
			ReportKey: tableReportKey("run-0", schema, table),
		}
	}
	request := func(filter *mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter) *GetTablesToPiiScanRequest {
		return &GetTablesToPiiScanRequest{
			AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1", Filter: filter,
			IncrementalConfig: &IncrementalConfig{LastWorkflowId: "run-0"},
		}
	}
	previous := func(response *GetTablesToPiiScanResponse) []string {
		names := []string{}
		for _, entry := range response.PreviousReports {
			names = append(names, entry.TableSchema+"."+entry.TableName+"@"+entry.ReportKey.GetJobRunId())
		}
		return names
	}

	t.Run("unchanged, changed, incomplete, dropped and new tables", func(t *testing.T) {
		index, err := json.Marshal(&report.JobReport{SuccessfulTableReports: []*report.TableEntry{
			earlierEntry("public", "users", current["public.users"], false),             // unchanged
			earlierEntry("public", "orders", "the fingerprint of other columns", false), // changed
			earlierEntry("public", "dropped", "any", false),                             // no longer in the source
			// sales.items is new: the earlier run did not know it.
		}})
		require.NoError(t, err)
		run := listing(t, &fakeJobs{contexts: map[string][]byte{indexKey("run-0"): index}}, nil)

		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(nil))
		require.NoError(t, err)
		require.Equal(t, []string{"public.orders", "sales.items"}, listed(response))
		require.Equal(t, []string{"public.orders@run-0", "public.users@run-0"}, previous(response))
	})

	t.Run("a table the earlier run scanned without the model is scanned again", func(t *testing.T) {
		index, err := json.Marshal(&report.JobReport{SuccessfulTableReports: []*report.TableEntry{
			earlierEntry("public", "users", current["public.users"], true),
			earlierEntry("public", "orders", current["public.orders"], false),
			earlierEntry("sales", "items", current["sales.items"], false),
		}})
		require.NoError(t, err)
		run := listing(t, &fakeJobs{contexts: map[string][]byte{indexKey("run-0"): index}}, nil)

		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(nil))
		require.NoError(t, err)
		require.Equal(t, []string{"public.users"}, listed(response))
		require.Len(t, response.PreviousReports, 3)
	})

	t.Run("a table the job no longer scans leaves the report", func(t *testing.T) {
		index, err := json.Marshal(&report.JobReport{SuccessfulTableReports: []*report.TableEntry{
			earlierEntry("public", "users", current["public.users"], false),
			earlierEntry("sales", "items", current["sales.items"], false),
		}})
		require.NoError(t, err)
		run := listing(t, &fakeJobs{contexts: map[string][]byte{indexKey("run-0"): index}}, nil)
		onlyPublic := &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter{
			Mode: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TableScanFilter_Include{
				Include: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect_TablePatterns{Schemas: []string{"public"}},
			},
		}

		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(onlyPublic))
		require.NoError(t, err)
		require.Equal(t, []string{"public.orders"}, listed(response))
		require.Equal(t, []string{"public.users@run-0"}, previous(response))
	})

	t.Run("an index stored without the later members is read", func(t *testing.T) {
		index := `{"successfulTableReports":[{"tableSchema":"public","tableName":"users",` +
			`"reportKey":{"jobRunId":"run-0","externalId":"public.users--table-pii-report","accountId":"account-1"},` +
			`"scanFingerprint":"a fingerprint computed another way"}]}`
		run := listing(t, &fakeJobs{contexts: map[string][]byte{indexKey("run-0"): []byte(index)}}, nil)

		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(nil))
		require.NoError(t, err)
		require.Len(t, response.Tables, 3, "a fingerprint computed another way matches none: every table is scanned")
		require.Equal(t, []string{"public.users@run-0"}, previous(response))
	})

	t.Run("no index under the earlier run: every table is scanned", func(t *testing.T) {
		run := listing(t, &fakeJobs{}, nil)
		response, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(nil))
		require.NoError(t, err)
		require.Len(t, response.Tables, 3)
		require.Empty(t, response.PreviousReports)
	})

	t.Run("an index that cannot be read fails the activity", func(t *testing.T) {
		run := listing(t, &fakeJobs{getErr: connect.NewError(connect.CodeUnavailable, errors.New("the API is away"))}, nil)
		_, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(nil))
		require.ErrorContains(t, err, "the index of the earlier run cannot be read")
	})

	t.Run("an index that cannot be decoded fails the activity", func(t *testing.T) {
		run := listing(t, &fakeJobs{contexts: map[string][]byte{indexKey("run-0"): []byte("not JSON")}}, nil)
		_, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", request(nil))
		require.ErrorContains(t, err, "the index of the earlier run cannot be decoded")
	})
}

func Test_GetTablesToPiiScan_RefusesASourceWithoutTables(t *testing.T) {
	t.Run("by the kind of its connection", func(t *testing.T) {
		builder := connectiondata.NewMockConnectionDataBuilder(t) // never asked
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		_, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", &GetTablesToPiiScanRequest{
			AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-mongo",
		})
		appErr := requireNotRetried(t, err, "UnsupportedSource")
		require.Contains(t, appErr.Message(), "a MongoDB database")
	})

	t.Run("by what its reader answers", func(t *testing.T) {
		builder, data := source(t)
		data.EXPECT().GetSchema(mock.Anything, mock.Anything).Return(nil, errors.ErrUnsupported)
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		_, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", &GetTablesToPiiScanRequest{
			AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1",
		})
		requireNotRetried(t, err, "UnsupportedSource")
	})

	t.Run("a catalogue that cannot be read may be read at the next attempt", func(t *testing.T) {
		builder, data := source(t)
		data.EXPECT().GetSchema(mock.Anything, mock.Anything).Return(nil, errors.New("connection refused"))
		run := newActivityRun(t, NewActivities(&fakeJobs{}, connections, builder, nil, nil, &Config{}))
		_, _, err := execute[GetTablesToPiiScanResponse](t, run, "GetTablesToPiiScan", &GetTablesToPiiScanRequest{
			AccountId: "account-1", JobId: "job-1", SourceConnectionId: "connection-1",
		})
		var appErr *temporal.ApplicationError
		require.ErrorAs(t, err, &appErr)
		require.False(t, appErr.NonRetryable())
	})
}

func Test_SaveJobPiiDetectReport(t *testing.T) {
	jobs := &fakeJobs{}
	run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{}))

	response, _, err := execute[SaveJobPiiDetectReportResponse](t, run, "SaveJobPiiDetectReport", &SaveJobPiiDetectReportRequest{
		AccountId: "account-1", JobId: "job-1",
		Report: &report.JobReport{
			SuccessfulTableReports: []*report.TableEntry{
				{TableSchema: "public", TableName: "users", ReportKey: tableReportKey("run-1", "public", "users"), ScanFingerprint: "abc", Incomplete: true},
			},
			FailedTables: []*report.FailedTable{{TableSchema: "public", TableName: "orders", Reason: "the columns cannot be read"}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "account-1", response.Key.GetAccountId())
	require.Equal(t, testRunId, response.Key.GetJobRunId(), "the index is stored under the id of the run")
	require.Equal(t, "job-1--job-pii-report", response.Key.GetExternalId())
	require.JSONEq(t, `{
		"successfulTableReports": [{
			"tableSchema": "public", "tableName": "users",
			"reportKey": {"jobRunId": "run-1", "externalId": "public.users--table-pii-report", "accountId": "account-1"},
			"scanFingerprint": "abc", "incomplete": true
		}],
		"failedTables": [{"tableSchema": "public", "tableName": "orders", "reason": "the columns cannot be read"}]
	}`, string(jobs.contexts[contextKey(response.Key)]))

	// An index without table holds an empty list.
	for _, empty := range []*report.JobReport{nil, {}, {SuccessfulTableReports: []*report.TableEntry{}}} {
		response, _, err = execute[SaveJobPiiDetectReportResponse](t, run, "SaveJobPiiDetectReport", &SaveJobPiiDetectReportRequest{
			AccountId: "account-1", JobId: "job-1", Report: empty,
		})
		require.NoError(t, err)
		require.JSONEq(t, `{"successfulTableReports":[]}`, string(jobs.contexts[contextKey(response.Key)]))
	}
}

func Test_SaveJobPiiDetectReport_FailsWhenTheIndexCannotBeStored(t *testing.T) {
	jobs := &fakeJobs{setErr: connect.NewError(connect.CodeUnavailable, errors.New("connection refused"))}
	run := newActivityRun(t, NewActivities(jobs, nil, nil, nil, nil, &Config{}))
	_, _, err := execute[SaveJobPiiDetectReportResponse](t, run, "SaveJobPiiDetectReport", &SaveJobPiiDetectReportRequest{
		AccountId: "account-1", JobId: "job-1", Report: &report.JobReport{},
	})
	require.ErrorContains(t, err, "unable to set run context")
}
