// Package sqlmanager_mssql reads the catalog of a SQL Server database and runs statements on it.
package sqlmanager_mssql

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cenkalti/backoff/v7"
	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Manager is the SQL Server database of a connection: the only holder of its querier, its
// handle, its closer and its logger.
type Manager struct {
	querier mssql_queries.Querier
	db      mysql_queries.DBTX
	close   func()
	logger  *slog.Logger
	// retryOpts are the options a catalog that changed under its read is read again with.
	retryOpts func() []backoff.RetryOption
}

func NewManager(
	querier mssql_queries.Querier,
	db mysql_queries.DBTX,
	closer func(),
	logger *slog.Logger,
) *Manager {
	return &Manager{
		querier:   querier,
		db:        db,
		close:     closer,
		logger:    logger,
		retryOpts: sqlmanager_shared.CatalogRetryOptions,
	}
}

// ErrUnsupportedOperation tells an operation that SQL Server does not have. It wraps
// errors.ErrUnsupported.
func ErrUnsupportedOperation(operation string) error {
	return fmt.Errorf("sql server: %s: %w", operation, errors.ErrUnsupported)
}

// The three operations below serve the reconciliation of a destination schema, which SQL
// Server does not have.

func (m *Manager) GetTableConstraintsByTables(
	ctx context.Context,
	schema string,
	tables []string,
) (map[string]*sqlmanager_shared.AllTableConstraints, error) {
	return nil, ErrUnsupportedOperation("GetTableConstraintsByTables")
}

func (m *Manager) GetColumnsByTables(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) ([]*sqlmanager_shared.TableColumn, error) {
	return nil, ErrUnsupportedOperation("GetColumnsByTables")
}

func (m *Manager) GetDataTypesByTables(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) (*sqlmanager_shared.AllTableDataTypes, error) {
	return nil, ErrUnsupportedOperation("GetDataTypesByTables")
}

func (m *Manager) Exec(ctx context.Context, statement string) error {
	_, err := m.db.ExecContext(ctx, statement)
	return err
}

// BatchExec runs the statements one by one, each a batch of its own, and stops at the first
// that fails: SQL Server takes one batch per call.
func (m *Manager) BatchExec(
	ctx context.Context,
	batchSize int,
	statements []string,
	opts *sqlmanager_shared.BatchExecOpts,
) error {
	total := len(statements)
	for idx, stmt := range statements {
		if err := m.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("failed to execute batch statement %d/%d: %w", idx+1, total, err)
		}
	}
	return nil
}

func (m *Manager) Close() {
	if m.db != nil && m.close != nil {
		m.close()
	}
}
