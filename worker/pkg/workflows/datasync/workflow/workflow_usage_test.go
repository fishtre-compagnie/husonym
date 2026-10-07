package datasync_workflow

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/testutil"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	runconfigs "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	accountstatus_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/account-status"
	destinationtriggers_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/destination-triggers"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	jobhooks_by_timing_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/jobhooks-by-timing"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	syncactivityopts_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-activity-opts"
	workflow_shared "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	tablesync_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/workflow"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func usageTestConfig(table string, dependsOn ...string) *benthosbuilder.BenthosConfigResponse {
	deps := []*runconfigs.DependsOn{}
	for _, d := range dependsOn {
		deps = append(deps, &runconfigs.DependsOn{Table: "public." + d, Columns: []string{"id"}})
	}
	return &benthosbuilder.BenthosConfigResponse{
		Name:        "public." + table,
		DependsOn:   deps,
		Columns:     []string{"id"},
		TableSchema: "public",
		TableName:   table,
		Config: &husonym_benthos.BenthosConfig{
			StreamConfig: husonym_benthos.StreamConfig{
				Input: &husonym_benthos.InputConfig{
					Inputs: husonym_benthos.Inputs{
						PooledSqlRaw: &husonym_benthos.InputPooledSqlRaw{OrderByColumns: []string{"id"}},
					},
				},
			},
		},
	}
}

// runUsageWorkflow runs executeWorkflow on the given tables, whose table workflows answer as
// tableSync says, and returns the totals the run kept with the error it ended on.
func runUsageWorkflow(
	t *testing.T,
	configs []*benthosbuilder.BenthosConfigResponse,
	tableSync func(*tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error),
) (*workflow_shared.RunTotals, error) {
	t.Helper()
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	totals := &workflow_shared.RunTotals{}
	run := func(ctx workflow.Context, req *WorkflowRequest) (*WorkflowResponse, error) {
		return executeWorkflow(ctx, req, true, totals)
	}
	env.RegisterWorkflowWithOptions(run, workflow.RegisterOptions{Name: "usage-run"})
	mockRunOfTables(env, configs, func(_ workflow.Context, req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
		return tableSync(req)
	})

	env.ExecuteWorkflow("usage-run", &WorkflowRequest{})
	require.True(t, env.IsWorkflowCompleted())
	return totals, env.GetWorkflowError()
}

// mockRunOfTables mocks what a run of the given tables asks for, their table workflows
// answering as tableSync says.
func mockRunOfTables(
	env *testsuite.TestWorkflowEnvironment,
	configs []*benthosbuilder.BenthosConfigResponse,
	tableSync func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error),
) {
	var genact *genbenthosconfigs_activity.Activity
	env.OnActivity(genact.GenerateBenthosConfigs, mock.Anything, mock.Anything).
		Return(&genbenthosconfigs_activity.GenerateBenthosConfigsResponse{BenthosConfigs: configs}, nil)
	var activityOpts *syncactivityopts_activity.Activity
	env.OnActivity(activityOpts.RetrieveActivityOptions, mock.Anything, mock.Anything).
		Return(&syncactivityopts_activity.RetrieveActivityOptionsResponse{
			SyncActivityOptions: &workflow.ActivityOptions{StartToCloseTimeout: time.Minute},
		}, nil)
	var accStatsActivity *accountstatus_activity.Activity
	env.OnActivity(accStatsActivity.CheckAccountStatus, mock.Anything, mock.Anything).
		Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: true}, nil)
	var privilegesActivity *preflight_activity.Activity
	env.OnActivity(privilegesActivity.CheckRunPrivileges, mock.Anything, mock.Anything).
		Return(&preflight_activity.CheckRunPrivilegesResponse{}, nil).Maybe()
	env.OnActivity(privilegesActivity.RunPreflight, mock.Anything, mock.Anything).
		Return(&preflight_activity.RunPreflightResponse{}, nil).Maybe()
	var triggersActivity *destinationtriggers_activity.Activity
	env.OnActivity(triggersActivity.SuspendTriggers, mock.Anything, mock.Anything).
		Return(&destinationtriggers_activity.SuspendTriggersResponse{}, nil).Maybe()
	env.OnActivity(triggersActivity.RestoreTriggers, mock.Anything, mock.Anything).
		Return(&destinationtriggers_activity.RestoreTriggersResponse{}, nil).Maybe()
	var jobHookTimingActivity *jobhooks_by_timing_activity.Activity
	env.OnActivity(jobHookTimingActivity.RunJobHooksByTiming, mock.Anything, mock.Anything).
		Return(&jobhooks_by_timing_activity.RunJobHooksByTimingResponse{}, nil).Maybe()

	var tableSyncWorkflow tablesync_workflow.Workflow
	env.OnWorkflow(tableSyncWorkflow.TableSync, mock.Anything, mock.Anything).Return(tableSync)
}

func Test_Workflow_SumsTheRowsOfItsTables(t *testing.T) {
	counts := map[string]*tablesync_workflow.TableSyncResponse{
		"users":    {RowsRead: 35, RowsDiscarded: 1, Retries: 2},
		"accounts": {RowsRead: 7},
	}
	totals, err := runUsageWorkflow(t,
		[]*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), usageTestConfig("accounts")},
		func(req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			return counts[req.TableName], nil
		})
	require.NoError(t, err)
	require.Equal(t, workflow_shared.RunTotals{RowsRead: 42, RowsDiscarded: 1, Retries: 2}, *totals)
}

func Test_Workflow_KeepsWhatFinishedTablesCountedWhenAnotherFails(t *testing.T) {
	totals, err := runUsageWorkflow(t,
		[]*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), usageTestConfig("foo", "users")},
		func(req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			if req.TableName == "foo" {
				return nil, errors.New("TestFailure")
			}
			return &tablesync_workflow.TableSyncResponse{RowsRead: 35, RowsDiscarded: 1}, nil
		})
	require.Error(t, err)
	require.Equal(t, workflow_shared.RunTotals{RowsRead: 35, RowsDiscarded: 1}, *totals)
}

// reportedUsage is what a run told the API of itself.
type reportedUsage struct {
	mu      sync.Mutex
	started []*runusage.RunStartedRequest
	ended   []*runusage.RunEndedRequest
}

// expectRunUsage has the environment expect what every run tells the API: its start and its
// end, once each.
func expectRunUsage(env *testsuite.TestWorkflowEnvironment) *reportedUsage {
	reported := &reportedUsage{}
	var usage *runusage.Activities
	env.OnActivity(usage.RecordRunStarted, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *runusage.RunStartedRequest) error {
			reported.mu.Lock()
			defer reported.mu.Unlock()
			reported.started = append(reported.started, req)
			return nil
		}).Once()
	env.OnActivity(usage.RecordRunEnded, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *runusage.RunEndedRequest) error {
			reported.mu.Lock()
			defer reported.mu.Unlock()
			reported.ended = append(reported.ended, req)
			return nil
		}).Once()
	return reported
}

// The id the test environment gives the run.
const usageTestRunId = "default-test-workflow-id"

// reportUsageOfRun runs the whole workflow of a job on the given tables, under a license
// that announces no event, and returns what the run told the API with the error it ended on.
func reportUsageOfRun(
	t *testing.T,
	env *testsuite.TestWorkflowEnvironment,
	configs []*benthosbuilder.BenthosConfigResponse,
	tableSync func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error),
) (*runusage.RunEndedRequest, error) {
	t.Helper()
	reported := expectRunUsage(env)
	mockRunOfTables(env, configs, tableSync)

	env.ExecuteWorkflow(
		New(testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures())).Workflow,
		&WorkflowRequest{JobId: "job-1"},
	)
	require.True(t, env.IsWorkflowCompleted())
	env.AssertExpectations(t)

	reported.mu.Lock()
	defer reported.mu.Unlock()
	require.Len(t, reported.started, 1)
	require.Len(t, reported.ended, 1)
	started, ended := reported.started[0], reported.ended[0]
	require.Equal(t, "job-1", started.JobId)
	require.Equal(t, usageTestRunId, started.RunId)
	require.Equal(t, "job-1", ended.JobId)
	require.Equal(t, usageTestRunId, ended.RunId)
	require.True(t, started.StartedAt.Equal(ended.StartedAt))
	require.False(t, ended.EndedAt.Before(ended.StartedAt))
	return ended, env.GetWorkflowError()
}

func Test_Workflow_ReportsTheRowsOfARunThatCompletes(t *testing.T) {
	counts := map[string]*tablesync_workflow.TableSyncResponse{
		"users":    {RowsRead: 35, RowsDiscarded: 1, Retries: 2},
		"accounts": {RowsRead: 7},
	}
	ended, err := reportUsageOfRun(t,
		(&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment(),
		[]*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), usageTestConfig("accounts")},
		func(_ workflow.Context, req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			return counts[req.TableName], nil
		})
	require.NoError(t, err)
	require.Equal(t, runusage.OutcomeCompleted, ended.Outcome)
	require.Equal(t, int64(42), ended.RowsRead)
	require.Equal(t, int64(1), ended.RowsDiscarded)
	require.Equal(t, int64(2), ended.Retries)
}

// What a failed run read is at least what its finished tables counted.
func Test_Workflow_ReportsARunThatFails(t *testing.T) {
	ended, err := reportUsageOfRun(t,
		(&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment(),
		[]*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), usageTestConfig("foo", "users")},
		func(_ workflow.Context, req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			if req.TableName == "foo" {
				return nil, errors.New("TestFailure")
			}
			return &tablesync_workflow.TableSyncResponse{RowsRead: 35, RowsDiscarded: 1}, nil
		})
	require.ErrorContains(t, err, "TestFailure")
	require.Equal(t, runusage.OutcomeFailed, ended.Outcome)
	require.Equal(t, int64(35), ended.RowsRead)
	require.Equal(t, int64(1), ended.RowsDiscarded)
}

// A run canceled while a table is synced ends canceled, as it did before it told the API
// anything; the API is told so.
func Test_Workflow_ReportsARunThatIsCanceled(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterDelayedCallback(env.CancelWorkflow, time.Minute)
	ended, err := reportUsageOfRun(t, env,
		[]*benthosbuilder.BenthosConfigResponse{usageTestConfig("users")},
		func(ctx workflow.Context, _ *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			return nil, workflow.Sleep(ctx, time.Hour)
		})
	require.True(t, temporal.IsCanceledError(err), "%v", err)
	require.Equal(t, runusage.OutcomeCanceled, ended.Outcome)
}

// A run started before its usage was reported replays as it ran: it tells the API nothing.
func Test_Workflow_EarlierRunsReportNothing(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.OnGetVersion("run-usage-reported", workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
	mockRunOfTables(env, []*benthosbuilder.BenthosConfigResponse{usageTestConfig("users")},
		func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			return &tablesync_workflow.TableSyncResponse{RowsRead: 35}, nil
		})

	env.ExecuteWorkflow(
		New(testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures())).Workflow,
		&WorkflowRequest{JobId: "job-1"},
	)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertNotCalled(t, "RecordRunStarted", mock.Anything, mock.Anything)
	env.AssertNotCalled(t, "RecordRunEnded", mock.Anything, mock.Anything)
}
