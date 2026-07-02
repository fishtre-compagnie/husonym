package initschema_activity

import (
	"context"
	"time"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	sql_manager "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/ee/license"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

type Activity struct {
	jobclient  mgmtv1alpha1connect.JobServiceClient
	connclient mgmtv1alpha1connect.ConnectionServiceClient

	sqlmanager sql_manager.SqlManagerClient

	eelicense license.EEInterface
}

func New(
	jobclient mgmtv1alpha1connect.JobServiceClient,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	sqlmanager sql_manager.SqlManagerClient,
	eelicense license.EEInterface,
) *Activity {
	return &Activity{
		jobclient:  jobclient,
		connclient: connclient,
		sqlmanager: sqlmanager,
		eelicense:  eelicense,
	}
}

type RunSqlInitTableStatementsRequest struct {
	JobId         string
	JobRunId      string
	DestinationId string
}

type RunSqlInitTableStatementsResponse struct {
}

func (a *Activity) RunSqlInitTableStatements(
	ctx context.Context,
	req *RunSqlInitTableStatementsRequest,
) (*RunSqlInitTableStatementsResponse, error) {
	info := activity.GetInfo(ctx)
	logger := log.With(
		activity.GetLogger(ctx),
		"jobId", req.JobId,
		"WorkflowID", info.WorkflowExecution.ID,
		"RunID", info.WorkflowExecution.RunID,
		"destinationId", req.DestinationId,
	)
	go func() {
		for {
			select {
			case <-time.After(1 * time.Second):
				activity.RecordHeartbeat(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()

	builder := newInitStatementBuilder(
		a.sqlmanager,
		a.jobclient,
		a.connclient,
		a.eelicense,
		req.JobRunId,
	)
	slogger := temporallogger.NewSlogger(logger)
	return builder.RunSqlInitTableStatements(
		ctx,
		req,
		connectionmanager.NewUniqueSession(
			connectionmanager.WithSessionGroup(info.WorkflowExecution.ID),
		),
		slogger,
	)
}
