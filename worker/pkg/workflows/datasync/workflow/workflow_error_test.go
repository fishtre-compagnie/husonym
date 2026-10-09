package datasync_workflow

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	bb_shared "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder/shared"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	accountstatus_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/account-status"
	destinationtriggers_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/destination-triggers"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	jobhooks_by_timing_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/jobhooks-by-timing"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	referentialintegrity_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/referential-integrity"
	syncactivityopts_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-activity-opts"
	syncrediscleanup_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-redis-clean-up"
	schemainit_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/schemainit/workflow"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runusage"
	tablesync_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/tablesync/workflow"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

const (
	categoryInsufficientPrivileges = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES
	categoryConstraintViolated     = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED
	categoryObjectMissing          = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING
	categoryTimeout                = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
	categoryLicense                = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE
	categoryCanceled               = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED
	categoryOther                  = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
	categoryUnspecified            = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED

	stepUnspecified = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED
)

// failingRun is a run of the whole workflow of a job in which one thing fails. What fails is
// mocked first: the first answer registered for an activity or a child is the one given.
type failingRun struct {
	env *testsuite.TestWorkflowEnvironment
	// configs are the tables of the run; one table without a parent when nil.
	configs []*benthosbuilder.BenthosConfigResponse
	// tableSync is how the sync of a table ends; it succeeds when nil.
	tableSync func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error)
	// license is the license of the run; one that announces no event when nil.
	license *testutil.FakeEELicense
}

func newFailingRun() *failingRun {
	return &failingRun{env: (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()}
}

// execute runs the workflow, and returns what the run told the API of its end with the error
// it ended on. Unlike reportUsageOfRun it does not require every mock to be called: a run that
// fails early asks for little.
func (r *failingRun) execute(t *testing.T) (*runusage.RunEndedRequest, error) {
	t.Helper()
	reported := expectRunUsage(r.env)
	configs := r.configs
	if configs == nil {
		configs = []*benthosbuilder.BenthosConfigResponse{usageTestConfig("users")}
	}
	tableSync := r.tableSync
	if tableSync == nil {
		tableSync = func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			return &tablesync_workflow.TableSyncResponse{RowsRead: 1}, nil
		}
	}
	mockRunOfTables(r.env, configs, tableSync)
	var integrityActivity *referentialintegrity_activity.Activity
	r.env.OnActivity(integrityActivity.CheckReferentialIntegrity, mock.Anything, mock.Anything).
		Return(&referentialintegrity_activity.CheckReferentialIntegrityResponse{}, nil).Maybe()
	var redisActivity *syncrediscleanup_activity.Activity
	r.env.OnActivity(redisActivity.DeleteRedisHash, mock.Anything, mock.Anything).
		Return(&syncrediscleanup_activity.DeleteRedisHashResponse{}, nil).Maybe()
	r.env.OnWorkflow(accounthooks.ProcessAccountHook, mock.Anything, mock.Anything).
		Return(&accounthooks.ProcessAccountHookResponse{}, nil).Maybe()

	license := r.license
	if license == nil {
		license = testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures())
	}
	r.env.ExecuteWorkflow(New(license).Workflow, &WorkflowRequest{JobId: "job-1"})
	require.True(t, r.env.IsWorkflowCompleted())

	reported.mu.Lock()
	defer reported.mu.Unlock()
	require.Len(t, reported.started, 1)
	require.Len(t, reported.ended, 1, "the end of the run is told once")
	return reported.ended[0], r.env.GetWorkflowError()
}

// hooksFailAt has the job hooks of one timing fail, and those of the other run.
func hooksFailAt(env *testsuite.TestWorkflowEnvironment, timing mgmtv1alpha1.GetActiveJobHooksByTimingRequest_Timing, err error) {
	var hooksActivity *jobhooks_by_timing_activity.Activity
	env.OnActivity(hooksActivity.RunJobHooksByTiming, mock.Anything,
		mock.MatchedBy(func(req *jobhooks_by_timing_activity.RunJobHooksByTimingRequest) bool {
			return req.Timing == timing
		})).Return(nil, err)
}

// withDestination gives the run a destination, whose schema is then initialized.
func withDestination(env *testsuite.TestWorkflowEnvironment) {
	var activityOpts *syncactivityopts_activity.Activity
	env.OnActivity(activityOpts.RetrieveActivityOptions, mock.Anything, mock.Anything).
		Return(&syncactivityopts_activity.RetrieveActivityOptionsResponse{
			SyncActivityOptions: &workflow.ActivityOptions{StartToCloseTimeout: time.Minute},
			Destinations:        []*mgmtv1alpha1.JobDestination{{Id: "destination-1"}},
		}, nil)
}

// childTable is a table that references users, and is checked once every table is written.
func childTable(name string) *benthosbuilder.BenthosConfigResponse {
	config := usageTestConfig(name, "users")
	config.RunType = runconfigs.RunTypeInsert
	config.ForeignKeys = []*tableplan.ForeignKey{{
		Columns: []string{"user_id"}, ParentSchema: "public", ParentTable: "users", ParentColumns: []string{"id"},
	}}
	return config
}

// Each thing a run may fail on, with the step and the category the API is told, and the
// message the run ends on, which is the one it ended on before it told anything.
func Test_Workflow_TellsTheStepAndTheCategoryItFailedOn(t *testing.T) {
	denied := runerror.Carry(fmt.Errorf("checking users: %w", &pgconn.PgError{Code: "42501", Message: "permission denied"}))
	duplicate := runerror.Carry(fmt.Errorf("writing users: %w", &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}))
	absent := runerror.Carry(fmt.Errorf("creating users: %w", &pgconn.PgError{Code: "3F000", Message: "schema missing"}))

	cases := []struct {
		name     string
		arrange  func(run *failingRun)
		message  string
		category mgmtv1alpha1.RunErrorCategory
		step     mgmtv1alpha1.RunErrorStep
	}{
		{
			name: "the options of the run cannot be read",
			arrange: func(run *failingRun) {
				var activityOpts *syncactivityopts_activity.Activity
				run.env.OnActivity(activityOpts.RetrieveActivityOptions, mock.Anything, mock.Anything).
					Return(nil, errors.New("the API is away"))
			},
			message: "the API is away", category: categoryOther, step: stepOther,
		},
		{
			name: "the account is refused at the start",
			arrange: func(run *failingRun) {
				var statusActivity *accountstatus_activity.Activity
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: false}, nil)
			},
			message: "exiting workflow due to invalid account status", category: categoryLicense, step: stepOther,
		},
		{
			name: "the account is refused at the start, under a license that announces the events",
			arrange: func(run *failingRun) {
				run.license = testutil.NewFakeEELicense(testutil.WithIsValid())
				var statusActivity *accountstatus_activity.Activity
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: false}, nil)
			},
			message: "exiting workflow due to invalid account status", category: categoryLicense, step: stepOther,
		},
		{
			name: "the account status cannot be checked",
			arrange: func(run *failingRun) {
				var statusActivity *accountstatus_activity.Activity
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(nil, errors.New("the API is away"))
			},
			message: "the API is away", category: categoryOther, step: stepOther,
		},
		{
			name: "the privilege check of an earlier run, made before the configs",
			arrange: func(run *failingRun) {
				run.env.OnGetVersion("run-privilege-check", workflow.DefaultVersion, 3).Return(workflow.Version(1))
				var privilegesActivity *preflight_activity.Activity
				run.env.OnActivity(privilegesActivity.CheckRunPrivileges, mock.Anything, mock.Anything).
					Return(nil, errors.New("privilege check failed: missing INSERT"))
			},
			message: "privilege check failed: missing INSERT", category: categoryOther, step: stepPreflight,
		},
		{
			name: "the configs cannot be generated",
			arrange: func(run *failingRun) {
				var genActivity *genbenthosconfigs_activity.Activity
				run.env.OnActivity(genActivity.GenerateBenthosConfigs, mock.Anything, mock.Anything).
					Return(nil, errors.New("unknown transformer"))
			},
			message: "unknown transformer", category: categoryOther, step: stepOther,
		},
		{
			name: "the configs cannot be generated, after the privilege check of an earlier run passed",
			arrange: func(run *failingRun) {
				run.env.OnGetVersion("run-privilege-check", workflow.DefaultVersion, 3).Return(workflow.Version(1))
				var genActivity *genbenthosconfigs_activity.Activity
				run.env.OnActivity(genActivity.GenerateBenthosConfigs, mock.Anything, mock.Anything).
					Return(nil, errors.New("unknown transformer"))
			},
			message: "unknown transformer", category: categoryOther, step: stepOther,
		},
		{
			name: "the privilege check of an earlier run, made on the configs",
			arrange: func(run *failingRun) {
				run.env.OnGetVersion("run-privilege-check", workflow.DefaultVersion, 3).Return(workflow.Version(2))
				var privilegesActivity *preflight_activity.Activity
				run.env.OnActivity(privilegesActivity.CheckRunPrivileges, mock.Anything, mock.Anything).
					Return(nil, denied)
			},
			message: "permission denied", category: categoryInsufficientPrivileges, step: stepPreflight,
		},
		{
			name: "the pre-flight check meets a database that refuses it",
			arrange: func(run *failingRun) {
				var preflightActivity *preflight_activity.Activity
				run.env.OnActivity(preflightActivity.RunPreflight, mock.Anything, mock.Anything).Return(nil, denied)
			},
			message: "permission denied", category: categoryInsufficientPrivileges, step: stepPreflight,
		},
		{
			name: "the pre-flight check stops the run on a table that is not there",
			arrange: func(run *failingRun) {
				var preflightActivity *preflight_activity.Activity
				run.env.OnActivity(preflightActivity.RunPreflight, mock.Anything, mock.Anything).
					Return(nil, runerror.Tell(
						temporal.NewNonRetryableApplicationError("pre-flight check stopped the run: no table", "PreflightBlocking", nil),
						categoryObjectMissing,
					))
			},
			message: "pre-flight check stopped the run: no table", category: categoryObjectMissing, step: stepPreflight,
		},
		{
			name: "a hook of the start fails",
			arrange: func(run *failingRun) {
				hooksFailAt(run.env, mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC, errors.New("the hook failed"))
			},
			message: "the hook failed", category: categoryOther, step: stepHooks,
		},
		{
			name: "the hooks of the start are refused by the license",
			arrange: func(run *failingRun) {
				hooksFailAt(run.env, mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
					runerror.License(errors.New("the worker has not received the license")))
			},
			message: "the worker has not received the license", category: categoryLicense, step: stepHooks,
		},
		{
			name: "the schema of a destination cannot be initialized",
			arrange: func(run *failingRun) {
				withDestination(run.env)
				var schemaInit schemainit_workflow.Workflow
				run.env.OnWorkflow(schemaInit.SchemaInit, mock.Anything, mock.Anything).Return(nil, absent)
			},
			message: "schema missing", category: categoryObjectMissing, step: stepSchemaInit,
		},
		{
			name: "the triggers of the destination cannot be suspended",
			arrange: func(run *failingRun) {
				withDestination(run.env)
				var schemaInit schemainit_workflow.Workflow
				run.env.OnWorkflow(schemaInit.SchemaInit, mock.Anything, mock.Anything).
					Return(&schemainit_workflow.SchemaInitResponse{}, nil)
				var triggersActivity *destinationtriggers_activity.Activity
				run.env.OnActivity(triggersActivity.SuspendTriggers, mock.Anything, mock.Anything).Return(nil, denied)
			},
			message: "permission denied", category: categoryInsufficientPrivileges, step: stepOther,
		},
		{
			name: "a table fails on a constraint",
			arrange: func(run *failingRun) {
				run.tableSync = func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
					return nil, duplicate
				}
			},
			message: "Duplicate entry", category: categoryConstraintViolated, step: stepTableSync,
		},
		{
			name: "a table fails on an error that tells nothing",
			arrange: func(run *failingRun) {
				run.tableSync = func(workflow.Context, *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
					return nil, errors.New("the stream stopped")
				}
			},
			message: "the stream stopped", category: categoryOther, step: stepTableSync,
		},
		{
			name: "a table that depends on another fails",
			arrange: func(run *failingRun) {
				run.configs = []*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), usageTestConfig("orders", "users")}
				run.tableSync = func(_ workflow.Context, req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
					if req.TableName == "orders" {
						return nil, duplicate
					}
					return &tablesync_workflow.TableSyncResponse{}, nil
				}
			},
			message: "Duplicate entry", category: categoryConstraintViolated, step: stepTableSync,
		},
		{
			name: "the account is refused while the tables are synced",
			arrange: func(run *failingRun) {
				var statusActivity *accountstatus_activity.Activity
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: true, ShouldPoll: true}, nil).Once()
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: false}, nil)
				run.tableSync = func(ctx workflow.Context, _ *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
					return nil, workflow.Sleep(ctx, time.Hour)
				}
			},
			message: "exiting workflow due to invalid account status", category: categoryLicense, step: stepTableSync,
		},
		{
			// The run ends on the same error as a refusal, and no account was refused.
			name: "the account status cannot be checked while the tables are synced",
			arrange: func(run *failingRun) {
				var statusActivity *accountstatus_activity.Activity
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: true, ShouldPoll: true}, nil).Once()
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(nil, errors.New("the API is away"))
				run.tableSync = func(ctx workflow.Context, _ *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
					return nil, workflow.Sleep(ctx, time.Hour)
				}
			},
			message: "exiting workflow due to invalid account status", category: categoryOther, step: stepTableSync,
		},
		{
			name: "the check of the account status meets a database that refuses it while the tables are synced",
			arrange: func(run *failingRun) {
				var statusActivity *accountstatus_activity.Activity
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).
					Return(&accountstatus_activity.CheckAccountStatusResponse{IsValid: true, ShouldPoll: true}, nil).Once()
				run.env.OnActivity(statusActivity.CheckAccountStatus, mock.Anything, mock.Anything).Return(nil, denied)
				run.tableSync = func(ctx workflow.Context, _ *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
					return nil, workflow.Sleep(ctx, time.Hour)
				}
			},
			message: "exiting workflow due to invalid account status", category: categoryInsufficientPrivileges, step: stepTableSync,
		},
		{
			name: "the references of the destination do not hold",
			arrange: func(run *failingRun) {
				run.configs = []*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), childTable("orders")}
				var integrityActivity *referentialintegrity_activity.Activity
				run.env.OnActivity(integrityActivity.CheckReferentialIntegrity, mock.Anything, mock.Anything).
					Return(nil, errors.New("orders references users that are missing"))
			},
			message: "orders references users that are missing", category: categoryOther, step: stepIntegrityCheck,
		},
		{
			name: "the triggers of the destination cannot be restored",
			arrange: func(run *failingRun) {
				var triggersActivity *destinationtriggers_activity.Activity
				run.env.OnActivity(triggersActivity.SuspendTriggers, mock.Anything, mock.Anything).
					Return(&destinationtriggers_activity.SuspendTriggersResponse{}, nil)
				run.env.OnActivity(triggersActivity.RestoreTriggers, mock.Anything, mock.Anything).
					Return(nil, errors.New("the trigger was not restored"))
			},
			message: "the trigger was not restored", category: categoryOther, step: stepOther,
		},
		{
			name: "a hook of the end fails",
			arrange: func(run *failingRun) {
				hooksFailAt(run.env, mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC, duplicate)
			},
			message: "Duplicate entry", category: categoryConstraintViolated, step: stepHooks,
		},
		{
			name: "what the run kept in Redis cannot be cleaned",
			arrange: func(run *failingRun) {
				config := usageTestConfig("users")
				config.RedisConfig = []*bb_shared.BenthosRedisConfig{{Key: "hash-1"}}
				run.configs = []*benthosbuilder.BenthosConfigResponse{config}
				var redisActivity *syncrediscleanup_activity.Activity
				run.env.OnActivity(redisActivity.DeleteRedisHash, mock.Anything, mock.Anything).
					Return(nil, errors.New("redis is away"))
			},
			message: "redis is away", category: categoryOther, step: stepOther,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newFailingRun()
			tc.arrange(run)

			ended, err := run.execute(t)

			require.ErrorContains(t, err, tc.message)
			assert.Equal(t, runusage.OutcomeFailed, ended.Outcome)
			assert.Equal(t, tc.category, ended.ErrorCategory, "category")
			assert.Equal(t, tc.step, ended.ErrorStep, "step")
		})
	}
}

// A run canceled while a table is synced is canceled, at the step it was at.
func Test_Workflow_TellsWhereACanceledRunWas(t *testing.T) {
	run := newFailingRun()
	run.env.RegisterDelayedCallback(run.env.CancelWorkflow, time.Minute)
	run.tableSync = func(ctx workflow.Context, _ *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
		return nil, workflow.Sleep(ctx, time.Hour)
	}

	ended, err := run.execute(t)

	require.True(t, temporal.IsCanceledError(err), "%v", err)
	assert.Equal(t, runusage.OutcomeCanceled, ended.Outcome)
	assert.Equal(t, categoryCanceled, ended.ErrorCategory)
	assert.Equal(t, stepTableSync, ended.ErrorStep)
}

// A run that completes tells no error, whatever steps it went through.
func Test_Workflow_ACompletedRunTellsNoError(t *testing.T) {
	run := newFailingRun()
	withDestination(run.env)
	var schemaInit schemainit_workflow.Workflow
	run.env.OnWorkflow(schemaInit.SchemaInit, mock.Anything, mock.Anything).
		Return(&schemainit_workflow.SchemaInitResponse{}, nil)
	run.configs = []*benthosbuilder.BenthosConfigResponse{usageTestConfig("users"), childTable("orders")}

	ended, err := run.execute(t)

	require.NoError(t, err)
	assert.Equal(t, runusage.OutcomeCompleted, ended.Outcome)
	assert.Equal(t, categoryUnspecified, ended.ErrorCategory)
	assert.Equal(t, stepUnspecified, ended.ErrorStep)
}

// Several tables may fail in one run. The run ends on the first failure it is handed, and
// tells the category of that one: the pair told is the one of the error of the run, on every
// replay.
func Test_Workflow_TellsTheErrorTheRunEndsOnWhenSeveralTablesFail(t *testing.T) {
	failures := map[string]struct {
		after time.Duration
		err   error
	}{
		"users": {2 * time.Minute, runerror.Carry(fmt.Errorf("reading users: %w", context.DeadlineExceeded))},
		"orders": {time.Minute, runerror.Carry(fmt.Errorf("writing orders: %w",
			&mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}))},
		"accounts": {3 * time.Minute, runerror.Carry(fmt.Errorf("writing accounts: %w",
			&pgconn.PgError{Code: "42501", Message: "permission denied"}))},
	}
	for range 5 {
		run := newFailingRun()
		run.configs = []*benthosbuilder.BenthosConfigResponse{
			usageTestConfig("users"), usageTestConfig("orders"), usageTestConfig("accounts"),
		}
		run.tableSync = func(ctx workflow.Context, req *tablesync_workflow.TableSyncRequest) (*tablesync_workflow.TableSyncResponse, error) {
			failure := failures[req.TableName]
			// A table that is stopped by the failure of another fails all the same.
			_ = workflow.Sleep(ctx, failure.after)
			return nil, failure.err
		}

		ended, err := run.execute(t)

		require.ErrorContains(t, err, "Duplicate entry")
		require.NotContains(t, err.Error(), "permission denied")
		assert.Equal(t, runusage.OutcomeFailed, ended.Outcome)
		assert.Equal(t, categoryConstraintViolated, ended.ErrorCategory)
		assert.Equal(t, stepTableSync, ended.ErrorStep)
		assert.NotEqual(t, categoryTimeout, ended.ErrorCategory)
	}
}
