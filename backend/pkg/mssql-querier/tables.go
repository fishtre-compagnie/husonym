package mssql_queries

import (
	"context"
	"database/sql"
	"encoding/json"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

const getDatabaseInfo = `-- name: GetDatabaseInfo :one
SELECT
    d.compatibility_level,
    COALESCE(d.collation_name, ''),
    CAST(SERVERPROPERTY('ProductMajorVersion') AS int),
    COALESCE(HAS_PERMS_BY_NAME(DB_NAME(), 'DATABASE', 'VIEW DEFINITION'), 0)
FROM sys.databases d
WHERE d.database_id = DB_ID();
`

type GetDatabaseInfoRow struct {
	CompatibilityLevel int
	Collation          string
	MajorVersion       int
	// CanViewDefinitions tells whether the login holds VIEW DEFINITION on the database. Without
	// it the catalog answers all the same, with the definitions of the objects left out.
	CanViewDefinitions bool
}

// GetDatabaseInfo tells the compatibility level and default collation of the current database,
// the major version of the server, and whether the login may read definitions.
func (q *Queries) GetDatabaseInfo(ctx context.Context, db mysql_queries.DBTX) (*GetDatabaseInfoRow, error) {
	var i GetDatabaseInfoRow
	err := db.QueryRowContext(ctx, getDatabaseInfo).
		Scan(&i.CompatibilityLevel, &i.Collation, &i.MajorVersion, &i.CanViewDefinitions)
	if err != nil {
		return nil, err
	}
	return &i, nil
}

const resolveTables = `-- name: ResolveTables :many
SELECT
    CAST(j.[key] AS int) AS position,
    o.object_id,
    s.name,
    o.name,
    CASE WHEN o.type = 'V' THEN 1 ELSE 0 END,
    COALESCE(t.temporal_type, 0),
    COALESCE(t.history_table_id, 0),
    COALESCE(hs.name, ''),
    COALESCE(h.name, ''),
    COALESCE(t.history_retention_period, 0),
    COALESCE(t.history_retention_period_unit_desc, ''),
    COALESCE(ps.name, ''),
    COALESCE(pe.name, ''),
    COALESCE(t.is_memory_optimized, 0),
    COALESCE(t.is_filetable, 0),
    COALESCE(t.is_external, 0),
    COALESCE(t.is_node, 0),
    COALESCE(t.is_edge, 0),
    CASE WHEN t.uses_ansi_nulls = 0 THEN 1 ELSE 0 END,
    CASE WHEN h.uses_ansi_nulls = 0 THEN 1 ELSE 0 END
FROM OPENJSON(@tables) j
JOIN sys.schemas s ON s.name = JSON_VALUE(j.value, '$.s') COLLATE CATALOG_DEFAULT
JOIN sys.objects o ON o.schema_id = s.schema_id AND o.type IN ('U', 'V')
    AND o.name = JSON_VALUE(j.value, '$.t') COLLATE CATALOG_DEFAULT
LEFT JOIN sys.tables t ON t.object_id = o.object_id
LEFT JOIN sys.tables h ON h.object_id = t.history_table_id
LEFT JOIN sys.schemas hs ON hs.schema_id = h.schema_id
LEFT JOIN sys.periods p ON p.object_id = t.object_id
LEFT JOIN sys.columns ps ON ps.object_id = p.object_id AND ps.column_id = p.start_column_id
LEFT JOIN sys.columns pe ON pe.object_id = p.object_id AND pe.column_id = p.end_column_id
ORDER BY position;
`

type ResolveTablesRow struct {
	// Position is the place of the requested table in the list given.
	Position int
	ObjectID int64
	// Schema and Name are spelled as the catalog spells them.
	Schema string
	Name   string
	// IsView tells a requested name that is a view: the other fields of the row are empty.
	IsView bool

	TemporalType      int
	HistoryID         int64
	HistorySchema     string
	HistoryName       string
	RetentionPeriod   int
	RetentionUnit     string
	PeriodStartColumn string
	PeriodEndColumn   string

	IsMemoryOptimized bool
	IsFileTable       bool
	IsExternal        bool
	IsNode            bool
	IsEdge            bool
	// AnsiNullsOff and HistoryAnsiNullsOff tell a table, and its history table, created under
	// ANSI_NULLS OFF.
	AnsiNullsOff        bool
	HistoryAnsiNullsOff bool
}

// SchemaTable names a table by its schema and its name.
type SchemaTable struct {
	Schema string `json:"s"`
	Table  string `json:"t"`
}

// ResolveTables finds the requested tables. The server compares the names, under the collation
// of the catalog; a name that is neither a table nor a view gives no row.
func (q *Queries) ResolveTables(
	ctx context.Context,
	db mysql_queries.DBTX,
	tables []SchemaTable,
) ([]*ResolveTablesRow, error) {
	encoded, err := json.Marshal(tables)
	if err != nil {
		return nil, err
	}
	return queryRows(ctx, db, resolveTables, func(rows *sql.Rows, i *ResolveTablesRow) error {
		return rows.Scan(
			&i.Position,
			&i.ObjectID,
			&i.Schema,
			&i.Name,
			&i.IsView,
			&i.TemporalType,
			&i.HistoryID,
			&i.HistorySchema,
			&i.HistoryName,
			&i.RetentionPeriod,
			&i.RetentionUnit,
			&i.PeriodStartColumn,
			&i.PeriodEndColumn,
			&i.IsMemoryOptimized,
			&i.IsFileTable,
			&i.IsExternal,
			&i.IsNode,
			&i.IsEdge,
			&i.AnsiNullsOff,
			&i.HistoryAnsiNullsOff,
		)
	}, sql.Named("tables", string(encoded)))
}
