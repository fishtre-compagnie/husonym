package mssql_queries

import (
	"context"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

type Querier interface {
	GetDatabaseInfo(ctx context.Context, db mysql_queries.DBTX) (*GetDatabaseInfoRow, error)
	ResolveTables(ctx context.Context, db mysql_queries.DBTX, tables []SchemaTable) ([]*ResolveTablesRow, error)
	GetObjectVersions(ctx context.Context, db mysql_queries.DBTX, ids []int64) ([]*GetObjectVersionsRow, error)
	GetColumns(ctx context.Context, db mysql_queries.DBTX, ids []int64) ([]*GetColumnsRow, error)
	GetColumnsOfUserTables(ctx context.Context, db mysql_queries.DBTX) ([]*GetColumnsRow, error)
	GetIndexes(ctx context.Context, db mysql_queries.DBTX, ids []int64) ([]*GetIndexesRow, error)
	GetIndexesBySchemas(ctx context.Context, db mysql_queries.DBTX, schemas []string) ([]*GetIndexesRow, error)
	GetForeignKeys(ctx context.Context, db mysql_queries.DBTX, ids []int64) ([]*GetForeignKeysRow, error)
	GetForeignKeysBySchemas(
		ctx context.Context,
		db mysql_queries.DBTX,
		schemas []string,
	) ([]*GetForeignKeysRow, error)
	GetCheckConstraints(ctx context.Context, db mysql_queries.DBTX, ids []int64) ([]*GetCheckConstraintsRow, error)
	GetModuleHeaders(ctx context.Context, db mysql_queries.DBTX) ([]*GetModuleHeadersRow, error)
	GetDependencies(ctx context.Context, db mysql_queries.DBTX) ([]*GetDependenciesRow, error)
	GetModuleDefinitions(
		ctx context.Context,
		db mysql_queries.DBTX,
		ids []int64,
	) ([]*GetModuleDefinitionsRow, error)
	GetSequences(ctx context.Context, db mysql_queries.DBTX, ids []int64) ([]*GetSequencesRow, error)
	GetTableNotices(
		ctx context.Context,
		db mysql_queries.DBTX,
		ids []int64,
		majorVersion int,
	) ([]*GetTableNoticesRow, error)

	GetAllSchemas(ctx context.Context, db mysql_queries.DBTX) ([]string, error)
	GetAllTables(ctx context.Context, db mysql_queries.DBTX) ([]*GetAllTablesRow, error)
	GetCustomSequencesBySchemas(
		ctx context.Context,
		db mysql_queries.DBTX,
		schemas []string,
	) ([]*GetCustomSequencesBySchemasRow, error)
	GetCustomTriggersBySchemasAndTables(
		ctx context.Context,
		db mysql_queries.DBTX,
		schematables []string,
	) ([]*GetCustomTriggersBySchemasAndTablesRow, error)
	GetDataTypesBySchemas(
		ctx context.Context,
		db mysql_queries.DBTX,
		schematables []string,
	) ([]*GetDataTypesBySchemasRow, error)
	GetDatabaseSchema(ctx context.Context, db mysql_queries.DBTX) ([]*GetDatabaseSchemaRow, error)
	GetDatabaseTableSchemasBySchemasAndTables(
		ctx context.Context,
		db mysql_queries.DBTX,
		schematables []string,
	) ([]*GetDatabaseSchemaRow, error)
	GetIndicesBySchemasAndTables(
		ctx context.Context,
		db mysql_queries.DBTX,
		schematables []string,
	) ([]*GetIndicesBySchemasAndTablesRow, error)
	GetRolePermissions(ctx context.Context, db mysql_queries.DBTX) ([]*GetRolePermissionsRow, error)
	GetTableConstraintsBySchemas(
		ctx context.Context,
		db mysql_queries.DBTX,
		schemas []string,
	) ([]*GetTableConstraintsBySchemasRow, error)
	GetViewsAndFunctionsBySchemas(
		ctx context.Context,
		db mysql_queries.DBTX,
		schemas []string,
	) ([]*GetViewsAndFunctionsBySchemasRow, error)
	GetUniqueIndexesBySchema(
		ctx context.Context,
		db mysql_queries.DBTX,
		schemas []string,
	) ([]*GetUniqueIndexesBySchemaRow, error)
}

var _ Querier = (*Queries)(nil)
