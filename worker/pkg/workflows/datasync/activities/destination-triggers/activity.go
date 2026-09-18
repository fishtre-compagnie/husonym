package destinationtriggers_activity

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

type Activity struct {
	jobclient        mgmtv1alpha1connect.JobServiceClient
	connclient       mgmtv1alpha1connect.ConnectionServiceClient
	sqlmanagerclient sqlmanager.SqlManagerClient
}

func New(
	jobclient mgmtv1alpha1connect.JobServiceClient,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	sqlmanagerclient sqlmanager.SqlManagerClient,
) *Activity {
	return &Activity{jobclient: jobclient, connclient: connclient, sqlmanagerclient: sqlmanagerclient}
}

type SuspendTriggersRequest struct {
	JobId     string
	JobRunId  string
	AccountId string
	Tables    []TableRef
}

type SuspendTriggersResponse struct {
	// Suspended counts the triggers taken out of the way, all destinations together.
	Suspended int
}

type RestoreTriggersRequest struct {
	JobId     string
	JobRunId  string
	AccountId string
}

type RestoreTriggersResponse struct {
	// Restored counts the triggers put back, all destinations together.
	Restored int
}

// SuspendTriggers takes out of the way the triggers the destinations hold on the tables of
// the job, after recording what puts them back in the run context of this run. RestoreTriggers reads
// that record, so a restore running on another worker puts back exactly what was taken.
func (a *Activity) SuspendTriggers(
	ctx context.Context,
	req *SuspendTriggersRequest,
) (*SuspendTriggersResponse, error) {
	logger := a.logger(ctx, req.JobId)
	destinations, err := a.destinations(ctx, req.JobId)
	if err != nil {
		return nil, err
	}

	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activity.GetInfo(ctx).WorkflowExecution.ID),
	)
	response := &SuspendTriggersResponse{}
	var stored []*suspended
	for _, connectionID := range destinations {
		db, driver, closeDb, err := a.connect(ctx, session, connectionID, logger)
		if err != nil {
			return nil, err
		}
		triggers, err := a.suspendOne(ctx, db, driver, req.Tables, connectionID, logger)
		closeDb()
		if err != nil {
			return nil, err
		}
		if len(triggers) > 0 {
			stored = append(stored, &suspended{ConnectionID: connectionID, Triggers: triggers})
			response.Suspended += len(triggers)
		}
	}

	if err := a.store(ctx, req.JobRunId, req.AccountId, stored); err != nil {
		return nil, err
	}
	return response, nil
}

// RestoreTriggers puts back what SuspendTriggers took away.
func (a *Activity) RestoreTriggers(
	ctx context.Context,
	req *RestoreTriggersRequest,
) (*RestoreTriggersResponse, error) {
	logger := a.logger(ctx, req.JobId)
	stored, err := a.read(ctx, req.JobRunId, req.AccountId)
	if err != nil {
		return nil, err
	}

	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activity.GetInfo(ctx).WorkflowExecution.ID),
	)
	response := &RestoreTriggersResponse{}
	for _, destination := range stored {
		db, _, closeDb, err := a.connect(ctx, session, destination.ConnectionID, logger)
		if err != nil {
			return nil, err
		}
		restored, err := restoreOne(ctx, db, destination.Triggers, logger)
		closeDb()
		response.Restored += restored
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

// suspendOne takes out of the way the triggers one destination holds on the tables of
// the job, and returns what puts them back.
func (a *Activity) suspendOne(
	ctx context.Context,
	db triggerReader,
	driver string,
	tables []TableRef,
	connectionID string,
	logger *slog.Logger,
) ([]*Trigger, error) {
	found, err := db.GetSchemaTableTriggers(ctx, tablesOf(tables))
	if err != nil {
		return nil, fmt.Errorf("unable to read the triggers of the destination: %w", err)
	}
	triggers := toTriggers(driver, found)
	for _, trigger := range triggers {
		// Logged before suspending: a run terminated before it restores them leaves, in its
		// own history, the statements that put them back.
		logger.Warn("suspending a destination trigger for the time of the run",
			"connectionId", connectionID, "trigger", trigger.Name, "table", trigger.Table,
			"restore", trigger.Restore)
		if err := db.Exec(ctx, trigger.Suspend); err != nil {
			return nil, fmt.Errorf("unable to suspend the trigger %s of %s.%s, which writes what the run writes: %w",
				trigger.Name, trigger.Schema, trigger.Table, err)
		}
	}
	return triggers, nil
}

// restoreOne puts back the triggers of one destination and returns how many it put back.
func restoreOne(
	ctx context.Context,
	db triggerReader,
	triggers []*Trigger,
	logger *slog.Logger,
) (int, error) {
	restored := 0
	for _, trigger := range triggers {
		if err := db.Exec(ctx, trigger.Restore); err != nil {
			return restored, fmt.Errorf("unable to restore the trigger %s of %s.%s: %w\n%s",
				trigger.Name, trigger.Schema, trigger.Table, err, trigger.Restore)
		}
		logger.Info("destination trigger restored", "trigger", trigger.Name, "table", trigger.Table)
		restored++
	}
	return restored, nil
}

// destinations returns the connection ids of the SQL destinations of the job.
func (a *Activity) destinations(ctx context.Context, jobID string) ([]string, error) {
	resp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobID}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve job: %w", err)
	}
	var connections []string
	for _, destination := range resp.Msg.GetJob().GetDestinations() {
		options := destination.GetOptions()
		if options.GetMysqlOptions() == nil && options.GetPostgresOptions() == nil && options.GetMssqlOptions() == nil {
			continue
		}
		connections = append(connections, destination.GetConnectionId())
	}
	return connections, nil
}

// connect opens a destination and returns it with its driver, and what closes it.
func (a *Activity) connect(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connectionID string,
	logger *slog.Logger,
) (db triggerReader, driver string, closeDb func(), err error) {
	resp, err := a.connclient.GetConnection(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{
		Id: connectionID,
	}))
	if err != nil {
		return nil, "", nil, fmt.Errorf("unable to retrieve destination connection: %w", err)
	}
	connection := resp.Msg.GetConnection()
	sqlconnection, err := a.sqlmanagerclient.NewSqlConnection(ctx, session, connection,
		logger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return nil, "", nil, fmt.Errorf("unable to initialize destination sql connection: %w", err)
	}
	return sqlconnection.Db(), sqlconnection.Driver(), sqlconnection.Db().Close, nil
}

// store records what was suspended, so the restore knows what to put back.
func (a *Activity) store(ctx context.Context, jobRunID, accountID string, stored []*suspended) error {
	bits, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("unable to record the suspended triggers: %w", err)
	}
	_, err = a.jobclient.SetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId:   jobRunID,
			ExternalId: shared.GetSuspendedTriggersExternalId(),
			AccountId:  accountID,
		},
		Value: bits,
	}))
	if err != nil {
		return fmt.Errorf("unable to record the suspended triggers: %w", err)
	}
	return nil
}

// read returns what the suspending activity recorded, empty when it recorded nothing.
func (a *Activity) read(ctx context.Context, jobRunID, accountID string) ([]*suspended, error) {
	resp, err := a.jobclient.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId:   jobRunID,
			ExternalId: shared.GetSuspendedTriggersExternalId(),
			AccountId:  accountID,
		},
	}))
	if connect.CodeOf(err) == connect.CodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read the suspended triggers: %w", err)
	}
	var stored []*suspended
	if err := json.Unmarshal(resp.Msg.GetValue(), &stored); err != nil {
		return nil, fmt.Errorf("unable to read the suspended triggers: %w", err)
	}
	return stored, nil
}

func (a *Activity) logger(ctx context.Context, jobID string) *slog.Logger {
	info := activity.GetInfo(ctx)
	return temporallogger.NewSlogger(log.With(activity.GetLogger(ctx),
		"WorkflowID", info.WorkflowExecution.ID,
		"RunID", info.WorkflowExecution.RunID,
		"jobId", jobID,
	))
}
