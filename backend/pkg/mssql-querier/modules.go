package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// Every view, function, procedure and trigger of a user, written in T-SQL or held by an
// assembly, without its text. A module held by an assembly has no row in sys.sql_modules; an
// encrypted one has a row whose definition is NULL.
const getModuleHeaders = `-- name: GetModuleHeaders :many
SELECT
    o.object_id,
    s.name,
    o.name,
    RTRIM(o.type),
    o.parent_object_id,
    COALESCE(m.uses_ansi_nulls, 0),
    COALESCE(m.uses_quoted_identifier, 0),
    COALESCE(m.is_schema_bound, 0),
    CASE WHEN m.definition IS NULL THEN 0 ELSE 1 END,
    COALESCE(tr.is_disabled, 0)
FROM sys.objects o
JOIN sys.schemas s ON s.schema_id = o.schema_id
LEFT JOIN sys.sql_modules m ON m.object_id = o.object_id
LEFT JOIN sys.triggers tr ON tr.object_id = o.object_id
WHERE o.is_ms_shipped = 0
  AND o.type IN ('V', 'FN', 'IF', 'TF', 'P', 'TR', 'FS', 'FT', 'PC', 'TA', 'AF')
ORDER BY o.object_id;
`

type GetModuleHeadersRow struct {
	ObjectID int64
	Schema   string
	Name     string
	// Type is the type code of sys.objects, without its padding.
	Type                 string
	ParentID             int64
	UsesAnsiNulls        bool
	UsesQuotedIdentifier bool
	IsSchemaBound        bool
	HasDefinition        bool
	IsDisabled           bool
}

// GetModuleHeaders gives every module of the database, without its text.
func (q *Queries) GetModuleHeaders(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*GetModuleHeadersRow, error) {
	return queryRows(ctx, db, getModuleHeaders, func(rows *sql.Rows, i *GetModuleHeadersRow) error {
		return rows.Scan(
			&i.ObjectID,
			&i.Schema,
			&i.Name,
			&i.Type,
			&i.ParentID,
			&i.UsesAnsiNulls,
			&i.UsesQuotedIdentifier,
			&i.IsSchemaBound,
			&i.HasDefinition,
			&i.IsDisabled,
		)
	})
}

var getModuleDefinitions = `-- name: GetModuleDefinitions :many
SELECT m.object_id, m.definition
FROM sys.sql_modules m
WHERE m.definition IS NOT NULL AND ` + inIDs("m.object_id") + `
ORDER BY m.object_id;
`

type GetModuleDefinitionsRow struct {
	ObjectID   int64
	Definition string
}

// GetModuleDefinitions gives the text of the given modules. A module whose text cannot be
// read gives no row.
func (q *Queries) GetModuleDefinitions(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetModuleDefinitionsRow, error) {
	return queryByIDs(ctx, db, getModuleDefinitions, ids, func(rows *sql.Rows, i *GetModuleDefinitionsRow) error {
		return rows.Scan(&i.ObjectID, &i.Definition)
	})
}
