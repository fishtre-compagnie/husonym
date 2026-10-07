package datasync_workflow

import (
	"sync"
	"testing"
	"time"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/license"
	runconfigs "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	accountstatus_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/account-status"
	destinationtriggers_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/destination-triggers"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	jobhooks_by_timing_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/jobhooks-by-timing"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	syncactivityopts_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-activity-opts"
	tablesync_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/workflow"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// oneTableRun is a run of a job of one table, whose activities and children all succeed. It
// keeps the events whose account hooks were started and how many tables were synced.
type oneTableRun struct {
	env *testsuite.TestWorkflowEnvironment

	mu     sync.Mutex
	events []string
	synced int
}

func newOneTableRun() *oneTableRun {
	testSuite := &testsuite.WorkflowTestSuite{}
	run := &oneTableRun{env: testSuite.NewTestWorkflowEnvironment()}
	env := run.env
	expectRunUsage(env)

	var activityOpts *syncactivityopts_activity.Activity
	env.OnActivity(activityOpts.RetrieveActivityOptions, mock.Anything, mock.Anything).
		Return(&syncactivityopts_activity.RetrieveActivityOptionsResponse{
			SyncActivityOptions: &workflow.ActivityOptions{
				StartToCloseTimeout: time.Minute,
			},
			AccountId: uuid.NewString(),
		}, nil)
	var accStatsActivity *accountstatus_activity.Activity
	env.OnActivity(accStatsActivity.CheckAccountStatus, mock.Anything, mock.Anything).
		Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: true}, nil)
	var preflightActivity *preflight_activity.Activity
	env.OnActivity(preflightActivity.RunPreflight, mock.Anything, mock.Anything).
		Return(&preflight_activity.RunPreflightResponse{}, nil)
	var triggersActivity *destinationtriggers_activity.Activity
	env.OnActivity(triggersActivity.SuspendTriggers, mock.Anything, mock.Anything).
		Return(&destinationtriggers_activity.SuspendTriggersResponse{}, nil)
	env.OnActivity(triggersActivity.RestoreTriggers, mock.Anything, mock.Anything).
		Return(&destinationtriggers_activity.RestoreTriggersResponse{}, nil)
	var genact *genbenthosconfigs_activity.Activity
	env.OnActivity(genact.GenerateBenthosConfigs, mock.Anything, mock.Anything).
		Return(&genbenthosconfigs_activity.GenerateBenthosConfigsResponse{BenthosConfigs: []*benthosbuilder.BenthosConfigResponse{
			{
				Name:      "public.users",
				DependsOn: []*runconfigs.DependsOn{},
				Config:    &husonym_benthos.BenthosConfig{},
			},
		}}, nil)
	var jobHookTimingActivity *jobhooks_by_timing_activity.Activity
	env.OnActivity(jobHookTimingActivity.RunJobHooksByTiming, mock.Anything, mock.Anything).
		Return(&jobhooks_by_timing_activity.RunJobHooksByTimingResponse{}, nil)

	// A run that announces nothing starts no child for its account hooks: the expectation is
	// there to count the children, not to ask for one.
	env.OnWorkflow(accounthooks.ProcessAccountHook, mock.Anything, mock.Anything).
		Return(func(
			_ workflow.Context,
			req *accounthooks.ProcessAccountHookRequest,
		) (*accounthooks.ProcessAccountHookResponse, error) {
			run.mu.Lock()
			defer run.mu.Unlock()
			run.events = append(run.events, req.Event.Kind().String())
			return &accounthooks.ProcessAccountHookResponse{}, nil
		}).Maybe()

	syncWorkflow := tablesync_workflow.New(10)
	env.OnWorkflow(syncWorkflow.TableSync, mock.Anything, mock.Anything).
		Return(func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			run.mu.Lock()
			defer run.mu.Unlock()
			run.synced++
			return &tablesync_workflow.TableSyncResponse{}, nil
		})
	return run
}

// The account hooks are a feature of their own. A run under a valid license that lacks it
// syncs its tables and tells no account hook of it; the other features of the license do not
// stand in for it.
func Test_Datasync_AnnouncesItsEventsWhenTheLicenseIncludesAccountHooks(t *testing.T) {
	announced := []string{"ACCOUNT_HOOK_EVENT_JOB_RUN_CREATED", "ACCOUNT_HOOK_EVENT_JOB_RUN_SUCCEEDED"}

	for _, tt := range []struct {
		name     string
		features []license.Feature
		// earlier says the run started before the feature was asked.
		earlier bool
		events  []string
	}{
		{
			name:     "the license lacks the feature",
			features: []license.Feature{license.FeatureJobHooks, license.FeaturePiiDetection},
			events:   nil,
		},
		{
			name:     "the license includes the feature",
			features: []license.Feature{license.FeatureAccountHooks},
			events:   announced,
		},
		{
			// It announced its events under a valid license, whatever the license included:
			// it goes on as it started.
			name:     "the license lacks the feature, the run started before it was asked",
			features: []license.Feature{},
			earlier:  true,
			events:   announced,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			run := newOneTableRun()
			if tt.earlier {
				run.env.OnGetVersion("license-feature-read-recorded", workflow.DefaultVersion, 1).
					Return(workflow.DefaultVersion)
			}
			eelicense := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(tt.features...))

			run.env.ExecuteWorkflow(New(eelicense).Workflow, &WorkflowRequest{})

			require.True(t, run.env.IsWorkflowCompleted())
			require.NoError(t, run.env.GetWorkflowError())
			run.mu.Lock()
			defer run.mu.Unlock()
			require.Equal(t, 1, run.synced, "the table is synced")
			require.Equal(t, tt.events, run.events)
			run.env.AssertExpectations(t)
		})
	}
}
