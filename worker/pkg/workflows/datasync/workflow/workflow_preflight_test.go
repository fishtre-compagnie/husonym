package datasync_workflow

import (
	"context"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	accountstatus_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/account-status"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	syncactivityopts_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-activity-opts"
	accounthook_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/account_hooks/workflow"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// A blocking finding stops the run at its start: the pre-flight check gets what the plan
// told, and nothing after it runs — no hook, no schema init, no trigger taken away, no
// table synced, none of which the test registers.
func Test_Workflow_PreflightStopsTheRun(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	accountID := uuid.NewString()

	var activityOpts *syncactivityopts_activity.Activity
	env.OnActivity(activityOpts.RetrieveActivityOptions, mock.Anything, mock.Anything).
		Return(&syncactivityopts_activity.RetrieveActivityOptionsResponse{
			SyncActivityOptions: &workflow.ActivityOptions{StartToCloseTimeout: time.Minute},
			AccountId:           accountID,
		}, nil)
	var accStatsActivity *accountstatus_activity.Activity
	env.OnActivity(accStatsActivity.CheckAccountStatus, mock.Anything, mock.Anything).
		Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: true}, nil)

	planned := &preflight.Finding{
		Kind:    mgmtv1alpha1.PreflightFinding_KIND_GENERATED_COLUMN_WRITTEN,
		Level:   preflight.Blocking,
		Table:   "public.ligne",
		Columns: []string{"total"},
		Message: "public.ligne.total is computed by the destination",
	}
	var genact *genbenthosconfigs_activity.Activity
	env.OnActivity(genact.GenerateBenthosConfigs, mock.Anything, mock.Anything).
		Return(&genbenthosconfigs_activity.GenerateBenthosConfigsResponse{
			AccountId: accountID,
			BenthosConfigs: []*benthosbuilder.BenthosConfigResponse{{
				Name:             "public.ligne.insert",
				TableSchema:      "public",
				TableName:        "ligne",
				Columns:          []string{"id", "total"},
				GeneratedColumns: []string{"total"},
				RunType:          runconfigs.RunTypeInsert,
				DependsOn:        []*runconfigs.DependsOn{},
			}},
			Findings: []*preflight.Finding{planned},
		}, nil)

	var preflightActivity *preflight_activity.Activity
	env.OnActivity(preflightActivity.RunPreflight, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *preflight_activity.RunPreflightRequest) (*preflight_activity.RunPreflightResponse, error) {
			assert.Equal(t, accountID, req.AccountId)
			assert.NotEmpty(t, req.JobRunId)
			assert.Equal(t, []*preflight.Finding{planned}, req.Findings)
			// A generated column is not among the columns the run writes.
			assert.Equal(t, []*preflight_activity.TableColumns{{Schema: "public", Table: "ligne", Columns: []string{"id"}}}, req.Tables)
			// What the activity returns on a blocking finding: asked once, never again.
			return nil, temporal.NewNonRetryableApplicationError(
				"pre-flight check stopped the run: public.ligne.total is computed by the destination", "PreflightBlocking", nil)
		}).Once()

	env.OnWorkflow(accounthook_workflow.ProcessAccountHook, mock.Anything, mock.Anything).
		Return(&accounthook_workflow.ProcessAccountHookResponse{}, nil).Twice()

	datasyncWorkflow := New(testutil.NewFakeEELicense(testutil.WithIsValid()))
	env.ExecuteWorkflow(datasyncWorkflow.Workflow, &WorkflowRequest{JobId: uuid.NewString()})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-flight check stopped the run")
	env.AssertExpectations(t)
}
