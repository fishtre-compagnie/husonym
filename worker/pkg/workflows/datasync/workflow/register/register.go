package datasync_workflow_register

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	sql_manager "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/fishtre-compagnie/husonym/worker/pkg/consistencykey"
	accountstatus_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/account-status"
	destinationtriggers_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/destination-triggers"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	jobhooks_by_timing_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/jobhooks-by-timing"
	posttablesync_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/post-table-sync"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	referentialintegrity_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/referential-integrity"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	syncactivityopts_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-activity-opts"
	syncrediscleanup_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/sync-redis-clean-up"
	datasync_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/workflow"
	preflight_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/preflight/workflow"
	"github.com/redis/go-redis/v9"
)

type Worker interface {
	RegisterWorkflow(workflow any)
	RegisterActivity(activity any)
}

func Register(
	w Worker,
	userclient mgmtv1alpha1connect.UserAccountServiceClient,
	jobclient mgmtv1alpha1connect.JobServiceClient,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	transformerclient mgmtv1alpha1connect.TransformersServiceClient,
	sqlmanager *sql_manager.SqlManager,
	sqlconnmanager connectionmanager.Interface[husonym_benthos_sql.SqlDbtx],
	athanor shared.AthanorPolicy,
	eelicense license.EEInterface,
	redisclient redis.UniversalClient,
	isOtelEnabled bool,
	pageLimit int,
	keys *consistencykey.Resolver,
) {
	genbenthosActivity := genbenthosconfigs_activity.New(
		jobclient,
		connclient,
		transformerclient,
		sqlmanager,
		isOtelEnabled,
		pageLimit,
		keys,
		athanor,
	)

	retrieveActivityOpts := syncactivityopts_activity.New(jobclient)
	accountStatusActivity := accountstatus_activity.New(userclient)
	runPostTableSyncActivity := posttablesync_activity.New(jobclient, sqlmanager, connclient)
	jobhookByTimingActivity := jobhooks_by_timing_activity.New(
		jobclient,
		connclient,
		sqlmanager,
		eelicense,
	)
	redisCleanUpActivity := syncrediscleanup_activity.New(redisclient)
	referentialIntegrityActivity := referentialintegrity_activity.New(jobclient, connclient, sqlmanager)
	preflightActivity := preflight_activity.New(jobclient, connclient, sqlconnmanager, sqlmanager, athanor,
		te.NewUserDefinedTransformerResolver(transformerclient))
	destinationTriggersActivity := destinationtriggers_activity.New(jobclient, connclient, sqlmanager, sqlconnmanager)

	wf := datasync_workflow.New(eelicense)

	w.RegisterWorkflow(wf.Workflow)
	w.RegisterActivity(retrieveActivityOpts.RetrieveActivityOptions)
	w.RegisterActivity(redisCleanUpActivity.DeleteRedisHash)
	w.RegisterActivity(genbenthosActivity.GenerateBenthosConfigs)
	w.RegisterActivity(accountStatusActivity.CheckAccountStatus)
	w.RegisterActivity(runPostTableSyncActivity.RunPostTableSync)
	w.RegisterActivity(jobhookByTimingActivity.RunJobHooksByTiming)
	w.RegisterActivity(referentialIntegrityActivity.CheckReferentialIntegrity)
	w.RegisterActivity(preflightActivity.CheckRunPrivileges)
	w.RegisterActivity(preflightActivity.RunPreflight)
	w.RegisterActivity(preflightActivity.CheckPreflight)
	w.RegisterActivity(genbenthosActivity.PlanPreflight)

	// The pre-flight check of a job, before any run: the same activities as a run's start.
	w.RegisterWorkflow(preflight_workflow.New().JobPreflight)
	w.RegisterActivity(destinationTriggersActivity.SuspendTriggers)
	w.RegisterActivity(destinationTriggersActivity.RestoreTriggers)
}
