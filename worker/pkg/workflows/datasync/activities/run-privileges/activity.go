// Package runprivileges_activity stops a run at its start when a connection lacks what
// its role in the job needs.
//
// What a connection must be able to do depends on how the job uses it: a source is read,
// and may be a read-only replica; a destination is written, on a server that accepts
// writes, emptied first when the job says so, and has the triggers of its tables taken out
// of the way of the run and put back. Without this check a run found out half way: on the
// first refused INSERT, after other tables were already written, or never, when the
// statement was retried for minutes — or it went on, when MySQL hid triggers from an
// account that could not suspend them. The message names what is missing so that it can
// be granted.
package runprivileges_activity

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

type Activity struct {
	jobclient  mgmtv1alpha1connect.JobServiceClient
	connclient mgmtv1alpha1connect.ConnectionServiceClient
	// sqlconnmanager hands out the plain SQL connection each database is asked through.
	sqlconnmanager connectionmanager.Interface[husonym_benthos_sql.SqlDbtx]
	// athanor tells which engine runs the job, the one the table syncs will use: what only
	// Athanor needs must not be required of Benthos.
	athanor shared.AthanorPolicy
	// transformers resolves the user-defined transformers, whose rules may need Athanor.
	transformers te.UserDefinedTransformerResolver
}

func New(
	jobclient mgmtv1alpha1connect.JobServiceClient,
	connclient mgmtv1alpha1connect.ConnectionServiceClient,
	sqlconnmanager connectionmanager.Interface[husonym_benthos_sql.SqlDbtx],
	athanor shared.AthanorPolicy,
	transformers te.UserDefinedTransformerResolver,
) *Activity {
	return &Activity{
		jobclient: jobclient, connclient: connclient, sqlconnmanager: sqlconnmanager,
		athanor: athanor, transformers: transformers,
	}
}

type CheckRunPrivilegesRequest struct {
	JobId string
	// Tables are the tables the run reads and writes, with the columns it writes: what the
	// generated configs hold, source mappings the source no longer has left out and
	// columns added by the job's own options included.
	Tables []*TableColumns
}

// TableColumns is a table of the run and the columns it writes, generated ones left out.
type TableColumns struct {
	Schema  string
	Table   string
	Columns []string
}

type CheckRunPrivilegesResponse struct{}

// Privileges a role needs on every table of the job.
var (
	sourcePrivileges      = []string{"SELECT"}
	destinationPrivileges = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
)

// CheckRunPrivileges verifies the MySQL and PostgreSQL connections of a job against their
// role. SQL Server is not checked yet and runs as before.
func (a *Activity) CheckRunPrivileges(
	ctx context.Context,
	req *CheckRunPrivilegesRequest,
) (*CheckRunPrivilegesResponse, error) {
	activityInfo := activity.GetInfo(ctx)
	logger := log.With(activity.GetLogger(ctx),
		"WorkflowID", activityInfo.WorkflowExecution.ID,
		"RunID", activityInfo.WorkflowExecution.RunID,
		"jobId", req.JobId,
	)
	slogger := temporallogger.NewSlogger(logger)

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

	tables := make([]*jobTable, len(req.Tables))
	for i, t := range req.Tables {
		tables[i] = &jobTable{Schema: t.Schema, Table: t.Table, Columns: t.Columns}
	}
	if len(tables) == 0 {
		return &CheckRunPrivilegesResponse{}, nil
	}
	jobResp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: req.JobId}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve job: %w", err)
	}
	job := jobResp.Msg.GetJob()
	usesAthanor := a.athanor.UsesAthanor(job)
	if usesAthanor {
		if err := shared.AthanorRuns(job); err != nil {
			return nil, err
		}
	} else if err := shared.BenthosRuns(ctx, job, a.transformers); err != nil {
		return nil, err
	}

	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activityInfo.WorkflowExecution.ID),
	)
	defer a.sqlconnmanager.ReleaseSession(session, slogger)
	var findings []string

	sourceOptions := job.GetSource().GetOptions()
	sourceID := sourceOptions.GetMysql().GetConnectionId()
	if sourceID == "" {
		sourceID = sourceOptions.GetPostgres().GetConnectionId()
	}
	if sourceID != "" {
		found, err := a.check(ctx, session, sourceID, slogger, func(name string, db sqlDb, isMysql bool) ([]string, error) {
			if isMysql {
				return checkMysqlSource(ctx, db, name, tables)
			}
			return checkPostgresSource(ctx, db, name, tables)
		})
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	for _, destination := range job.GetDestinations() {
		mysqlOptions := destination.GetOptions().GetMysqlOptions()
		postgresOptions := destination.GetOptions().GetPostgresOptions()
		if mysqlOptions == nil && postgresOptions == nil {
			continue
		}
		found, err := a.check(ctx, session, destination.GetConnectionId(), slogger,
			func(name string, db sqlDb, isMysql bool) ([]string, error) {
				if isMysql {
					return checkMysqlDestination(ctx, db, name, tables, mysqlOptions.GetInitTableSchema(),
						mysqlOptions.GetTruncateTable().GetTruncateBeforeInsert())
				}
				return checkPostgresDestination(ctx, db, name, tables, postgresOptions.GetInitTableSchema(),
					postgresOptions.GetTruncateTable().GetTruncateBeforeInsert(), usesAthanor)
			})
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	if len(findings) > 0 {
		return nil, fmt.Errorf("privilege check failed: %s", strings.Join(findings, "; "))
	}
	logger.Debug("privilege check passed")
	return &CheckRunPrivilegesResponse{}, nil
}

// check asks one connection of the job, through a plain SQL connection. A connection that
// is neither MySQL nor PostgreSQL is not checked.
func (a *Activity) check(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connectionID string,
	slogger *slog.Logger,
	check func(name string, db sqlDb, isMysql bool) ([]string, error),
) ([]string, error) {
	connResp, err := a.connclient.GetConnection(ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: connectionID}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve connection %s: %w", connectionID, err)
	}
	connection := connResp.Msg.GetConnection()
	config := connection.GetConnectionConfig()
	if config.GetMysqlConfig() == nil && config.GetPgConfig() == nil {
		return nil, nil
	}
	db, err := a.sqlconnmanager.GetConnection(session, connection,
		slogger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return nil, fmt.Errorf("unable to open connection %q: %w", connection.GetName(), err)
	}
	return check(connection.GetName(), db, config.GetMysqlConfig() != nil)
}

// jobTable is a table of the run with the columns it writes.
type jobTable struct {
	Schema  string
	Table   string
	Columns []string
}

func (t *jobTable) String() string { return t.Schema + "." + t.Table }
