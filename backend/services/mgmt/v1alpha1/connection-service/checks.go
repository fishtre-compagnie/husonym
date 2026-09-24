package v1alpha1_connectionservice

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	connectionchecks "github.com/fishtre-compagnie/husonym/internal/connection-checks"
)

// checkRole asks a connection whether it can do what its role in a job needs: the checks a
// run makes at its start, made before it. A connection neither MySQL nor PostgreSQL is not
// checked.
func checkRole(
	ctx context.Context,
	db *sqlmanager.SqlConnection,
	config *mgmtv1alpha1.ConnectionConfig,
	name string,
	scope *mgmtv1alpha1.ConnectionCheckScope,
) ([]*mgmtv1alpha1.ConnectionCheck, error) {
	var dialect connectionchecks.Dialect
	switch {
	case config.GetPgConfig() != nil:
		dialect = connectionchecks.Postgres
	case config.GetMysqlConfig() != nil:
		dialect = connectionchecks.MySQL
	default:
		return nil, nil
	}
	if db.Queryer() == nil {
		return nil, fmt.Errorf("the connection %q cannot be asked about its privileges", name)
	}
	tables, err := checkedTables(ctx, db, scope.GetTables())
	if err != nil {
		return nil, fmt.Errorf("unable to read the columns of the tables to check: %w", err)
	}

	var findings []*connectionchecks.Finding
	if scope.GetRole() == mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_SOURCE {
		findings, err = connectionchecks.Source(ctx, db.Queryer(), dialect, name, tables)
	} else {
		engine := scope.GetEngine()
		findings, err = connectionchecks.Destination(ctx, db.Queryer(), dialect, name, tables,
			connectionchecks.DestinationOptions{
				CreatesTables: scope.GetInitTableSchema(),
				Truncates:     scope.GetTruncateBeforeInsert(),
				// Athanor suspends foreign keys on PostgreSQL. Which engine an unspecified one is
				// depends on the deployment, which the API does not see: then the refusal is
				// reported, as a warning.
				SuspendsForeignKeys: dialect == connectionchecks.Postgres &&
					engine != mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
			})
		if engine == mgmtv1alpha1.JobEngine_JOB_ENGINE_UNSPECIFIED {
			for _, finding := range findings {
				if finding.Check == connectionchecks.CheckForeignKeySuspension {
					finding.Level = connectionchecks.Warning
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	checks := make([]*mgmtv1alpha1.ConnectionCheck, 0, len(findings))
	for _, finding := range findings {
		checks = append(checks, toConnectionCheck(finding))
	}
	return checks, nil
}

// checkedTables are the tables to check, a table given without columns taken with all of
// the columns a run can write: every one but those the database generates.
func checkedTables(
	ctx context.Context,
	db *sqlmanager.SqlConnection,
	given []*mgmtv1alpha1.ConnectionCheckTable,
) ([]*connectionchecks.Table, error) {
	tables := make([]*connectionchecks.Table, 0, len(given))
	var withoutColumns []*sqlmanager_shared.SchemaTable
	byKey := map[string]*connectionchecks.Table{}
	for _, table := range given {
		t := &connectionchecks.Table{Schema: table.GetSchema(), Table: table.GetTable(), Columns: table.GetColumns()}
		tables = append(tables, t)
		if len(t.Columns) == 0 {
			withoutColumns = append(withoutColumns, &sqlmanager_shared.SchemaTable{Schema: t.Schema, Table: t.Table})
			byKey[t.String()] = t
		}
	}
	if len(withoutColumns) == 0 {
		return tables, nil
	}
	rows, err := db.Db().GetDatabaseTableSchemasBySchemasAndTables(ctx, withoutColumns)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		t, ok := byKey[row.TableSchema+"."+row.TableName]
		if !ok || row.GeneratedType != nil && *row.GeneratedType != "" {
			continue
		}
		t.Columns = append(t.Columns, row.ColumnName)
	}
	return tables, nil
}

func toConnectionCheck(finding *connectionchecks.Finding) *mgmtv1alpha1.ConnectionCheck {
	check := &mgmtv1alpha1.ConnectionCheck{
		Kind:    checkKinds[finding.Check],
		Level:   mgmtv1alpha1.ConnectionCheck_LEVEL_BLOCKING,
		Table:   finding.Table,
		Missing: finding.Missing,
		Message: finding.Message,
	}
	if finding.Level == connectionchecks.Warning {
		check.Level = mgmtv1alpha1.ConnectionCheck_LEVEL_WARNING
	}
	if finding.Remedy != "" {
		check.Remedy = &finding.Remedy
	}
	return check
}

var checkKinds = map[connectionchecks.Check]mgmtv1alpha1.ConnectionCheck_Kind{
	connectionchecks.CheckTableExists:          mgmtv1alpha1.ConnectionCheck_KIND_TABLE_EXISTS,
	connectionchecks.CheckReadable:             mgmtv1alpha1.ConnectionCheck_KIND_READABLE,
	connectionchecks.CheckServerWritable:       mgmtv1alpha1.ConnectionCheck_KIND_SERVER_WRITABLE,
	connectionchecks.CheckWritable:             mgmtv1alpha1.ConnectionCheck_KIND_WRITABLE,
	connectionchecks.CheckTruncate:             mgmtv1alpha1.ConnectionCheck_KIND_TRUNCATE,
	connectionchecks.CheckTriggers:             mgmtv1alpha1.ConnectionCheck_KIND_TRIGGERS,
	connectionchecks.CheckTriggerDefiner:       mgmtv1alpha1.ConnectionCheck_KIND_TRIGGER_DEFINER,
	connectionchecks.CheckForeignKeySuspension: mgmtv1alpha1.ConnectionCheck_KIND_FOREIGN_KEY_SUSPENSION,
}
