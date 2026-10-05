package mssql_queries

import (
	"context"
	"database/sql"
	"encoding/json"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// userSchema tells the schemas that hold the objects of a user: every schema but the two of the
// system, guest, and the nine a database gives its fixed roles. The names are compared whole: a
// user schema may be called db_anything. s is sys.schemas.
const userSchema = `s.name NOT IN (
    'sys', 'INFORMATION_SCHEMA', 'guest',
    'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin', 'db_backupoperator',
    'db_datareader', 'db_datawriter', 'db_denydatareader', 'db_denydatawriter'
)`

// userTable tells the tables of a user: those the server did not ship, less the history tables,
// which the server writes by itself. t is sys.tables.
const userTable = `t.is_ms_shipped = 0 AND t.temporal_type <> 1`

// A list of ids or of names goes to a query as one parameter, a JSON array that OPENJSON reads:
// a list has no limit of length, and a name holds any character.

// inIDs keeps the rows whose column is one of the ids of the parameter @ids.
func inIDs(column string) string {
	return column + ` IN (SELECT CAST(value AS int) FROM OPENJSON(@ids))`
}

// inSchemas keeps the rows whose schema name is one of the parameter @schemas. The names are
// compared under the collation of the catalog, as the server compares them.
const inSchemas = `s.name IN (SELECT value COLLATE CATALOG_DEFAULT FROM OPENJSON(@schemas))`

// jsonParam is a list as one named parameter.
func jsonParam[T int64 | string](name string, values []T) (sql.NamedArg, error) {
	if values == nil {
		values = []T{}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return sql.NamedArg{}, err
	}
	return sql.Named(name, string(encoded)), nil
}

// queryRows runs a query and gives one item per row, in order.
func queryRows[T any](
	ctx context.Context,
	db mysql_queries.DBTX,
	query string,
	scan func(rows *sql.Rows, item *T) error,
	args ...any,
) ([]*T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []*T{}
	for rows.Next() {
		var item T
		if err := scan(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// queryByIDs runs a query that takes the parameter @ids.
func queryByIDs[T any](
	ctx context.Context,
	db mysql_queries.DBTX,
	query string,
	ids []int64,
	scan func(rows *sql.Rows, item *T) error,
) ([]*T, error) {
	param, err := jsonParam("ids", ids)
	if err != nil {
		return nil, err
	}
	return queryRows(ctx, db, query, scan, param)
}

// queryBySchemas runs a query that takes the parameter @schemas.
func queryBySchemas[T any](
	ctx context.Context,
	db mysql_queries.DBTX,
	query string,
	schemas []string,
	scan func(rows *sql.Rows, item *T) error,
) ([]*T, error) {
	param, err := jsonParam("schemas", schemas)
	if err != nil {
		return nil, err
	}
	return queryRows(ctx, db, query, scan, param)
}
