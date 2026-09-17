// Package runprivileges_activity stops a run at its start when a connection lacks what
// its role in the job needs.
//
// What a connection must be able to do depends on how the job uses it: a source is read,
// and may be a read-only replica; a destination is written, on a server that accepts
// writes. Without this check a run found out half way: on the first refused INSERT, after
// other tables were already written, or never, when the statement was retried for minutes.
// The message names the missing privileges so that they can be granted.
package runprivileges_activity

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
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

type CheckRunPrivilegesRequest struct {
	JobId string
}

type CheckRunPrivilegesResponse struct{}

// Privileges a role needs on every table of the job.
var (
	sourcePrivileges      = []string{"SELECT"}
	destinationPrivileges = []string{"SELECT", "INSERT", "UPDATE", "DELETE"}
)

// CheckRunPrivileges verifies the MySQL connections of a job against their role. Other
// databases are not checked yet and run as before.
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

	jobResp, err := a.jobclient.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: req.JobId}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve job: %w", err)
	}
	job := jobResp.Msg.GetJob()
	tables := jobTables(job.GetMappings())
	if len(tables) == 0 {
		return &CheckRunPrivilegesResponse{}, nil
	}

	session := connectionmanager.NewUniqueSession(
		connectionmanager.WithSessionGroup(activityInfo.WorkflowExecution.ID),
	)
	var findings []string
	if sourceID := job.GetSource().GetOptions().GetMysql().GetConnectionId(); sourceID != "" {
		found, err := a.checkConnection(ctx, session, sourceID, slogger, func(name string, db privilegesDb) ([]string, error) {
			return checkSource(ctx, db, name, tables)
		})
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	for _, destination := range job.GetDestinations() {
		options := destination.GetOptions().GetMysqlOptions()
		if options == nil {
			continue
		}
		found, err := a.checkConnection(ctx, session, destination.GetConnectionId(), slogger,
			func(name string, db privilegesDb) ([]string, error) {
				return checkDestination(ctx, db, name, tables, options.GetInitTableSchema())
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

func (a *Activity) checkConnection(
	ctx context.Context,
	session connectionmanager.SessionInterface,
	connectionID string,
	slogger *slog.Logger,
	check func(name string, db privilegesDb) ([]string, error),
) ([]string, error) {
	connResp, err := a.connclient.GetConnection(ctx, connect.NewRequest(&mgmtv1alpha1.GetConnectionRequest{Id: connectionID}))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve connection %s: %w", connectionID, err)
	}
	connection := connResp.Msg.GetConnection()
	if connection.GetConnectionConfig().GetMysqlConfig() == nil {
		return nil, nil
	}
	sqlconnection, err := a.sqlmanagerclient.NewSqlConnection(ctx, session, connection,
		slogger.With("connectionId", connection.GetId(), "accountId", connection.GetAccountId()))
	if err != nil {
		return nil, fmt.Errorf("unable to initialize sql connection %q: %w", connection.GetName(), err)
	}
	defer sqlconnection.Db().Close()
	return check(connection.GetName(), sqlconnection.Db())
}

// A run is stopped on evidence only. The privileges are read for the account 'user'@'%':
// an account declared for a specific host, or holding its rights through a role, shows no
// privilege at all. An empty answer therefore means "unknown", not "nothing granted", and
// lets the run go on as it did before this check existed.

// privilegesDb is what the check needs from a connection.
type privilegesDb interface {
	GetRolePermissionsMap(ctx context.Context) (map[string][]string, error)
	GetTableRowCount(ctx context.Context, schema, table string, whereClause *string) (int64, error)
}

func jobTables(mappings []*mgmtv1alpha1.JobMapping) []string {
	seen := map[string]bool{}
	var tables []string
	for _, mapping := range mappings {
		table := sqlmanager_shared.BuildTable(mapping.GetSchema(), mapping.GetTable())
		if !seen[table] {
			seen[table] = true
			tables = append(tables, table)
		}
	}
	sort.Strings(tables)
	return tables
}

// checkSource: a source only needs to be read. A read-only server is fine.
func checkSource(ctx context.Context, db privilegesDb, name string, tables []string) ([]string, error) {
	granted, err := db.GetRolePermissionsMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of source %q: %w", name, err)
	}
	if len(granted) == 0 {
		return nil, nil // unknown, not refused: see the note above privilegesDb
	}
	var findings []string
	for _, table := range tables {
		if missing := missingPrivileges(granted[table], sourcePrivileges); len(missing) > 0 {
			findings = append(findings, fmt.Sprintf("source %q cannot read %s (missing %s)", name, table, strings.Join(missing, ", ")))
		}
	}
	return findings, nil
}

// checkDestination: a destination must accept writes, on the server and on every table.
// A table the job is about to create has no privilege of its own yet.
func checkDestination(
	ctx context.Context,
	db privilegesDb,
	name string,
	tables []string,
	createsTables bool,
) ([]string, error) {
	// read_only refuses the writes of regular accounts, super_read_only everyone's.
	readOnly := "VARIABLE_NAME IN ('read_only', 'super_read_only') AND VARIABLE_VALUE = 'ON'"
	count, err := db.GetTableRowCount(ctx, "performance_schema", "global_variables", &readOnly)
	if err != nil {
		return nil, fmt.Errorf("unable to tell whether destination %q accepts writes: %w", name, err)
	}
	if count > 0 {
		return []string{fmt.Sprintf("destination %q is a read-only server (read_only or super_read_only is ON): "+
			"a destination must be a standalone or primary server", name)}, nil
	}

	granted, err := db.GetRolePermissionsMap(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to read the privileges of destination %q: %w", name, err)
	}
	if len(granted) == 0 {
		return nil, nil // unknown, not refused: see the note above privilegesDb
	}
	var findings []string
	for _, table := range tables {
		privileges, known := granted[table]
		if !known && createsTables {
			continue
		}
		if missing := missingPrivileges(privileges, destinationPrivileges); len(missing) > 0 {
			findings = append(findings, fmt.Sprintf("destination %q cannot write %s (missing %s)", name, table, strings.Join(missing, ", ")))
		}
	}
	return findings, nil
}

func missingPrivileges(granted, needed []string) []string {
	var missing []string
	for _, privilege := range needed {
		if !slices.ContainsFunc(granted, func(g string) bool { return strings.EqualFold(g, privilege) }) {
			missing = append(missing, privilege)
		}
	}
	return missing
}
