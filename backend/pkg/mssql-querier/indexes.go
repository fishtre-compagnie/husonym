package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// One row per column of an index, and one row for an index that lists no column. Heaps and
// hypothetical indexes are left out. The name of the index that backs a primary key or a unique
// constraint is the name of that constraint.
const getIndexes = `-- name: GetIndexes :many
SELECT
    t.object_id,
    s.name,
    t.name,
    i.index_id,
    i.name,
    i.type,
    i.is_unique,
    i.is_primary_key,
    i.is_unique_constraint,
    i.is_disabled,
    i.is_padded,
    i.ignore_dup_key,
    i.allow_row_locks,
    i.allow_page_locks,
    i.fill_factor,
    i.has_filter,
    COALESCE(i.filter_definition, ''),
    COALESCE(ic.index_column_id, 0),
    COALESCE(c.name, ''),
    COALESCE(ic.key_ordinal, 0),
    COALESCE(ic.is_descending_key, 0),
    COALESCE(ic.is_included_column, 0)
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.indexes i ON i.object_id = t.object_id
LEFT JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
LEFT JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
WHERE i.type > 0 AND i.is_hypothetical = 0 AND `

const orderIndexes = `
ORDER BY s.name, t.name, i.index_id, ic.index_column_id;
`

type GetIndexesRow struct {
	ObjectID    int64
	TableSchema string
	TableName   string

	IndexID            int
	Name               string
	Type               int
	IsUnique           bool
	IsPrimaryKey       bool
	IsUniqueConstraint bool
	IsDisabled         bool
	IsPadded           bool
	IgnoreDupKey       bool
	AllowRowLocks      bool
	AllowPageLocks     bool
	FillFactor         int
	HasFilter          bool
	FilterDefinition   string

	// IndexColumnID is 0 on the row of an index that lists no column.
	IndexColumnID int
	ColumnName    string
	KeyOrdinal    int
	IsDescending  bool
	IsIncluded    bool
}

func scanIndex(rows *sql.Rows, i *GetIndexesRow) error {
	return rows.Scan(
		&i.ObjectID,
		&i.TableSchema,
		&i.TableName,
		&i.IndexID,
		&i.Name,
		&i.Type,
		&i.IsUnique,
		&i.IsPrimaryKey,
		&i.IsUniqueConstraint,
		&i.IsDisabled,
		&i.IsPadded,
		&i.IgnoreDupKey,
		&i.AllowRowLocks,
		&i.AllowPageLocks,
		&i.FillFactor,
		&i.HasFilter,
		&i.FilterDefinition,
		&i.IndexColumnID,
		&i.ColumnName,
		&i.KeyOrdinal,
		&i.IsDescending,
		&i.IsIncluded,
	)
}

// GetIndexes gives the indexes of the given tables.
func (q *Queries) GetIndexes(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetIndexesRow, error) {
	return queryByIDs(ctx, db, getIndexes+inIDs("t.object_id")+orderIndexes, ids, scanIndex)
}

// GetIndexesBySchemas gives the indexes of the tables of the given schemas.
func (q *Queries) GetIndexesBySchemas(
	ctx context.Context,
	db mysql_queries.DBTX,
	schemas []string,
) ([]*GetIndexesRow, error) {
	return queryBySchemas(ctx, db, getIndexes+inSchemas+orderIndexes, schemas, scanIndex)
}
