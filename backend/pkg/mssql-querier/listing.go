package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

const getAllSchemas = `-- name: GetAllSchemas :many
SELECT s.name
FROM sys.schemas s
WHERE ` + userSchema + `
ORDER BY s.name;
`

// GetAllSchemas gives the names of the schemas of a user.
func (q *Queries) GetAllSchemas(ctx context.Context, db mysql_queries.DBTX) ([]string, error) {
	names, err := queryRows(ctx, db, getAllSchemas, func(rows *sql.Rows, name *string) error {
		return rows.Scan(name)
	})
	if err != nil {
		return nil, err
	}
	schemas := make([]string, len(names))
	for i, name := range names {
		schemas[i] = *name
	}
	return schemas, nil
}

const getAllTables = `-- name: GetAllTables :many
SELECT s.name, t.name
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE ` + userSchema + ` AND ` + userTable + `
ORDER BY s.name, t.name;
`

type GetAllTablesRow struct {
	TableSchema string
	TableName   string
}

// GetAllTables gives the tables of a user.
func (q *Queries) GetAllTables(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*GetAllTablesRow, error) {
	return queryRows(ctx, db, getAllTables, func(rows *sql.Rows, i *GetAllTablesRow) error {
		return rows.Scan(&i.TableSchema, &i.TableName)
	})
}
