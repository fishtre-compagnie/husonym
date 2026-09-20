package destinationtriggers_activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

type Activity struct {
	jobclient  mgmtv1alpha1connect.JobServiceClient
	connclient mgmtv1alpha1connect.ConnectionServiceClient
	// sqlmanagerclient reads the triggers of a destination.
	sqlmanagerclient sqlmanager.SqlManagerClient
	// sqlconnmanager hands out the plain connection the statements run on: putting a MySQL
	// trigger back takes several statements on one session.
	sqlconnmanager connectionmanager.Interface[husonym_benthos_sql.SqlDbtx]
}

func New(
	jobclient mgmtv1alpha1connect.JobServiceClient,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	sqlmanagerclient sqlmanager.SqlManagerClient,
	sqlconnmanager connectionmanager.Interface[husonym_benthos_sql.SqlDbtx],
) *Activity {
	return &Activity{
		jobclient: jobclient, connclient: connclient, sqlmanagerclient: sqlmanagerclient, sqlconnmanager: sqlconnmanager,
	}
}

type SuspendTriggersRequest struct {
	JobId     string
	AccountId string
	Tables    []TableRef
}

type SuspendTriggersResponse struct {
	// Suspended counts the triggers taken out of the way, all destinations together.
	Suspended int
}

type RestoreTriggersRequest struct {
	JobId     string
	AccountId string
}

type RestoreTriggersResponse struct {
	// Restored counts the triggers put back, all destinations together.
	Restored int
}

// SuspendTriggers takes out of the way the triggers the destinations hold on the tables of
// the job. What puts them back is recorded for the job first, merged with what an earlier
// attempt or an earlier run of the job left recorded: a trigger is never out of the way
// without its record, and RestoreTriggers, on whatever worker and in whatever run, puts
// back exactly what was taken.
func (a *Activity) SuspendTriggers(
	ctx context.Context,
	req *SuspendTriggersRequest,
) (*SuspendTriggersResponse, error) {
	logger := a.logger(ctx, req.JobId)
	destinations, err := a.destinations(ctx, req.JobId)
	if err != nil {
		return nil, err
	}
	recorded, err := a.read(ctx, req.JobId, req.AccountId)
	if err != nil {
		return nil, err
	}

	session := a.session(ctx)
	defer a.sqlconnmanager.ReleaseSession(session, logger)
	for _, connectionID := range destinations {
		triggers, err := a.readTriggers(ctx, session, connectionID, req.Tables, logger)
		if err != nil {
			return nil, err
		}
		recorded = merge(recorded, connectionID, triggers)
	}
	if err := a.store(ctx, req.JobId, req.AccountId, recorded); err != nil {
		return nil, err
	}

	response := &SuspendTriggersResponse{}
	for _, destination := range recorded {
		if !slices.Contains(destinations, destination.ConnectionID) {
			continue // left by an earlier run on a destination the job no longer has
		}
		db, err := a.open(ctx, session, destination.ConnectionID, logger)
		if err != nil {
			return nil, err
		}
		for _, trigger := range destination.Triggers {
			// Also logged: the record is in the run context, the log is where people look.
			logger.Warn("suspending a destination trigger for the time of the run",
				"connectionId", destination.ConnectionID, "trigger", trigger.Name, "table", trigger.Table,
				"restore", trigger.Restore)
			if _, err := db.ExecContext(ctx, trigger.Suspend); err != nil {
				return nil, fmt.Errorf("unable to suspend the trigger %s of %s.%s, which writes what the run writes: %w",
					trigger.Name, trigger.Schema, trigger.Table, err)
			}
			response.Suspended++
		}
	}
	return response, nil
}

// RestoreTriggers puts back what SuspendTriggers recorded, and keeps in the record only what
// it could not put back.
func (a *Activity) RestoreTriggers(
	ctx context.Context,
	req *RestoreTriggersRequest,
) (*RestoreTriggersResponse, error) {
	logger := a.logger(ctx, req.JobId)
	recorded, err := a.read(ctx, req.JobId, req.AccountId)
	if err != nil {
		return nil, err
	}
	if len(recorded) == 0 {
		return &RestoreTriggersResponse{}, nil
	}

	session := a.session(ctx)
	defer a.sqlconnmanager.ReleaseSession(session, logger)
	response := &RestoreTriggersResponse{}
	var remaining []*suspended
	var restoreErr error
	for _, destination := range recorded {
		if restoreErr != nil {
			remaining = append(remaining, destination)
			continue
		}
		restored, err := a.restoreDestination(ctx, session, destination, logger)
		response.Restored += restored
		if err != nil {
			restoreErr = err
			remaining = append(remaining, &suspended{
				ConnectionID: destination.ConnectionID, Triggers: destination.Triggers[restored:],
			})
		}
	}
	if err := a.store(ctx, req.JobId, req.AccountId, remaining); err != nil {
		return response, errors.Join(restoreErr, err)
	}
	return response, restoreErr
}

// restoreDestination puts back the triggers of one destination, in their recorded order,
// and returns how many it put back before the first failure.
func (a *Activity) restoreDestination(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	destination *suspended,
	logger *slog.Logger,
) (int, error) {
	db, err := a.open(ctx, session, destination.ConnectionID, logger)
	if err != nil {
		return 0, err
	}
	for i, trigger := range destination.Triggers {
		if err := restoreOne(ctx, db, trigger); err != nil {
			return i, fmt.Errorf("unable to restore the trigger %s of %s.%s: %w\n%v",
				trigger.Name, trigger.Schema, trigger.Table, err, trigger.Restore)
		}
		logger.Info("destination trigger restored", "trigger", trigger.Name, "table", trigger.Table)
	}
	return len(destination.Triggers), nil
}

// restoreOne runs what puts a trigger back on one session, held by a transaction, then gives
// the session back its settings. A session whose settings could not be given back is
// thrown away rather than returned to the pool: the next statement run on it would inherit
// the sql_mode of the trigger.
func restoreOne(ctx context.Context, db sqlio.TxBeginner, trigger *Trigger) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var restoreErr error
	for _, stmt := range trigger.Restore {
		if _, restoreErr = tx.ExecContext(ctx, stmt); restoreErr != nil {
			break
		}
	}
	if trigger.Reset != "" {
		if _, err := tx.ExecContext(ctx, trigger.Reset); err != nil {
			restoreErr = errors.Join(restoreErr, err)
			// Only MySQL triggers change the session they are created on.
			if discard, ok := (sqlio.MySQLDialect{}).DiscardSessionStatement(); ok {
				_, _ = tx.ExecContext(context.WithoutCancel(ctx), discard)
			}
		}
	}
	if restoreErr != nil {
		return errors.Join(restoreErr, tx.Rollback())
	}
	return tx.Commit()
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

// readTriggers returns the triggers one destination holds on the tables of the job, each
// with what puts it back.
func (a *Activity) readTriggers(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connectionID string,
	tables []TableRef,
	logger *slog.Logger,
) ([]*Trigger, error) {
	connection, err := a.connection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	sqlconnection, err := a.sqlmanagerclient.NewSqlConnection(ctx, session, connection,
		logger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return nil, fmt.Errorf("unable to initialize destination sql connection: %w", err)
	}
	defer sqlconnection.Db().Close()
	var reader triggerReader = sqlconnection.Db()
	found, err := reader.GetSchemaTableTriggers(ctx, tablesOf(tables))
	if err != nil {
		return nil, fmt.Errorf("unable to read the triggers of the destination: %w", err)
	}
	return toTriggers(sqlconnection.Driver(), found)
}

// open returns the plain connection statements run on in a destination.
func (a *Activity) open(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connectionID string,
	logger *slog.Logger,
) (husonym_benthos_sql.SqlDbtx, error) {
	connection, err := a.connection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	db, err := a.sqlconnmanager.GetConnection(session, connection,
		logger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return nil, fmt.Errorf("unable to open the destination %q: %w", connection.GetName(), err)
	}
	return db, nil
}

func (a *Activity) connection(ctx context.Context, connectionID string) (*mgmtv1alpha1.Connection, error) {
	resp, err := a.connclient.GetConnection(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{
		Id: connectionID,
	}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve destination connection: %w", err)
	}
	return resp.Msg.GetConnection(), nil
}

func (a *Activity) session(ctx context.Context) connectionmanager.SessionInterface {
	return connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activity.GetInfo(ctx).WorkflowExecution.ID),
	)
}

// recordKey is where the triggers out of the way are recorded: per job, not per run, so
// that a run stopped before putting them back leaves them to the next run of the job.
//
// One record for the whole job holds because two runs of a job never overlap: the job is
// a Temporal schedule, and both its cron firings and the triggers of CreateJobRun go
// through its overlap policy, which is the default — skip a firing while a run is still
// going. A policy letting them overlap would have one run put the triggers back while the
// other is still writing, and this record would have to say which runs hold them.
func recordKey(jobID, accountID string) *mgmtv1alpha1.RunContextKey {
	return &mgmtv1alpha1.RunContextKey{
		JobRunId:   jobID,
		ExternalId: shared.GetSuspendedTriggersExternalId(),
		AccountId:  accountID,
	}
}

// store records what is out of the way, so the restore knows what to put back. An empty
// record says nothing is.
func (a *Activity) store(ctx context.Context, jobID, accountID string, recorded []*suspended) error {
	bits, err := json.Marshal(recorded)
	if err != nil {
		return fmt.Errorf("unable to record the suspended triggers: %w", err)
	}
	_, err = a.jobclient.SetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{
		Id:    recordKey(jobID, accountID),
		Value: bits,
	}))
	if err != nil {
		return fmt.Errorf("unable to record the suspended triggers: %w", err)
	}
	return nil
}

// read returns what is recorded as out of the way, empty when nothing was ever recorded.
func (a *Activity) read(ctx context.Context, jobID, accountID string) ([]*suspended, error) {
	resp, err := a.jobclient.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: recordKey(jobID, accountID),
	}))
	if connect.CodeOf(err) == connect.CodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to read the suspended triggers: %w", err)
	}
	var recorded []*suspended
	if err := json.Unmarshal(resp.Msg.GetValue(), &recorded); err != nil {
		return nil, fmt.Errorf("unable to read the suspended triggers: %w", err)
	}
	return recorded, nil
}

func (a *Activity) logger(ctx context.Context, jobID string) *slog.Logger {
	info := activity.GetInfo(ctx)
	return temporallogger.NewSlogger(log.With(activity.GetLogger(ctx),
		"WorkflowID", info.WorkflowExecution.ID,
		"RunID", info.WorkflowExecution.RunID,
		"jobId", jobID,
	))
}
