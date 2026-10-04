package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// One row per column of a foreign key: both sides, and the actions as the codes of the catalog.
const getForeignKeys = `-- name: GetForeignKeys :many
SELECT
    t.object_id,
    s.name,
    t.name,
    fk.object_id,
    fk.name,
    fk.referenced_object_id,
    rs.name,
    rt.name,
    fk.delete_referential_action,
    fk.update_referential_action,
    fk.is_disabled,
    fk.is_not_trusted,
    fk.is_not_for_replication,
    pc.name,
    COALESCE(pc.is_nullable, 0),
    rc.name
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.foreign_keys fk ON fk.parent_object_id = t.object_id
JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
JOIN sys.tables rt ON rt.object_id = fk.referenced_object_id
JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
WHERE `

const orderForeignKeys = `
ORDER BY s.name, t.name, fk.name, fk.object_id, fkc.constraint_column_id;
`

type GetForeignKeysRow struct {
	ObjectID    int64
	TableSchema string
	TableName   string

	ConstraintID        int64
	Name                string
	ReferencedID        int64
	ReferencedSchema    string
	ReferencedTable     string
	DeleteAction        int
	UpdateAction        int
	IsDisabled          bool
	IsNotTrusted        bool
	IsNotForReplication bool

	ColumnName       string
	ColumnIsNullable bool
	ReferencedColumn string
}

func scanForeignKey(rows *sql.Rows, i *GetForeignKeysRow) error {
	return rows.Scan(
		&i.ObjectID,
		&i.TableSchema,
		&i.TableName,
		&i.ConstraintID,
		&i.Name,
		&i.ReferencedID,
		&i.ReferencedSchema,
		&i.ReferencedTable,
		&i.DeleteAction,
		&i.UpdateAction,
		&i.IsDisabled,
		&i.IsNotTrusted,
		&i.IsNotForReplication,
		&i.ColumnName,
		&i.ColumnIsNullable,
		&i.ReferencedColumn,
	)
}

// GetForeignKeys gives the foreign keys of the given tables.
func (q *Queries) GetForeignKeys(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetForeignKeysRow, error) {
	return queryByIDs(ctx, db, getForeignKeys+inIDs("t.object_id")+orderForeignKeys, ids, scanForeignKey)
}

// GetForeignKeysBySchemas gives the foreign keys of the tables of the given schemas.
func (q *Queries) GetForeignKeysBySchemas(
	ctx context.Context,
	db mysql_queries.DBTX,
	schemas []string,
) ([]*GetForeignKeysRow, error) {
	return queryBySchemas(ctx, db, getForeignKeys+inSchemas+orderForeignKeys, schemas, scanForeignKey)
}
