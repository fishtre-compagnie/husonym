package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"sync"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"golang.org/x/sync/errgroup"
)

// The generated query that reads the columns of a table (GetDatabaseTableSchemasBySchemasAndTables)
// leaves out what a column definition holds besides its type, nullability, default, generation
// and comment, and gives the empty string both for a column without a default and for one that
// defaults to the empty string. getColumnAttributes reads the rest, on MySQL 5.7, 8 and MariaDB
// alike, for the statements that add or modify a column, and for a table created from the
// catalog (see show-create.go): its character set and collation, its ON UPDATE clause, and
// whether its default is the empty string.
const getColumnAttributes = `SELECT
    c.TABLE_SCHEMA,
    c.TABLE_NAME,
    c.COLUMN_NAME,
    c.DATA_TYPE,
    c.CHARACTER_SET_NAME,
    c.COLLATION_NAME,
    c.EXTRA,
    CASE WHEN c.COLUMN_DEFAULT IS NOT NULL AND LENGTH(c.COLUMN_DEFAULT) = 0 THEN 1 ELSE 0 END
FROM information_schema.COLUMNS AS c
WHERE c.TABLE_SCHEMA = ? AND c.TABLE_NAME IN (/*SLICE:tables*/?)
`

// columnKey names a column of a table.
type columnKey struct {
	schema, table, column string
}

// textDataTypes are the types a character set and a collation are written for.
var textDataTypes = map[string]bool{
	"char": true, "varchar": true, "tinytext": true, "text": true, "mediumtext": true,
	"longtext": true, "enum": true, "set": true,
}

// onUpdateClause reads the ON UPDATE clause EXTRA reports: "on update CURRENT_TIMESTAMP" on 5.7,
// "DEFAULT_GENERATED on update CURRENT_TIMESTAMP(3)" on 8, "on update current_timestamp()" on
// MariaDB.
var onUpdateClause = regexp.MustCompile(
	`(?i)\bon update ((?:current_timestamp|now|localtime|localtimestamp)(?:\(\d*\))?)`,
)

// onUpdateValue is the form of the value of an ON UPDATE clause.
var onUpdateValue = regexp.MustCompile(`^(?i)(?:current_timestamp|now|localtime|localtimestamp)(?:\(\d*\))?$`)

// charsetName is the form of a character set and of a collation name.
var charsetName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// getColumnAttributes reads the attributes of the columns of the tables, by schema, keyed by the
// names the server answers with. A manager without a pool reads nothing.
func (m *MysqlManager) getColumnAttributes(
	ctx context.Context,
	schemaset map[string][]string,
) (map[columnKey]*sqlmanager_shared.MysqlColumn, error) {
	attributes := map[columnKey]*sqlmanager_shared.MysqlColumn{}
	if m.pool == nil {
		return attributes, nil
	}
	errgrp, errctx := errgroup.WithContext(ctx)
	errgrp.SetLimit(5)
	var mu sync.Mutex
	for schema, tables := range schemaset {
		errgrp.Go(func() error {
			query, params := expandSlice(getColumnAttributes, "tables", []any{schema}, tables)
			rows, err := m.pool.QueryContext(errctx, query, params...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var (
					key                columnKey
					dataType, extra    string
					charset, collation sql.NullString
					emptyDefault       int64
				)
				if err := rows.Scan(
					&key.schema, &key.table, &key.column, &dataType, &charset, &collation, &extra, &emptyDefault,
				); err != nil {
					return err
				}
				column := &sqlmanager_shared.MysqlColumn{EmptyStringDefault: emptyDefault == 1}
				if textDataTypes[strings.ToLower(dataType)] && charset.Valid && collation.Valid &&
					charsetName.MatchString(charset.String) && charsetName.MatchString(collation.String) {
					column.CharacterSet = &charset.String
					column.Collation = &collation.String
				}
				if match := onUpdateClause.FindStringSubmatch(extra); match != nil {
					column.OnUpdate = &match[1]
				}
				mu.Lock()
				attributes[key] = column
				mu.Unlock()
			}
			if err := rows.Close(); err != nil {
				return err
			}
			return rows.Err()
		})
	}
	if err := errgrp.Wait(); err != nil {
		return nil, err
	}
	return attributes, nil
}

// columnAttributeClauses writes what a column definition holds besides its type, nullability,
// default and comment: CHARACTER SET and COLLATE, which follow the type, and ON UPDATE, which
// follows the default.
func columnAttributeClauses(column *sqlmanager_shared.MysqlColumn) (afterType, afterDefault []string) {
	if column == nil {
		return nil, nil
	}
	if column.CharacterSet != nil && charsetName.MatchString(*column.CharacterSet) {
		afterType = append(afterType, "CHARACTER SET "+*column.CharacterSet)
		if column.Collation != nil && charsetName.MatchString(*column.Collation) {
			afterType = append(afterType, "COLLATE "+*column.Collation)
		}
	}
	if column.OnUpdate != nil && onUpdateValue.MatchString(*column.OnUpdate) {
		afterDefault = append(afterDefault, "ON UPDATE "+*column.OnUpdate)
	}
	return afterType, afterDefault
}
