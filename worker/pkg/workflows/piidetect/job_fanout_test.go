package piidetect

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// starts keeps, by table, the id of the run of the table and when it started, on the clock
// of the job run.
type starts struct {
	mu  sync.Mutex
	ids map[string]string
	at  map[string]time.Time
}

func watchStarts(run *jobRun) *starts {
	s := &starts{ids: map[string]string{}, at: map[string]time.Time{}}
	run.env.SetOnChildWorkflowStartedListener(func(info *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
		if info.WorkflowType.Name != TableWorkflowName {
			return
		}
		var req TablePiiDetectRequest
		if err := args.Get(&req); err != nil {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.ids[req.TableName] = info.WorkflowExecution.ID
		s.at[req.TableName] = run.env.Now()
	})
	return s
}

var sevenTables = []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7"}

// Tables are scanned a fixed number at once, and a table starts as soon as another ends:
// there are no rounds. A scan lasts a minute here.
func Test_JobPiiDetect_TablesAtOnce(t *testing.T) {
	for name, tt := range map[string]struct {
		registered int // what the worker is set to
		recorded   int // what the first activity of the run answered
		want       int
	}{
		"three, from the first activity":                      {registered: 1, recorded: 3, want: 3},
		"one, from the first activity":                        {registered: 3, recorded: 1, want: 1},
		"three, from the worker when the run recorded none":   {registered: 3, recorded: 0, want: 3},
		"one, from the worker when the run recorded none":     {registered: 1, recorded: 0, want: 1},
		"three when neither says how many":                    {registered: 0, recorded: 0, want: 3},
		"three when neither says a number that can be worked": {registered: -1, recorded: -2, want: 3},
	} {
		t.Run(name, func(t *testing.T) {
			run := newJobRun(t, tt.registered)
			run.scanTables(scanned)
			started := watchStarts(run)
			details := plainDetails()
			details.TablesAtOnce = tt.recorded
			run.withDetails(details)
			run.withTables(sevenTables...)
			run.savesReport()

			run.execute()

			require.NoError(t, run.env.GetWorkflowError())
			require.Equal(t, tt.want, run.maxRunning)
			require.Len(t, started.at, len(sevenTables))

			// The tables start in the order they were listed: the first ones together,
			// each of the others a minute after the one whose place it takes.
			first := started.at["t1"]
			for i, table := range sevenTables {
				require.Equal(t, time.Duration(i/tt.want)*time.Minute, started.at[table].Sub(first), table)
			}
		})
	}
}

// How many tables are scanned at once is not read from the settings of the process by
// the workflow: a worker set otherwise replays a run with the number the run recorded.
func Test_JobPiiDetect_ReadsNoSettingOfTheProcess(t *testing.T) {
	t.Setenv("TABLE_PII_DETECT_MAX_CONCURRENCY", "1")
	viper.Set("TABLE_PII_DETECT_MAX_CONCURRENCY", 1)
	t.Cleanup(func() { viper.Set("TABLE_PII_DETECT_MAX_CONCURRENCY", nil) })

	run := newJobRun(t, 3)
	run.scanTables(scanned)
	run.withDetails(plainDetails())
	run.withTables(sevenTables...)
	run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.Equal(t, 3, run.maxRunning)
}

// The files that hold workflow code import nothing that reads the environment or the
// settings of the process, nor the client of the model. They are found, not listed: every
// file of the package that imports the workflow API of Temporal.
func Test_WorkflowCode_ImportsNoSettingsReader(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	var workflowFiles []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, err)
		var imports []string
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err)
			imports = append(imports, path)
		}
		if !slices.Contains(imports, "go.temporal.io/sdk/workflow") {
			continue
		}
		workflowFiles = append(workflowFiles, file)
		for _, path := range imports {
			require.NotEqual(t, "os", path, file)
			require.False(t, strings.Contains(path, "viper"), "%s imports %s", file, path)
			require.False(t, strings.HasSuffix(path, "/model"), "%s imports %s", file, path)
		}
	}
	require.Subset(t, workflowFiles, []string{"job_workflow.go", "job_fanout.go", "table_workflow.go", "options.go"})
}

// The id of the run of a table is the id of the job run, the name of the table and the
// time of the run's clock.
func Test_JobPiiDetect_ChildIds(t *testing.T) {
	run := newJobRun(t, 3)
	run.scanTables(scanned)
	started := watchStarts(run)
	run.withDetails(plainDetails())
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.Anything).
		Return(&GetTablesToPiiScanResponse{Tables: []TableToScan{{Schema: "Sales", Table: "Order.Lines"}}}, nil).Once()
	run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.Len(t, started.ids, 1)
	at := started.at["Order.Lines"]
	require.Equal(t, testRunId+"-sales_order_lines-"+strconv.FormatInt(at.UnixNano(), 10), started.ids["Order.Lines"])
}

// Two tables whose names give the same id, started at the same instant of the run's
// clock, are both scanned: the second gets a suffix. Only then is the version of that
// change read.
func Test_JobPiiDetect_TwoTablesOfTheSameIdAreBothScanned(t *testing.T) {
	run := newJobRun(t, 3)
	run.scanTables(scanned)
	versions := watchVersions(run.env)
	started := watchStarts(run)
	run.withDetails(plainDetails())
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.Anything).
		Return(&GetTablesToPiiScanResponse{Tables: []TableToScan{
			{Schema: "public", Table: "Users", Fingerprint: "f1"},
			{Schema: "public", Table: "users", Fingerprint: "f2"},
			{Schema: "public", Table: "orders", Fingerprint: "f3"},
		}}, nil).Once()
	saved := run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.Len(t, started.ids, 3)
	require.Regexp(t, `-public_users-\d+$`, started.ids["Users"])
	require.Equal(t, started.ids["Users"]+"-2", started.ids["users"])
	require.Regexp(t, `-public_orders-\d+$`, started.ids["orders"])
	require.Len(t, (*saved).SuccessfulTableReports, 3)
	require.Empty(t, (*saved).FailedTables)
	require.Equal(t, []string{"license-read-recorded-1", "license-feature-read-recorded-1", "pii-detect-table-child-id-unique-1"}, versions.all())
}

// Runs started before the ids were made unique replay as they ran: the second table
// cannot start under the id of the first, and is one of the tables that failed.
func Test_JobPiiDetect_TwoTablesOfTheSameIdInARunThatStartedEarlier(t *testing.T) {
	run := newJobRun(t, 3)
	run.scanTables(scanned)
	run.env.OnGetVersion("pii-detect-table-child-id-unique", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	run.env.OnGetVersion("pii-detect-incomplete-run-fails", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	run.withDetails(plainDetails())
	run.env.OnActivity(run.activities.GetTablesToPiiScan, mock.Anything, mock.Anything).
		Return(&GetTablesToPiiScanResponse{Tables: []TableToScan{
			{Schema: "public", Table: "Users", Fingerprint: "f1"},
			{Schema: "public", Table: "users", Fingerprint: "f2"},
		}}, nil).Once()
	saved := run.savesReport()

	run.execute()

	require.NoError(t, run.env.GetWorkflowError())
	require.Len(t, (*saved).SuccessfulTableReports, 1)
	require.Len(t, (*saved).FailedTables, 1)
	require.Equal(t, "users", (*saved).FailedTables[0].TableName)
}

func Test_UniqueChildId(t *testing.T) {
	started := map[string]bool{"run-table-1": true}
	require.Equal(t, "run-table-1-2", uniqueChildId("run-table-1", started))
	started["run-table-1-2"] = true
	require.Equal(t, "run-table-1-3", uniqueChildId("run-table-1", started))

	// An id is at most 1000 characters long: the suffix takes the place of its end.
	long := strings.Repeat("a", 1000)
	unique := uniqueChildId(long, map[string]bool{long: true})
	require.Len(t, unique, 1000)
	require.True(t, strings.HasSuffix(unique, "a-2"))
	require.True(t, strings.HasPrefix(unique, strings.Repeat("a", 998)))
}

// What the run of a table is started with.
func Test_TableChildOptions(t *testing.T) {
	options := tableChildOptions("child-id")
	require.Equal(t, workflow.ChildWorkflowOptions{
		WorkflowID:         "child-id",
		WorkflowRunTimeout: 2 * time.Hour,
		RetryPolicy:        &temporal.RetryPolicy{MaximumAttempts: 1},
		ParentClosePolicy:  enums.PARENT_CLOSE_POLICY_TERMINATE,
	}, options)
}

// How each activity is retried. The test environment does not wait between two attempts
// as a server does: the intervals are read from the policies themselves.
func Test_RetryPolicies(t *testing.T) {
	three := &temporal.RetryPolicy{MaximumAttempts: 3}
	saving := &temporal.RetryPolicy{InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumAttempts: 5}
	require.Equal(t, three, jobDetailsOptions().RetryPolicy)
	require.Equal(t, three, lastRunOptions().RetryPolicy)
	require.Equal(t, three, tablesOptions().RetryPolicy)
	require.Equal(t, saving, saveJobReportOptions().RetryPolicy)
	require.Equal(t, three, columnDataOptions().RetryPolicy)
	require.Equal(t, three, rulesOptions().RetryPolicy)
	require.Equal(t,
		&temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumAttempts: 3},
		modelOptions(false).RetryPolicy,
	)
	require.Equal(t, &temporal.RetryPolicy{MaximumAttempts: 1}, modelOptions(true).RetryPolicy)
	require.Equal(t, saving, saveTableReportOptions().RetryPolicy)

	require.Equal(t, time.Minute, jobDetailsOptions().StartToCloseTimeout)
	require.Equal(t, time.Minute, lastRunOptions().StartToCloseTimeout)
	require.Equal(t, 5*time.Minute, tablesOptions().StartToCloseTimeout)
	require.Equal(t, time.Minute, saveJobReportOptions().StartToCloseTimeout)
	require.Equal(t, 30*time.Minute, modelOptions(true).StartToCloseTimeout)
	require.Equal(t, 3*time.Minute, modelOptions(true).HeartbeatTimeout)
}
