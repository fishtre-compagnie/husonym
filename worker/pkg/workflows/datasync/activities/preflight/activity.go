// Package preflight_activity tells, at the start of a run, what the run will meet, and
// stops it before anything is read or written when it cannot succeed.
//
// Without this a run found out half way: on the first refused INSERT, after other tables
// were already written, or never, when the statement was retried for minutes — or it went
// on, when MySQL hid triggers from an account that could not suspend them.
//
// What the plan of the tables tells is found while generating the configs (see
// internal/preflight). This activity adds what only asking the connections tells: whether
// each can do what its role in the job needs (internal/connection-checks, which the API
// asks too when a connection is tested in its role), whether the engine can run the job,
// and which triggers of the destinations the run takes out of its way. Nothing is read from
// the tables. The report is kept in the run context of the run.
package preflight_activity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	temporallogger "github.com/fishtre-compagnie/husonym/worker/internal/temporal-logger"
	husonym_benthos_sql "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/sql"
	te "github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformer_executor"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/encoding/protojson"
)

type Activity struct {
	jobclient  mgmtv1alpha1connect.JobServiceClient
	connclient mgmtv1alpha1connect.ConnectionServiceClient
	// sqlconnmanager hands out the plain SQL connection each database is asked through.
	sqlconnmanager connectionmanager.Interface[husonym_benthos_sql.SqlDbtx]
	// sqlmanagerclient reads the triggers of a destination.
	sqlmanagerclient sqlmanager.SqlManagerClient
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
	sqlmanagerclient sqlmanager.SqlManagerClient,
	athanor shared.AthanorPolicy,
	transformers te.UserDefinedTransformerResolver,
) *Activity {
	return &Activity{
		jobclient: jobclient, connclient: connclient, sqlconnmanager: sqlconnmanager,
		sqlmanagerclient: sqlmanagerclient, athanor: athanor, transformers: transformers,
	}
}

// TableColumns is a table of the run and the columns it writes, generated ones left out.
type TableColumns struct {
	Schema  string
	Table   string
	Columns []string
}

// TablesOf returns the tables of the configs of a run, with the columns the run writes
// into each.
func TablesOf(configs []*benthosbuilder.BenthosConfigResponse) []*TableColumns {
	var tables []*TableColumns
	byName := map[string]*TableColumns{}
	for _, cfg := range configs {
		key := cfg.TableSchema + "." + cfg.TableName
		table, ok := byName[key]
		if !ok {
			table = &TableColumns{Schema: cfg.TableSchema, Table: cfg.TableName}
			byName[key] = table
			tables = append(tables, table)
		}
		for _, column := range cfg.Columns {
			// A generated column is computed by the destination, never written by the run.
			if !slices.Contains(table.Columns, column) && !slices.Contains(cfg.GeneratedColumns, column) {
				table.Columns = append(table.Columns, column)
			}
		}
	}
	return tables
}

type RunPreflightRequest struct {
	JobId string
	// JobRunId and AccountId key the run context the report is kept in.
	JobRunId  string
	AccountId string
	// Tables are the tables the run reads and writes, with the columns it writes: what the
	// generated configs hold, source mappings the source no longer has left out and
	// columns added by the job's own options included.
	Tables []*TableColumns
	// Findings are what the plan of the tables tells, as the generation of the configs found.
	Findings []*preflight.Finding
}

type RunPreflightResponse struct{}

// RunPreflight completes the report of the run with what the connections tell, keeps it in
// the run context of the run, and fails on a blocking finding.
func (a *Activity) RunPreflight(ctx context.Context, req *RunPreflightRequest) (*RunPreflightResponse, error) {
	logger, slogger := loggers(ctx, req.JobId)
	stop := heartbeat(ctx)
	defer stop()

	report, findings, err := a.report(ctx, req.JobId, req.Tables, req.Findings, slogger)
	if err != nil {
		return nil, err
	}
	if err := a.keep(ctx, req, report); err != nil {
		return nil, err
	}
	for _, finding := range findings {
		if finding.Level == preflight.Warning {
			logger.Warn("pre-flight: " + finding.Message)
		}
	}
	if blocking := preflight.BlockingOf(findings); len(blocking) > 0 {
		// Asking again finds the same: the job, or the grants, have to change.
		return nil, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("pre-flight check stopped the run: %s", strings.Join(preflight.Messages(blocking), "; ")),
			"PreflightBlocking", nil)
	}
	logger.Debug("pre-flight check passed", "findings", len(findings))
	return &RunPreflightResponse{}, nil
}

type CheckPreflightRequest struct {
	JobId string
	// Tables are the tables a run would write, with the columns it would write.
	Tables []*TableColumns
	// Findings are what the plan of the tables tells.
	Findings []*preflight.Finding
}

type CheckPreflightResponse struct {
	Report *mgmtv1alpha1.PreflightReport
}

// CheckPreflight completes the report of a run not started, and returns it: what the
// pre-flight check of a job asks before any run. Nothing is kept, and a blocking finding is
// part of the report, not a failure.
func (a *Activity) CheckPreflight(ctx context.Context, req *CheckPreflightRequest) (*CheckPreflightResponse, error) {
	_, slogger := loggers(ctx, req.JobId)
	stop := heartbeat(ctx)
	defer stop()

	report, _, err := a.report(ctx, req.JobId, req.Tables, req.Findings, slogger)
	if err != nil {
		return nil, err
	}
	return &CheckPreflightResponse{Report: report}, nil
}

// report adds to what the plan told what the connections tell: whether the engine can run
// the job, what each connection lacks for its role, which destination triggers the run
// takes out of its way.
func (a *Activity) report(
	ctx context.Context,
	jobID string,
	tables []*TableColumns,
	planned []*preflight.Finding,
	slogger *slog.Logger,
) (*mgmtv1alpha1.PreflightReport, []*preflight.Finding, error) {
	job, err := a.job(ctx, jobID)
	if err != nil {
		return nil, nil, err
	}
	usesAthanor := a.athanor.UsesAthanor(job)
	findings := slices.Clone(planned)

	if err := a.engineRuns(ctx, job, usesAthanor); err != nil {
		var unsupported *shared.EngineUnsupportedError
		if !errors.As(err, &unsupported) {
			return nil, nil, err
		}
		findings = append(findings, &preflight.Finding{
			Kind:    mgmtv1alpha1.PreflightFinding_KIND_ENGINE_UNSUPPORTED,
			Level:   preflight.Blocking,
			Message: unsupported.Error(),
		})
	}

	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activity.GetInfo(ctx).WorkflowExecution.ID),
	)
	defer a.sqlconnmanager.ReleaseSession(session, slogger)
	found, err := a.connectionFindings(ctx, session, job, tables, usesAthanor, slogger)
	if err != nil {
		return nil, nil, err
	}
	// The run knows its engine: what a connection lacks stops it, as it always did. A
	// warning is what the API gives when it cannot tell the engine.
	for _, finding := range found {
		finding.Level = preflight.Blocking
	}
	findings = append(findings, found...)
	found, err = a.triggerFindings(ctx, session, job, tables, slogger)
	if err != nil {
		return nil, nil, err
	}
	findings = append(findings, found...)
	preflight.Sort(findings)

	engine := mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS
	if usesAthanor {
		engine = mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR
	}
	return preflight.Report(engine, findings), findings, nil
}

type CheckRunPrivilegesRequest struct {
	JobId string
	// Tables are the tables the run reads and writes, with the columns it writes.
	Tables []*TableColumns
}

type CheckRunPrivilegesResponse struct{}

// CheckRunPrivileges verifies the MySQL and PostgreSQL connections of a job against their
// role. It is what runs started before RunPreflight existed check at their start.
func (a *Activity) CheckRunPrivileges(
	ctx context.Context,
	req *CheckRunPrivilegesRequest,
) (*CheckRunPrivilegesResponse, error) {
	logger, slogger := loggers(ctx, req.JobId)
	stop := heartbeat(ctx)
	defer stop()

	if len(req.Tables) == 0 {
		return &CheckRunPrivilegesResponse{}, nil
	}
	job, err := a.job(ctx, req.JobId)
	if err != nil {
		return nil, err
	}
	usesAthanor := a.athanor.UsesAthanor(job)
	if err := a.engineRuns(ctx, job, usesAthanor); err != nil {
		return nil, err
	}
	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activity.GetInfo(ctx).WorkflowExecution.ID),
	)
	defer a.sqlconnmanager.ReleaseSession(session, slogger)
	findings, err := a.connectionFindings(ctx, session, job, req.Tables, usesAthanor, slogger)
	if err != nil {
		return nil, err
	}
	if len(findings) > 0 {
		return nil, fmt.Errorf("privilege check failed: %s", strings.Join(preflight.Messages(findings), "; "))
	}
	logger.Debug("privilege check passed")
	return &CheckRunPrivilegesResponse{}, nil
}

// engineRuns tells whether the engine of the run can run the job.
func (a *Activity) engineRuns(ctx context.Context, job *mgmtv1alpha1.Job, usesAthanor bool) error {
	if usesAthanor {
		return shared.AthanorRuns(job)
	}
	return shared.BenthosRuns(ctx, job, a.transformers)
}

// connectionFindings asks the MySQL and PostgreSQL connections of a job whether each can do
// what its role needs, on the tables of the run. SQL Server is not checked yet.
func (a *Activity) connectionFindings(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	job *mgmtv1alpha1.Job,
	runTables []*TableColumns,
	usesAthanor bool,
	slogger *slog.Logger,
) ([]*preflight.Finding, error) {
	if len(runTables) == 0 {
		return nil, nil
	}
	tables := make([]*connectionchecks.Table, len(runTables))
	for i, t := range runTables {
		tables[i] = &connectionchecks.Table{Schema: t.Schema, Table: t.Table, Columns: t.Columns}
	}
	var findings []*preflight.Finding

	sourceOptions := job.GetSource().GetOptions()
	sourceID := sourceOptions.GetMysql().GetConnectionId()
	if sourceID == "" {
		sourceID = sourceOptions.GetPostgres().GetConnectionId()
	}
	if sourceID != "" {
		found, err := a.check(ctx, session, sourceID, slogger,
			func(name string, db connectionchecks.Db, dialect connectionchecks.Dialect) ([]*connectionchecks.Finding, error) {
				return connectionchecks.Source(ctx, db, dialect, name, tables)
			})
		if err != nil {
			return nil, err
		}
		findings = append(findings, preflight.FromConnectionChecks(sourceID, found)...)
	}
	for _, destination := range job.GetDestinations() {
		mysqlOptions := destination.GetOptions().GetMysqlOptions()
		postgresOptions := destination.GetOptions().GetPostgresOptions()
		if mysqlOptions == nil && postgresOptions == nil {
			continue
		}
		options := connectionchecks.DestinationOptions{
			CreatesTables: mysqlOptions.GetInitTableSchema() || postgresOptions.GetInitTableSchema(),
			Truncates: mysqlOptions.GetTruncateTable().GetTruncateBeforeInsert() ||
				postgresOptions.GetTruncateTable().GetTruncateBeforeInsert(),
			// Athanor suspends foreign keys on PostgreSQL; on MySQL it is a session variable anyone sets.
			SuspendsForeignKeys: postgresOptions != nil && usesAthanor,
		}
		found, err := a.check(ctx, session, destination.GetConnectionId(), slogger,
			func(name string, db connectionchecks.Db, dialect connectionchecks.Dialect) ([]*connectionchecks.Finding, error) {
				return connectionchecks.Destination(ctx, db, dialect, name, tables, options)
			})
		if err != nil {
			return nil, err
		}
		findings = append(findings, preflight.FromConnectionChecks(destination.GetConnectionId(), found)...)
	}
	return findings, nil
}

// check asks one connection of the job, through a plain SQL connection. A connection that
// is neither MySQL nor PostgreSQL is not checked.
func (a *Activity) check(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connectionID string,
	slogger *slog.Logger,
	check func(name string, db connectionchecks.Db, dialect connectionchecks.Dialect) ([]*connectionchecks.Finding, error),
) ([]*connectionchecks.Finding, error) {
	connection, err := a.connection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	config := connection.GetConnectionConfig()
	if config.GetMysqlConfig() == nil && config.GetPgConfig() == nil {
		return nil, nil
	}
	db, err := a.sqlconnmanager.GetConnection(session, connection,
		slogger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return nil, fmt.Errorf("unable to open connection %q: %w", connection.GetName(), err)
	}
	dialect := connectionchecks.Postgres
	if config.GetMysqlConfig() != nil {
		dialect = connectionchecks.MySQL
	}
	return check(connection.GetName(), db, dialect)
}

// triggerFindings names, per MySQL and PostgreSQL destination, the triggers of the tables of
// the run that the run takes out of its way and puts back after it: while it writes, they
// do not fire. A PostgreSQL trigger the user disabled stays so, and is not named.
func (a *Activity) triggerFindings(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	job *mgmtv1alpha1.Job,
	runTables []*TableColumns,
	slogger *slog.Logger,
) ([]*preflight.Finding, error) {
	tables := make([]*sqlmanager_shared.SchemaTable, 0, len(runTables))
	for _, t := range runTables {
		tables = append(tables, &sqlmanager_shared.SchemaTable{Schema: t.Schema, Table: t.Table})
	}
	if len(tables) == 0 {
		return nil, nil
	}
	var findings []*preflight.Finding
	for _, destination := range job.GetDestinations() {
		options := destination.GetOptions()
		if options.GetMysqlOptions() == nil && options.GetPostgresOptions() == nil {
			continue
		}
		connection, err := a.connection(ctx, destination.GetConnectionId())
		if err != nil {
			return nil, err
		}
		db, err := a.sqlmanagerclient.NewSqlConnection(ctx, session, connection,
			slogger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
		if err != nil {
			return nil, fmt.Errorf("unable to open the destination %q: %w", connection.GetName(), err)
		}
		found, err := db.Db().GetSchemaTableTriggers(ctx, tables)
		db.Db().Close()
		if err != nil {
			return nil, fmt.Errorf("unable to read the triggers of the destination %q: %w", connection.GetName(), err)
		}
		byTable := map[string][]string{}
		for _, trigger := range found {
			if trigger.EnabledState == "D" {
				continue
			}
			table := sqlmanager_shared.BuildTable(trigger.Schema, trigger.Table)
			byTable[table] = append(byTable[table], trigger.TriggerName)
		}
		for table, names := range byTable {
			slices.Sort(names)
			findings = append(findings, &preflight.Finding{
				Kind:         mgmtv1alpha1.PreflightFinding_KIND_DESTINATION_TRIGGERS,
				Level:        preflight.Information,
				ConnectionID: connection.GetId(),
				Table:        table,
				Message: fmt.Sprintf("the triggers of %s on %q do not fire while the run writes it: "+
					"the run takes them out of its way, and puts them back after it (%s)",
					table, connection.GetName(), strings.Join(names, ", ")),
			})
		}
	}
	return findings, nil
}

// keep stores the report in the run context of the run, for the run page to show it.
func (a *Activity) keep(ctx context.Context, req *RunPreflightRequest, report *mgmtv1alpha1.PreflightReport) error {
	value, err := protojson.Marshal(report)
	if err != nil {
		return fmt.Errorf("unable to encode the pre-flight report: %w", err)
	}
	_, err = a.jobclient.SetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.SetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId:   req.JobRunId,
			ExternalId: shared.GetPreflightReportExternalId(),
			AccountId:  req.AccountId,
		},
		Value: value,
	}))
	if err != nil {
		return fmt.Errorf("unable to keep the pre-flight report of the run: %w", err)
	}
	return nil
}

func (a *Activity) job(ctx context.Context, jobID string) (*mgmtv1alpha1.Job, error) {
	resp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: jobID}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve job: %w", err)
	}
	return resp.Msg.GetJob(), nil
}

func (a *Activity) connection(ctx context.Context, connectionID string) (*mgmtv1alpha1.Connection, error) {
	resp, err := a.connclient.GetConnection(ctx,
		connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: connectionID}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve connection %s: %w", connectionID, err)
	}
	return resp.Msg.GetConnection(), nil
}

func loggers(ctx context.Context, jobID string) (logger log.Logger, slogger *slog.Logger) {
	info := activity.GetInfo(ctx)
	logger = log.With(activity.GetLogger(ctx),
		"WorkflowID", info.WorkflowExecution.ID,
		"RunID", info.WorkflowExecution.RunID,
		"jobId", jobID,
	)
	return logger, temporallogger.NewSlogger(logger)
}

// heartbeat records a heartbeat every second until stopped.
func heartbeat(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
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
	return cancel
}
