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
    COALESCE(tr.is_disabled, 0),
    CASE WHEN o.type = 'V' AND EXISTS (SELECT 1 FROM sys.indexes i WHERE i.object_id = o.object_id) THEN 1 ELSE 0 END
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
	// HasIndex tells a view that has an index.
	HasIndex bool
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
			&i.HasIndex,
		)
	})
}

// The triggers of every table of a user, with their text when it can be read: an encrypted
// trigger, a trigger held by an assembly, and any trigger for a login that may not read
// definitions, have none.
const getTableTriggers = `-- name: GetTableTriggers :many
SELECT
    tr.object_id,
    p.object_id,
    ps.name,
    p.name,
    tr.name,
    RTRIM(o.type),
    tr.is_disabled,
    COALESCE(m.uses_ansi_nulls, 0),
    COALESCE(m.uses_quoted_identifier, 0),
    CASE WHEN m.definition IS NULL THEN 0 ELSE 1 END,
    COALESCE(m.definition, '')
FROM sys.triggers tr
JOIN sys.objects o ON o.object_id = tr.object_id
JOIN sys.tables p ON p.object_id = tr.parent_id
JOIN sys.schemas ps ON ps.schema_id = p.schema_id
LEFT JOIN sys.sql_modules m ON m.object_id = tr.object_id
WHERE tr.parent_class = 1 AND tr.is_ms_shipped = 0
ORDER BY tr.object_id;
`

type GetTableTriggersRow struct {
	ObjectID    int64
	ParentID    int64
	TableSchema string
	TableName   string
	Name        string
	// Type is the type code of sys.objects, without its padding.
	Type                 string
	IsDisabled           bool
	UsesAnsiNulls        bool
	UsesQuotedIdentifier bool
	HasDefinition        bool
	Definition           string
}

// GetTableTriggers gives the triggers of the tables of the database. It asks for nothing but
// the right to see the tables.
func (q *Queries) GetTableTriggers(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*GetTableTriggersRow, error) {
	return queryRows(ctx, db, getTableTriggers, func(rows *sql.Rows, i *GetTableTriggersRow) error {
		return rows.Scan(
			&i.ObjectID,
			&i.ParentID,
			&i.TableSchema,
			&i.TableName,
			&i.Name,
			&i.Type,
			&i.IsDisabled,
			&i.UsesAnsiNulls,
			&i.UsesQuotedIdentifier,
			&i.HasDefinition,
			&i.Definition,
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
