package tablesync_workflow_register

import (
	"github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	benthosstream "github.com/Groupe-Hevea/neosync/internal/benthos-stream"
	connectionmanager "github.com/Groupe-Hevea/neosync/internal/connection-manager"
	neosync_benthos_mongodb "github.com/Groupe-Hevea/neosync/worker/pkg/benthos/mongodb"
	neosync_benthos_sql "github.com/Groupe-Hevea/neosync/worker/pkg/benthos/sql"
	sync_activity "github.com/Groupe-Hevea/neosync/worker/pkg/workflows/tablesync/activities/sync"
	tablesync_workflow "github.com/Groupe-Hevea/neosync/worker/pkg/workflows/tablesync/workflow"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	"go.temporal.io/sdk/client"
)

type Worker interface {
	RegisterWorkflow(workflow any)
	RegisterActivity(activity any)
}

func Register(
	w Worker,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	jobclient mgmtv1alpha1connect.JobServiceClient,
	sqlconnmanager connectionmanager.Interface[neosync_benthos_sql.SqlDbtx],
	mongoconnmanager connectionmanager.Interface[neosync_benthos_mongodb.MongoClient],
	meter metric.Meter, // optional
	benthosStreamManager benthosstream.BenthosStreamManagerClient,
	temporalclient client.Client,
	maxIterations int,
	anonymizationClient mgmtv1alpha1connect.AnonymizationServiceClient,
	redisclient redis.UniversalClient,
	athanorPolicy sync_activity.AthanorPolicy,
) {
	tsWf := tablesync_workflow.New(maxIterations)
	w.RegisterWorkflow(tsWf.TableSync)

	syncActivity := sync_activity.New(
		connclient,
		jobclient,
		sqlconnmanager,
		mongoconnmanager,
		meter,
		benthosStreamManager,
		temporalclient,
		anonymizationClient,
		redisclient,
		athanorPolicy,
	)

	w.RegisterActivity(syncActivity.Sync)
	w.RegisterActivity(syncActivity.SyncTable)
}
