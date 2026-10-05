package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// The values of a sequence are of its type: they are read as text.
var getSequences = `-- name: GetSequences :many
SELECT
    q.object_id,
    s.name,
    q.name,
    ts.name,
    ty.name,
    COALESCE(bt.name, ''),
    ty.is_user_defined,
    COALESCE(ty.is_nullable, 0),
    q.precision,
    COALESCE(q.scale, 0),
    CONVERT(nvarchar(60), q.start_value),
    CONVERT(nvarchar(60), q.increment),
    CONVERT(nvarchar(60), q.minimum_value),
    CONVERT(nvarchar(60), q.maximum_value),
    CONVERT(nvarchar(60), q.current_value),
    COALESCE(q.is_cycling, 0),
    COALESCE(q.is_cached, 0),
    COALESCE(q.cache_size, 0),
    CASE WHEN q.last_used_value IS NULL THEN 0 ELSE 1 END
FROM sys.sequences q
JOIN sys.schemas s ON s.schema_id = q.schema_id
JOIN sys.types ty ON ty.user_type_id = q.user_type_id
JOIN sys.schemas ts ON ts.schema_id = ty.schema_id
LEFT JOIN sys.types bt ON bt.user_type_id = ty.system_type_id
WHERE ` + inIDs("q.object_id") + `
ORDER BY q.object_id;
`

type GetSequencesRow struct {
	ObjectID int64
	Schema   string
	Name     string

	TypeSchema        string
	TypeName          string
	BaseTypeName      string
	IsUserDefinedType bool
	TypeIsNullable    bool
	Precision         int
	Scale             int

	StartValue   string
	Increment    string
	MinimumValue string
	MaximumValue string
	CurrentValue string
	IsCycling    bool
	IsCached     bool
	// CacheSize is 0 when the catalog records none.
	CacheSize int
	// IsUsed tells a sequence that gave a value at least once.
	IsUsed bool
}

// GetSequences gives the given sequences.
func (q *Queries) GetSequences(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetSequencesRow, error) {
	return queryByIDs(ctx, db, getSequences, ids, func(rows *sql.Rows, i *GetSequencesRow) error {
		return rows.Scan(
			&i.ObjectID,
			&i.Schema,
			&i.Name,
			&i.TypeSchema,
			&i.TypeName,
			&i.BaseTypeName,
			&i.IsUserDefinedType,
			&i.TypeIsNullable,
			&i.Precision,
			&i.Scale,
			&i.StartValue,
			&i.Increment,
			&i.MinimumValue,
			&i.MaximumValue,
			&i.CurrentValue,
			&i.IsCycling,
			&i.IsCached,
			&i.CacheSize,
			&i.IsUsed,
		)
	})
}
