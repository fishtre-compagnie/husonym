package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"strings"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// MariaDB reports schema metadata differently than MySQL 8 in three ways that
// break the MySQL-oriented logic of this package:
//
//  1. information_schema.statistics has no EXPRESSION column. MariaDB does not
//     support expression indexes (MDEV-35853); indexed virtual columns surface
//     as regular columns, so nothing is lost by selecting NULL instead.
//  2. COLUMN_DEFAULT is stored as a valid SQL expression: string literals come
//     back quoted (abc wrapped in single quotes) and expression defaults
//     (uuid()) carry no DEFAULT_GENERATED marker in EXTRA. The manager would
//     re-quote both, producing broken DDL with doubled quotes or turning
//     expressions into string literals.
//  3. A column with DEFAULT NULL reports the literal string "NULL" instead of
//     a SQL NULL.
//
// mariadbQuerier normalizes rows back to the MySQL 8 semantics the rest of the
// package relies on: literal "NULL" defaults are treated as absent, and other
// defaults are flagged as expressions so the manager emits DEFAULT (<expr>),
// which both engines accept.
type mariadbQuerier struct {
	mysql_queries.Querier
}

func newMariadbQuerier(inner mysql_queries.Querier) *mariadbQuerier {
	return &mariadbQuerier{Querier: inner}
}

// Same query as the generated GetIndicesBySchemasAndTables, with the
// MySQL-only s.EXPRESSION column replaced by NULL.
const mariadbGetIndicesBySchemasAndTables = `-- name: GetIndicesBySchemasAndTables :many
SELECT
    s.TABLE_SCHEMA as schema_name,
    s.TABLE_NAME as table_name,
    s.COLUMN_NAME as column_name,
    NULL as expression,
    s.INDEX_NAME as index_name,
    s.INDEX_TYPE as index_type,
    s.SEQ_IN_INDEX as seq_in_index,
    s.NULLABLE as nullable
FROM information_schema.statistics s
LEFT JOIN information_schema.table_constraints tc
       ON  s.TABLE_SCHEMA = tc.TABLE_SCHEMA
       AND s.TABLE_NAME   = tc.TABLE_NAME
       AND s.INDEX_NAME   = tc.CONSTRAINT_NAME
WHERE
      s.TABLE_SCHEMA = ?
  AND s.TABLE_NAME in (/*SLICE:tables*/?)
  AND tc.CONSTRAINT_NAME IS NULL -- filters out other constraints (foreign keys, unique, primary keys, etc)
ORDER BY
    s.TABLE_NAME,
    s.INDEX_NAME,
    s.SEQ_IN_INDEX
`

func (q *mariadbQuerier) GetIndicesBySchemasAndTables(
	ctx context.Context,
	db mysql_queries.DBTX,
	arg *mysql_queries.GetIndicesBySchemasAndTablesParams,
) ([]*mysql_queries.GetIndicesBySchemasAndTablesRow, error) {
	query := mariadbGetIndicesBySchemasAndTables
	var queryParams []any
	queryParams = append(queryParams, arg.Schema)
	if len(arg.Tables) > 0 {
		for _, v := range arg.Tables {
			queryParams = append(queryParams, v)
		}
		query = strings.Replace(query, "/*SLICE:tables*/?", strings.Repeat(",?", len(arg.Tables))[1:], 1)
	} else {
		query = strings.Replace(query, "/*SLICE:tables*/?", "NULL", 1)
	}
	rows, err := db.QueryContext(ctx, query, queryParams...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*mysql_queries.GetIndicesBySchemasAndTablesRow
	for rows.Next() {
		var i mysql_queries.GetIndicesBySchemasAndTablesRow
		if err := rows.Scan(
			&i.SchemaName,
			&i.TableName,
			&i.ColumnName,
			&i.Expression,
			&i.IndexName,
			&i.IndexType,
			&i.SeqInIndex,
			&i.Nullable,
		); err != nil {
			return nil, err
		}
		items = append(items, &i)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (q *mariadbQuerier) GetDatabaseSchema(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*mysql_queries.GetDatabaseSchemaRow, error) {
	rows, err := q.Querier.GetDatabaseSchema(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.ColumnDefault = normalizeMariadbDefault(row.ColumnDefault, &row.Extra)
	}
	return rows, nil
}

func (q *mariadbQuerier) GetDatabaseTableSchemasBySchemasAndTables(
	ctx context.Context,
	db mysql_queries.DBTX,
	arg *mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesParams,
) ([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow, error) {
	rows, err := q.Querier.GetDatabaseTableSchemasBySchemasAndTables(ctx, db, arg)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.ColumnDefault = normalizeMariadbDefault(row.ColumnDefault, &row.IdentityGeneration)
	}
	return rows, nil
}

// normalizeMariadbDefault rewrites a MariaDB column default and its EXTRA
// marker to the MySQL 8 convention. It returns the new default value and
// mutates extra in place when the default is an expression.
func normalizeMariadbDefault(columnDefault any, extra *sql.NullString) any {
	def, ok := columnDefault.([]uint8)
	if !ok {
		return columnDefault
	}
	if string(def) == "NULL" {
		// MariaDB reports DEFAULT NULL as the literal string "NULL"
		return []uint8("")
	}
	if len(def) > 0 && extra.Valid && extra.String == "" {
		// MariaDB defaults are already valid SQL expressions; flagging them as
		// DEFAULT_GENERATED makes the manager emit DEFAULT (<expr>) instead of
		// re-quoting the value
		extra.String = "DEFAULT_GENERATED"
	}
	return columnDefault
}
