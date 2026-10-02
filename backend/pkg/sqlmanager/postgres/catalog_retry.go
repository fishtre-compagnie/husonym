package sqlmanager_postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v7"
	pg_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/postgresql"
	"github.com/fishtre-compagnie/husonym/internal/backoffutil"
	"github.com/jackc/pgx/v5/pgconn"
)

// catalogReadAttempts bounds how often a read of the catalog is tried.
const catalogReadAttempts = 4

// catalogRetryOptions waits 50 ms before the second try, and twice as long at each one after.
func catalogRetryOptions() []backoff.RetryOption {
	wait := backoff.NewExponentialBackOff()
	wait.InitialInterval = 50 * time.Millisecond
	wait.Multiplier = 2
	wait.RandomizationFactor = 0
	return []backoff.RetryOption{
		backoff.WithBackOff(wait),
		backoff.WithMaxTries(catalogReadAttempts),
		backoff.WithNotify(func(err error, _ time.Duration) {
			slog.Default().Warn("the catalog changed under its read: reading it again", "error", err)
		}),
	}
}

// isCatalogChange says whether a read of the catalog failed because the catalog changed under
// it. The queries read the catalog of the whole database, and resolve the types, relations
// and functions they find by their id: when another session drops one of them meanwhile — a
// migration in another schema will do — PostgreSQL finds the id gone and fails the whole
// query with an internal error, "cache lookup failed for type 19421" or "could not open
// relation with OID 19421". Read again, the catalog no longer lists what was dropped.
func isCatalogChange(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "XX000" {
		return false
	}
	return strings.HasPrefix(pgErr.Message, "cache lookup failed") ||
		strings.HasPrefix(pgErr.Message, "could not open relation with OID")
}

// retryOnCatalogChange reads the catalog, again when it changed under the read.
func retryOnCatalogChange[T any](
	ctx context.Context,
	retryOpts func() []backoff.RetryOption,
	read func() (T, error),
) (T, error) {
	return backoffutil.Retry(ctx, read, retryOpts, isCatalogChange)
}

// catalogRetryQuerier reads the catalog again when it changed under a read. Every query is
// wrapped, each by hand: a query added to the interface does not build until it is too.
type catalogRetryQuerier struct {
	inner     pg_queries.Querier
	retryOpts func() []backoff.RetryOption
}

var _ pg_queries.Querier = (*catalogRetryQuerier)(nil)

func (q *catalogRetryQuerier) GetAllSchemas(ctx context.Context, db pg_queries.DBTX) ([]string, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]string, error) { return q.inner.GetAllSchemas(ctx, db) })
}

func (q *catalogRetryQuerier) GetAllTables(ctx context.Context, db pg_queries.DBTX) ([]*pg_queries.GetAllTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetAllTablesRow, error) { return q.inner.GetAllTables(ctx, db) })
}

func (q *catalogRetryQuerier) GetCompositeTypesByTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetCompositeTypesByTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetCompositeTypesByTablesRow, error) {
		return q.inner.GetCompositeTypesByTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetCustomFunctionsBySchemaAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	arg *pg_queries.GetCustomFunctionsBySchemaAndTablesParams,
) ([]*pg_queries.GetCustomFunctionsBySchemaAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetCustomFunctionsBySchemaAndTablesRow, error) {
		return q.inner.GetCustomFunctionsBySchemaAndTables(ctx, db, arg)
	})
}

func (q *catalogRetryQuerier) GetCustomSequencesBySchemaAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	arg *pg_queries.GetCustomSequencesBySchemaAndTablesParams,
) ([]*pg_queries.GetCustomSequencesBySchemaAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetCustomSequencesBySchemaAndTablesRow, error) {
		return q.inner.GetCustomSequencesBySchemaAndTables(ctx, db, arg)
	})
}

func (q *catalogRetryQuerier) GetCustomTriggersBySchemaAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetCustomTriggersBySchemaAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetCustomTriggersBySchemaAndTablesRow, error) {
		return q.inner.GetCustomTriggersBySchemaAndTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetDataTypesBySchemaAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	arg *pg_queries.GetDataTypesBySchemaAndTablesParams,
) ([]*pg_queries.GetDataTypesBySchemaAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetDataTypesBySchemaAndTablesRow, error) {
		return q.inner.GetDataTypesBySchemaAndTables(ctx, db, arg)
	})
}

func (q *catalogRetryQuerier) GetDatabaseSchema(ctx context.Context, db pg_queries.DBTX) ([]*pg_queries.GetDatabaseSchemaRow, error) {
	return retryOnCatalogChange(
		ctx,
		q.retryOpts,
		func() ([]*pg_queries.GetDatabaseSchemaRow, error) { return q.inner.GetDatabaseSchema(ctx, db) },
	)
}

func (q *catalogRetryQuerier) GetDatabaseTableSchemasBySchemasAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetDatabaseTableSchemasBySchemasAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetDatabaseTableSchemasBySchemasAndTablesRow, error) {
		return q.inner.GetDatabaseTableSchemasBySchemasAndTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetDomainsByTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetDomainsByTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetDomainsByTablesRow, error) {
		return q.inner.GetDomainsByTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetEnumTypesByTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetEnumTypesByTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetEnumTypesByTablesRow, error) {
		return q.inner.GetEnumTypesByTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetExtensionsBySchemas(
	ctx context.Context,
	db pg_queries.DBTX,
	schema []string,
) ([]*pg_queries.GetExtensionsBySchemasRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetExtensionsBySchemasRow, error) {
		return q.inner.GetExtensionsBySchemas(ctx, db, schema)
	})
}

func (q *catalogRetryQuerier) GetForeignKeyConstraintsBySchemas(
	ctx context.Context,
	db pg_queries.DBTX,
	schemas []string,
) ([]*pg_queries.GetForeignKeyConstraintsBySchemasRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetForeignKeyConstraintsBySchemasRow, error) {
		return q.inner.GetForeignKeyConstraintsBySchemas(ctx, db, schemas)
	})
}

func (q *catalogRetryQuerier) GetForeignKeyConstraintsBySchemasAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	arg *pg_queries.GetForeignKeyConstraintsBySchemasAndTablesParams,
) ([]*pg_queries.GetForeignKeyConstraintsBySchemasAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetForeignKeyConstraintsBySchemasAndTablesRow, error) {
		return q.inner.GetForeignKeyConstraintsBySchemasAndTables(ctx, db, arg)
	})
}

func (q *catalogRetryQuerier) GetIndicesBySchemasAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetIndicesBySchemasAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetIndicesBySchemasAndTablesRow, error) {
		return q.inner.GetIndicesBySchemasAndTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetNonForeignKeyTableConstraintsBySchema(
	ctx context.Context,
	db pg_queries.DBTX,
	schemas []string,
) ([]*pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetNonForeignKeyTableConstraintsBySchemaRow, error) {
		return q.inner.GetNonForeignKeyTableConstraintsBySchema(ctx, db, schemas)
	})
}

func (q *catalogRetryQuerier) GetNonForeignKeyTableConstraintsBySchemaAndTables(
	ctx context.Context,
	db pg_queries.DBTX,
	arg *pg_queries.GetNonForeignKeyTableConstraintsBySchemaAndTablesParams,
) ([]*pg_queries.GetNonForeignKeyTableConstraintsBySchemaAndTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetNonForeignKeyTableConstraintsBySchemaAndTablesRow, error) {
		return q.inner.GetNonForeignKeyTableConstraintsBySchemaAndTables(ctx, db, arg)
	})
}

func (q *catalogRetryQuerier) GetPartitionHierarchyByTable(
	ctx context.Context,
	db pg_queries.DBTX,
	table string,
) ([]*pg_queries.GetPartitionHierarchyByTableRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetPartitionHierarchyByTableRow, error) {
		return q.inner.GetPartitionHierarchyByTable(ctx, db, table)
	})
}

func (q *catalogRetryQuerier) GetPartitionedTablesBySchema(
	ctx context.Context,
	db pg_queries.DBTX,
	schema []string,
) ([]*pg_queries.GetPartitionedTablesBySchemaRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetPartitionedTablesBySchemaRow, error) {
		return q.inner.GetPartitionedTablesBySchema(ctx, db, schema)
	})
}

func (q *catalogRetryQuerier) GetPostgresRolePermissions(
	ctx context.Context,
	db pg_queries.DBTX,
) ([]*pg_queries.GetPostgresRolePermissionsRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetPostgresRolePermissionsRow, error) {
		return q.inner.GetPostgresRolePermissions(ctx, db)
	})
}

func (q *catalogRetryQuerier) GetSequencesOwnedByTables(
	ctx context.Context,
	db pg_queries.DBTX,
	schematables []string,
) ([]*pg_queries.GetSequencesOwnedByTablesRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetSequencesOwnedByTablesRow, error) {
		return q.inner.GetSequencesOwnedByTables(ctx, db, schematables)
	})
}

func (q *catalogRetryQuerier) GetUniqueIndexesBySchema(
	ctx context.Context,
	db pg_queries.DBTX,
	schema []string,
) ([]*pg_queries.GetUniqueIndexesBySchemaRow, error) {
	return retryOnCatalogChange(ctx, q.retryOpts, func() ([]*pg_queries.GetUniqueIndexesBySchemaRow, error) {
		return q.inner.GetUniqueIndexesBySchema(ctx, db, schema)
	})
}
