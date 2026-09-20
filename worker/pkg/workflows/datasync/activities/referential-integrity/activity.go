// Package referentialintegrity_activity checks, once every table of a run is written,
// that no destination row references a missing parent.
//
// The table syncs already avoid most orphans: nullable foreign keys to rows left out of
// the subset are read as NULL, and rows whose mandatory parent was left out are not
// written. Both rely on what the source says at the time each table is read. A parent row
// discarded when it was written, a source changing between two tables, or orphans the
// source itself holds still get through. This check is the one place that looks at what
// the destination really contains, virtual foreign keys included, which no database
// constraint guards.
package referentialintegrity_activity

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
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

// TableForeignKeys are the foreign keys of one table of the job.
type TableForeignKeys struct {
	Schema      string
	Table       string
	ForeignKeys []*tableplan.ForeignKey
}

type CheckReferentialIntegrityRequest struct {
	JobId  string
	Tables []*TableForeignKeys
}

type CheckReferentialIntegrityResponse struct {
	// RepairedRows counts the rows deleted or cleared, all destinations together.
	RepairedRows int64
}

// CheckReferentialIntegrity counts the orphans of every foreign key in each SQL
// destination of the job.
//
// When the destination lets foreign key violations be skipped and was emptied by the run,
// every row in it comes from this run: orphans are repaired the way a table sync would
// have avoided them, NULL in nullable keys and rows with a mandatory key deleted, again
// and again until nothing is left, since a deleted row can orphan its own children.
// Otherwise the run fails with the list of orphans: repairing would touch rows this run
// did not write, or hide a violation the user asked to hear about.
func (a *Activity) CheckReferentialIntegrity(
	ctx context.Context,
	req *CheckReferentialIntegrityRequest,
) (*CheckReferentialIntegrityResponse, error) {
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

	jobResp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: req.JobId}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve job: %w", err)
	}

	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activityInfo.WorkflowExecution.ID),
	)
	response := &CheckReferentialIntegrityResponse{}
	for _, destination := range jobResp.Msg.GetJob().GetDestinations() {
		policy, isSql := policyOf(destination.GetOptions())
		if !isSql {
			continue
		}
		connResp, err := a.connclient.GetConnection(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{
			Id: destination.GetConnectionId(),
		}))
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve destination connection: %w", err)
		}
		repaired, err := a.checkDestination(ctx, session, connResp.Msg.GetConnection(), req.Tables, policy, logger, slogger)
		if err != nil {
			return nil, err
		}
		response.RepairedRows += repaired
	}
	return response, nil
}

// policy is what the destination options allow once orphans are found.
type policy struct {
	skipViolations bool
	emptiedByRun   bool
}

func (p policy) canRepair() bool { return p.skipViolations && p.emptiedByRun }

func policyOf(options *mgmtv1alpha1.JobDestinationOptions) (p policy, isSql bool) {
	switch {
	case options.GetMysqlOptions() != nil:
		o := options.GetMysqlOptions()
		return policy{o.GetSkipForeignKeyViolations(), o.GetTruncateTable().GetTruncateBeforeInsert()}, true
	case options.GetPostgresOptions() != nil:
		o := options.GetPostgresOptions()
		return policy{o.GetSkipForeignKeyViolations(), o.GetTruncateTable().GetTruncateBeforeInsert()}, true
	case options.GetMssqlOptions() != nil:
		o := options.GetMssqlOptions()
		return policy{o.GetSkipForeignKeyViolations(), o.GetTruncateTable().GetTruncateBeforeInsert()}, true
	default:
		return policy{}, false
	}
}

// integrityDb is what the check needs from a destination.
type integrityDb interface {
	GetTableRowCount(ctx context.Context, schema, table string, whereClause *string) (int64, error)
	Exec(ctx context.Context, statement string) error
}

func (a *Activity) checkDestination(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connection *mgmtv1alpha1.Connection,
	tables []*TableForeignKeys,
	policy policy,
	logger log.Logger,
	slogger *slog.Logger,
) (int64, error) {
	sqlconnection, err := a.sqlmanagerclient.NewSqlConnection(ctx, session, connection,
		slogger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return 0, fmt.Errorf("unable to initialize destination sql connection: %w", err)
	}
	defer sqlconnection.Db().Close()
	return checkForeignKeys(ctx, sqlconnection.Db(), sqlconnection.Driver(), tables, policy, logger)
}

// checkForeignKeys counts the orphans of every foreign key and repairs them when the
// policy allows it. It returns the number of rows repaired.
func checkForeignKeys(
	ctx context.Context,
	db integrityDb,
	driver string,
	tables []*TableForeignKeys,
	policy policy,
	logger log.Logger,
) (int64, error) {
	var repaired int64
	// A deleted row can orphan its children, and a chain of tables is at most as long as
	// the job has tables: one more pass than that must find nothing.
	for pass := 0; pass <= len(tables); pass++ {
		var orphans []string
		var found int64
		for _, table := range tables {
			for _, fk := range table.ForeignKeys {
				condition := orphanCondition(driver, table, fk)
				count, err := db.GetTableRowCount(ctx, table.Schema, table.Table, &condition)
				if err != nil {
					return repaired, fmt.Errorf("unable to count orphans of %s.%s (%s): %w",
						table.Schema, table.Table, strings.Join(fk.Columns, ", "), err)
				}
				if count == 0 {
					continue
				}
				found += count
				orphans = append(orphans, fmt.Sprintf("%s.%s (%s) -> %s.%s: %d",
					table.Schema, table.Table, strings.Join(fk.Columns, ", "), fk.ParentSchema, fk.ParentTable, count))
				if !policy.canRepair() {
					continue
				}
				if err := db.Exec(ctx, repairStatement(driver, table, fk)); err != nil {
					return repaired, fmt.Errorf("unable to repair orphans of %s.%s (%s): %w",
						table.Schema, table.Table, strings.Join(fk.Columns, ", "), err)
				}
				repaired += count
			}
		}
		if found == 0 {
			return repaired, nil
		}
		if !policy.canRepair() {
			return 0, fmt.Errorf("referential integrity check failed, %d row(s) reference a missing parent: %s",
				found, strings.Join(orphans, "; "))
		}
		logger.Warn("referential integrity: orphans repaired", "pass", pass+1, "rows", found, "foreignKeys", orphans)
	}
	return repaired, fmt.Errorf("referential integrity check failed: orphans remain after %d repair passes", len(tables)+1)
}

// orphanCondition is the WHERE clause selecting the rows of a table whose foreign key is
// fully set (MATCH SIMPLE), is not the "no parent" value, and references no parent row.
func orphanCondition(driver string, table *TableForeignKeys, fk *tableplan.ForeignKey) string {
	quote := quoterFor(driver)
	child := quote(table.Schema) + "." + quote(table.Table)
	parent := quote(fk.ParentSchema) + "." + quote(fk.ParentTable)
	// MySQL refuses to modify a table it also reads in a subquery, unless the subquery
	// goes through a derived table. Only a self-reference needs it.
	if driver == sqlmanager_shared.MysqlDriver && fk.ParentSchema == table.Schema && fk.ParentTable == table.Table {
		parent = "(SELECT * FROM " + parent + ")"
	}

	conditions := make([]string, 0, 2*len(fk.Columns)+1)
	joins := make([]string, 0, len(fk.Columns))
	for i, column := range fk.Columns {
		conditions = append(conditions, child+"."+quote(column)+" IS NOT NULL")
		joins = append(joins, "p."+quote(fk.ParentColumns[i])+" = "+child+"."+quote(column))
	}
	if fk.NoParentValue != nil && len(fk.Columns) == 1 {
		literal := "'" + strings.ReplaceAll(*fk.NoParentValue, "'", "''") + "'"
		conditions = append(conditions, child+"."+quote(fk.Columns[0])+" <> "+literal)
	}
	conditions = append(conditions,
		"NOT EXISTS (SELECT 1 FROM "+parent+" p WHERE "+strings.Join(joins, " AND ")+")")
	return strings.Join(conditions, " AND ")
}

// repairStatement deletes the orphans of a mandatory key, and clears the nullable columns
// of any other key.
func repairStatement(driver string, table *TableForeignKeys, fk *tableplan.ForeignKey) string {
	quote := quoterFor(driver)
	child := quote(table.Schema) + "." + quote(table.Table)
	condition := orphanCondition(driver, table, fk)
	if fk.IsMandatory() {
		return "DELETE FROM " + child + " WHERE " + condition
	}
	assignments := []string{}
	for i, column := range fk.Columns {
		if i < len(fk.NotNull) && !fk.NotNull[i] {
			assignments = append(assignments, quote(column)+" = NULL")
		}
	}
	return "UPDATE " + child + " SET " + strings.Join(assignments, ", ") + " WHERE " + condition
}

func quoterFor(driver string) func(string) string {
	switch driver {
	case sqlmanager_shared.MysqlDriver:
		return func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	case sqlmanager_shared.MssqlDriver:
		return func(s string) string { return "[" + strings.ReplaceAll(s, "]", "]]") + "]" }
	default:
		return func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	}
}
