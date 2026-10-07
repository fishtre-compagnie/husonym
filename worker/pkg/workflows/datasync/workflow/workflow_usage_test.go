package datasync_workflow

import (
	"errors"
	"testing"
	"time"

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
	tablesync_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/workflow"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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
	env.OnWorkflow(tableSyncWorkflow.TableSync, mock.Anything, mock.Anything).
		Return(func(_ workflow.Context, req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			return tableSync(req)
		})

	env.ExecuteWorkflow("usage-run", &WorkflowRequest{})
	require.True(t, env.IsWorkflowCompleted())
	return totals, env.GetWorkflowError()
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
