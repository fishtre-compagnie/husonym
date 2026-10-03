package ddbuilder_mssql

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_mssql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	tabledependency "github.com/fishtre-compagnie/husonym/backend/pkg/table-dependency"
	connectionmanager "github.com/fishtre-compagnie/husonym/internal/connection-manager"
	"github.com/fishtre-compagnie/husonym/internal/license"
	shared "github.com/fishtre-compagnie/husonym/internal/schema-manager/shared"
)

type MssqlSchemaManager struct {
	logger                *slog.Logger
	eelicense             license.EEInterface
	sqlmanagerclient      sqlmanager.SqlManagerClient
	sourceConnection      *mgmtv1alpha1.Connection
	destinationConnection *mgmtv1alpha1.Connection
	destOpts              *mgmtv1alpha1.MssqlDestinationConnectionOptions
	destdb                *sqlmanager.SqlConnection
	sourcedb              *sqlmanager.SqlConnection
}

func NewMssqlSchemaManager(
	ctx context.Context,
	logger *slog.Logger,
	eelicense license.EEInterface,
	session connectionmanager.SessionInterface,
	sqlmanagerclient sqlmanager.SqlManagerClient,
	sourceConnection *mgmtv1alpha1.Connection,
	destinationConnection *mgmtv1alpha1.Connection,
	destOpts *mgmtv1alpha1.MssqlDestinationConnectionOptions,
) (*MssqlSchemaManager, error) {
	sourcedb, err := sqlmanagerclient.NewSqlConnection(ctx, session, sourceConnection, logger)
	if err != nil {
		return nil, fmt.Errorf("unable to create new sql db: %w", err)
	}

	destdb, err := sqlmanagerclient.NewSqlConnection(ctx, session, destinationConnection, logger)
	if err != nil {
		return nil, fmt.Errorf("unable to create new sql db: %w", err)
	}

	return &MssqlSchemaManager{
		logger:                logger,
		eelicense:             eelicense,
		sqlmanagerclient:      sqlmanagerclient,
		sourceConnection:      sourceConnection,
		destinationConnection: destinationConnection,
		destOpts:              destOpts,
		destdb:                destdb,
		sourcedb:              sourcedb,
	}, nil
}

func (d *MssqlSchemaManager) InitializeSchema(
	ctx context.Context,
	uniqueTables map[string]struct{},
) ([]*shared.InitSchemaError, error) {
	initErrors := []*shared.InitSchemaError{}
	if !d.destOpts.GetInitTableSchema() {
		d.logger.Info("skipping schema init as it is not enabled")
		return initErrors, nil
	}
	if !d.eelicense.IsValid() {
		return nil, fmt.Errorf(
			"invalid or non-existent Husonym License. SQL Server schema init requires valid Enterprise license",
		)
	}
	tables, err := d.requestedTables(ctx, uniqueTables)
	if err != nil {
		return nil, err
	}

	initblocks, err := d.sourcedb.Db().GetSchemaInitStatements(ctx, tables)
	if err != nil {
		return nil, err
	}

	for _, block := range initblocks {
		d.logger.Info(
			fmt.Sprintf(
				"[%s] found %d statements to execute during schema initialization",
				block.Label,
				len(block.Statements),
			),
		)
		// What the block leaves out is recorded with what fails in it.
		for _, skipped := range block.Skipped {
			d.logger.Warn(
				fmt.Sprintf("[%s] skipped %s: %s", block.Label, skipped.Object, skipped.Reason),
			)
			initErrors = append(initErrors, &shared.InitSchemaError{
				Statement: skipped.Object,
				Error:     "skipped: " + skipped.Reason,
			})
		}
		for _, stmt := range block.Statements {
			err = d.destdb.Db().Exec(ctx, stmt)
			if err != nil {
				d.logger.Error(
					fmt.Sprintf("unable to exec mssql %s statements: %s", block.Label, err.Error()),
				)
				// A view, a function or a procedure may need what the destination does not
				// hold: it is the one kind of statement whose failure does not stop the run.
				if block.Label != sqlmanager_mssql.ViewsFunctionsLabel {
					return nil, fmt.Errorf(
						"unable to exec mssql %s statements: %w",
						block.Label,
						err,
					)
				}
				initErrors = append(initErrors, &shared.InitSchemaError{
					Statement: stmt,
					Error:     err.Error(),
				})
			}
		}
	}
	return initErrors, nil
}

// requestedTables turns the keys of the tables of a job into tables of the source, in the order
// of the keys. A key is schema.table and either part may hold a dot: the key is looked up among
// the tables the source has. A key that none of them builds is split at its first dot, and the
// source tells whether it has that table.
func (d *MssqlSchemaManager) requestedTables(
	ctx context.Context,
	uniqueTables map[string]struct{},
) ([]*sqlmanager_shared.SchemaTable, error) {
	sourceTables, err := d.sourcedb.Db().GetAllTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to list the tables of the source: %w", err)
	}
	byKey := map[string][]*sqlmanager_shared.SchemaTable{}
	for _, table := range sourceTables {
		key := sqlmanager_shared.BuildTable(table.SchemaName, table.TableName)
		byKey[key] = append(byKey[key], &sqlmanager_shared.SchemaTable{
			Schema: table.SchemaName,
			Table:  table.TableName,
		})
	}

	tables := make([]*sqlmanager_shared.SchemaTable, 0, len(uniqueTables))
	for _, key := range slices.Sorted(maps.Keys(uniqueTables)) {
		switch matches := byKey[key]; len(matches) {
		case 0:
			schema, table := sqlmanager_shared.SplitTableKey(key)
			tables = append(tables, &sqlmanager_shared.SchemaTable{Schema: schema, Table: table})
		case 1:
			tables = append(tables, matches[0])
		default:
			return nil, fmt.Errorf(
				"table key %q names two tables of the source: %s and %s",
				key,
				ddl.QualifiedName(matches[0].Schema, matches[0].Table),
				ddl.QualifiedName(matches[1].Schema, matches[1].Table),
			)
		}
	}
	return tables, nil
}

func (d *MssqlSchemaManager) TruncateData(
	ctx context.Context,
	uniqueTables map[string]struct{},
	uniqueSchemas []string,
) error {
	if !d.destOpts.GetTruncateTable().GetTruncateBeforeInsert() {
		d.logger.Info("skipping truncate as it is not enabled")
		return nil
	}
	tableDependencies, err := d.sourcedb.Db().GetTableConstraintsBySchema(ctx, uniqueSchemas)
	if err != nil {
		return fmt.Errorf("unable to retrieve database foreign key constraints: %w", err)
	}
	d.logger.Info(
		fmt.Sprintf(
			"found %d foreign key constraints for database",
			len(tableDependencies.ForeignKeyConstraints),
		),
	)
	tablePrimaryDependencyMap := shared.GetFilteredForeignToPrimaryTableMap(
		tableDependencies.ForeignKeyConstraints,
		uniqueTables,
	)
	orderedTablesResp, err := tabledependency.GetTablesOrderedByDependency(
		tablePrimaryDependencyMap,
	)
	if err != nil {
		return err
	}

	orderedTableDelete := []string{}
	for i := len(orderedTablesResp.OrderedTables) - 1; i >= 0; i-- {
		st := orderedTablesResp.OrderedTables[i]
		orderedTableDelete = append(
			orderedTableDelete,
			sqlmanager_mssql.BuildMssqlDeleteStatement(st.Schema, st.Table),
		)
	}

	d.logger.Info(
		fmt.Sprintf(
			"executing %d sql statements that will delete from tables",
			len(orderedTableDelete),
		),
	)
	err = d.destdb.Db().BatchExec(ctx, 10, orderedTableDelete, &sqlmanager_shared.BatchExecOpts{})
	if err != nil {
		return fmt.Errorf("unable to exec ordered delete from statements: %w", err)
	}

	// reset identity column counts
	schemaColMap, err := d.sourcedb.Db().GetSchemaColumnMap(ctx)
	if err != nil {
		return err
	}

	identityStmts := []string{}
	for table, cols := range schemaColMap {
		if _, ok := uniqueTables[table]; !ok {
			continue
		}
		for _, c := range cols {
			if c.IdentityGeneration != nil && *c.IdentityGeneration != "" {
				identityResetStatement := sqlmanager_mssql.BuildMssqlIdentityColumnResetStatement(
					c.TableSchema,
					c.TableName,
					c.IdentitySeed,
					c.IdentityIncrement,
				)
				identityStmts = append(identityStmts, identityResetStatement)
			}
		}
	}
	if len(identityStmts) > 0 {
		err = d.destdb.Db().BatchExec(ctx, 10, identityStmts, &sqlmanager_shared.BatchExecOpts{})
		if err != nil {
			return fmt.Errorf("unable to exec identity reset statements: %w", err)
		}
	}
	return nil
}

func (d *MssqlSchemaManager) CalculateSchemaDiff(
	ctx context.Context,
	uniqueTables map[string]*sqlmanager_shared.SchemaTable,
) (*shared.SchemaDifferences, error) {
	return nil, sqlmanager_mssql.ErrUnsupportedOperation("CalculateSchemaDiff")
}

func (d *MssqlSchemaManager) BuildSchemaDiffStatements(
	ctx context.Context,
	diff *shared.SchemaDifferences,
) ([]*sqlmanager_shared.InitSchemaStatements, error) {
	return nil, sqlmanager_mssql.ErrUnsupportedOperation("BuildSchemaDiffStatements")
}

func (d *MssqlSchemaManager) ReconcileDestinationSchema(
	ctx context.Context,
	uniqueTables map[string]*sqlmanager_shared.SchemaTable,
	schemaStatements []*sqlmanager_shared.InitSchemaStatements,
) ([]*shared.InitSchemaError, error) {
	return nil, sqlmanager_mssql.ErrUnsupportedOperation("ReconcileDestinationSchema")
}

func (d *MssqlSchemaManager) TruncateTables(
	ctx context.Context,
	schemaDiff *shared.SchemaDifferences,
) error {
	return sqlmanager_mssql.ErrUnsupportedOperation("TruncateTables")
}
func (d *MssqlSchemaManager) CloseConnections() {
	d.destdb.Db().Close()
	d.sourcedb.Db().Close()
}
