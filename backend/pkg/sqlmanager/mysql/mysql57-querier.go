package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"regexp"
	"strings"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// MySQL 5.7 lacks parts of the catalog the generated queries read, and reports column
// defaults with other markers than MySQL 8:
//
//  1. information_schema.check_constraints does not exist (8.0.16+): 5.7 parses CHECK
//     clauses and drops them, so there is no check to read. The constraint queries are the
//     generated ones without that join, every row carrying the empty check clause the
//     generated queries give a constraint that is not a check.
//  2. information_schema.statistics has no EXPRESSION column (8.0.13+), and 5.7 has no
//     functional key parts: NULL is selected instead, as for MariaDB.
//  3. A default of CURRENT_TIMESTAMP (or CURRENT_TIMESTAMP(n), which NOW(), LOCALTIME and
//     their other synonyms are reported as) on a TIMESTAMP or DATETIME column carries no
//     DEFAULT_GENERATED marker in EXTRA, which is empty or holds "on update CURRENT_TIMESTAMP".
//     The manager would write the first as the string 'CURRENT_TIMESTAMP', and the second
//     as DEFAULT (CURRENT_TIMESTAMP), an expression default 5.7 refuses (8.0.13+). Nor
//     would it quote a literal default, such as '0000-00-00 00:00:00', on a column
//     updated on CURRENT_TIMESTAMP: it takes any non-empty EXTRA for an expression marker.
//
// mysql57Querier normalizes the rows so that the manager writes DDL a 5.7 server accepts:
// a CURRENT_TIMESTAMP default gets a NULL EXTRA, with which the manager writes the default
// as it is (DEFAULT CURRENT_TIMESTAMP, valid on every MySQL and MariaDB), and the "on
// update" marker is dropped from EXTRA so that any other default is quoted as the literal
// it is. The manager never writes an ON UPDATE clause, whatever the flavor: dropping the
// marker loses nothing it used.
type mysql57Querier struct {
	mysql_queries.Querier
}

func newMysql57Querier(inner mysql_queries.Querier) *mysql57Querier {
	return &mysql57Querier{Querier: inner}
}

func (q *mysql57Querier) GetIndicesBySchemasAndTables(
	ctx context.Context,
	db mysql_queries.DBTX,
	arg *mysql_queries.GetIndicesBySchemasAndTablesParams,
) ([]*mysql_queries.GetIndicesBySchemasAndTablesRow, error) {
	return queryIndicesWithoutExpression(ctx, db, arg)
}

// mysql57TableConstraintsFrom is the select list and the joins of the generated
// GetTableConstraints and GetTableConstraintsBySchemas, without information_schema.
// check_constraints: the check clause is the empty string the generated queries give every
// constraint that is not a check.
const mysql57TableConstraintsFrom = `SELECT
    tc.table_schema AS schema_name,
    tc.table_name AS table_name,
    JSON_ARRAYAGG(kcu.column_name) AS constraint_columns,
    JSON_ARRAYAGG(CASE WHEN c.is_nullable = 'YES' THEN 0 ELSE 1 END) AS not_nullable,
    tc.constraint_name AS constraint_name,
    tc.constraint_type AS constraint_type,
    COALESCE(kcu.referenced_table_schema, 'NULL') AS referenced_schema_name,
    COALESCE(kcu.referenced_table_name, 'NULL') AS referenced_table_name,
    JSON_ARRAYAGG(kcu.referenced_column_name) AS referenced_column_names,
    JSON_ARRAYAGG(JSON_OBJECT('column', kcu.column_name, 'sub_part', s.sub_part)) AS constraint_column_prefixes,
    rc.update_rule as update_rule,
    rc.delete_rule as delete_rule,
    '' AS check_clause
FROM
    information_schema.table_constraints AS tc
LEFT JOIN information_schema.key_column_usage AS kcu
    ON tc.constraint_name = kcu.constraint_name
    AND tc.table_schema = kcu.table_schema
    AND tc.table_name = kcu.table_name
LEFT JOIN information_schema.columns as c
    ON c.table_schema = kcu.table_schema
    AND c.table_name = kcu.table_name
    AND c.column_name = kcu.column_name
LEFT JOIN information_schema.referential_constraints as rc
	ON rc.constraint_schema = tc.table_schema
	AND rc.table_name = tc.table_name
	AND rc.constraint_name = tc.constraint_name
LEFT JOIN information_schema.statistics as s
	ON s.table_schema = tc.table_schema
	AND s.table_name = tc.table_name
	AND s.index_name = tc.constraint_name
	AND s.column_name = kcu.column_name
`

const mysql57TableConstraintsGroupBy = `GROUP BY
    tc.table_schema,
    tc.table_name,
    tc.constraint_name,
    tc.constraint_type,
    kcu.referenced_table_schema,
    kcu.referenced_table_name,
    rc.update_rule,
    rc.delete_rule
`

const mysql57GetTableConstraints = `-- name: GetTableConstraints :many
` + mysql57TableConstraintsFrom + `WHERE
    tc.table_schema = ?
    AND tc.table_name IN (/*SLICE:tables*/?)
` + mysql57TableConstraintsGroupBy

const mysql57GetTableConstraintsBySchemas = `-- name: GetTableConstraintsBySchemas :many
` + mysql57TableConstraintsFrom + `WHERE
    tc.table_schema IN (/*SLICE:schemas*/?)
` + mysql57TableConstraintsGroupBy

func (q *mysql57Querier) GetTableConstraints(
	ctx context.Context,
	db mysql_queries.DBTX,
	arg *mysql_queries.GetTableConstraintsParams,
) ([]*mysql_queries.GetTableConstraintsRow, error) {
	query, queryParams := expandSlice(mysql57GetTableConstraints, "tables", []any{arg.Schema}, arg.Tables)
	rows, err := db.QueryContext(ctx, query, queryParams...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*mysql_queries.GetTableConstraintsRow
	for rows.Next() {
		var i mysql_queries.GetTableConstraintsRow
		if err := rows.Scan(
			&i.SchemaName,
			&i.TableName,
			&i.ConstraintColumns,
			&i.NotNullable,
			&i.ConstraintName,
			&i.ConstraintType,
			&i.ReferencedSchemaName,
			&i.ReferencedTableName,
			&i.ReferencedColumnNames,
			&i.ConstraintColumnPrefixes,
			&i.UpdateRule,
			&i.DeleteRule,
			&i.CheckClause,
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

func (q *mysql57Querier) GetTableConstraintsBySchemas(
	ctx context.Context,
	db mysql_queries.DBTX,
	schemas []string,
) ([]*mysql_queries.GetTableConstraintsBySchemasRow, error) {
	query, queryParams := expandSlice(mysql57GetTableConstraintsBySchemas, "schemas", nil, schemas)
	rows, err := db.QueryContext(ctx, query, queryParams...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*mysql_queries.GetTableConstraintsBySchemasRow
	for rows.Next() {
		var i mysql_queries.GetTableConstraintsBySchemasRow
		if err := rows.Scan(
			&i.SchemaName,
			&i.TableName,
			&i.ConstraintColumns,
			&i.NotNullable,
			&i.ConstraintName,
			&i.ConstraintType,
			&i.ReferencedSchemaName,
			&i.ReferencedTableName,
			&i.ReferencedColumnNames,
			&i.ConstraintColumnPrefixes,
			&i.UpdateRule,
			&i.DeleteRule,
			&i.CheckClause,
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

func (q *mysql57Querier) GetDatabaseSchema(
	ctx context.Context,
	db mysql_queries.DBTX,
) ([]*mysql_queries.GetDatabaseSchemaRow, error) {
	rows, err := q.Querier.GetDatabaseSchema(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		normalizeMysql57Default(row.ColumnDefault, row.DataType, &row.Extra)
	}
	return rows, nil
}

func (q *mysql57Querier) GetDatabaseTableSchemasBySchemasAndTables(
	ctx context.Context,
	db mysql_queries.DBTX,
	arg *mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesParams,
) ([]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow, error) {
	rows, err := q.Querier.GetDatabaseTableSchemasBySchemasAndTables(ctx, db, arg)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		normalizeMysql57Default(row.ColumnDefault, row.ColumnType, &row.IdentityGeneration)
	}
	return rows, nil
}

// mysql57CurrentTimestamp is how 5.7 reports a CURRENT_TIMESTAMP default, with its
// fractional seconds precision when it has one. NOW(), LOCALTIME, LOCALTIMESTAMP and the
// other synonyms are all reported this way.
var mysql57CurrentTimestamp = regexp.MustCompile(`(?i)^CURRENT_TIMESTAMP(\([0-6]?\))?$`)

// mysql57OnUpdate is the marker 5.7 puts in EXTRA for ON UPDATE CURRENT_TIMESTAMP[(n)].
var mysql57OnUpdate = regexp.MustCompile(`(?i)\s*on update CURRENT_TIMESTAMP(\([0-6]?\))?`)

// isTemporalColumnType says whether a column type, as DATA_TYPE or COLUMN_TYPE gives it,
// is one of the two that 5.7 lets default to CURRENT_TIMESTAMP.
func isTemporalColumnType(columnType string) bool {
	columnType = strings.ToLower(strings.TrimSpace(columnType))
	return strings.HasPrefix(columnType, "timestamp") || strings.HasPrefix(columnType, "datetime")
}

// normalizeMysql57Default rewrites the EXTRA marker of a 5.7 column in place so that the
// manager writes its default in a form 5.7 accepts (see mysql57Querier):
//   - CURRENT_TIMESTAMP[(n)] on a TIMESTAMP or DATETIME column: EXTRA becomes NULL, and the
//     default is written as it is. The type matters: on any other column, CURRENT_TIMESTAMP
//     is a plain string default, which 5.7 reports the same way.
//   - any other default: the "on update" marker is dropped from EXTRA, which leaves it
//     empty, and the default is quoted as a literal ('0000-00-00 00:00:00' stays one).
//
// A column without a default is left alone.
func normalizeMysql57Default(columnDefault any, columnType string, extra *sql.NullString) {
	def, ok := columnDefault.([]uint8)
	if !ok || len(def) == 0 || !extra.Valid {
		return
	}
	if isTemporalColumnType(columnType) && mysql57CurrentTimestamp.Match(def) {
		*extra = sql.NullString{}
		return
	}
	extra.String = strings.TrimSpace(mysql57OnUpdate.ReplaceAllString(extra.String, ""))
}
