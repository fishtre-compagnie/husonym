package sqlmanager_mysql

import (
	"fmt"
	"strings"

	"github.com/doug-martin/goqu/v9"
	// The identifiers handed to goqu are prepared for the quote character of this dialect.
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
)

// my writes the names and the string values of the statements of this package.
const my = sqlident.MySQL

// checkTableName refuses a schema or a table name that no engine takes. A table may have no
// schema: it is then written alone.
func checkTableName(schema, table string) error {
	if schema != "" {
		if err := sqlident.Check(schema); err != nil {
			return fmt.Errorf("schema name: %w", err)
		}
	}
	if err := sqlident.Check(table); err != nil {
		return fmt.Errorf("table name: %w", err)
	}
	return nil
}

func checkNames(kind string, names ...string) error {
	for _, name := range names {
		if err := sqlident.Check(name); err != nil {
			return fmt.Errorf("%s name: %w", kind, err)
		}
	}
	return nil
}

// checkQualifiedTable refuses the names of a table that a statement writes with its schema.
func checkQualifiedTable(schema, table string) error {
	if err := checkNames("schema", schema); err != nil {
		return err
	}
	return checkNames("table", table)
}

func buildCreateSchemaStatement(schema string) (string, error) {
	if err := checkNames("schema", schema); err != nil {
		return "", err
	}
	return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", my.Quote(schema)), nil
}

// buildCreateTableStatement takes the columns as buildTableColForCreate writes them.
func buildCreateTableStatement(schema, table string, columns []string) (string, error) {
	if err := checkQualifiedTable(schema, table); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (%s);",
		my.Qualified(schema, table),
		strings.Join(columns, ", "),
	), nil
}

// buildColumnComment writes the COMMENT clause of a column, or nothing.
//
// The clause takes a quoted string and no other form of literal. A quoted string holding a
// backslash does not read the same under every sql_mode, and the statement is built without
// knowing the one of the session that runs it: such a comment is left out.
func buildColumnComment(comment *string) string {
	if comment == nil || *comment == "" || strings.Contains(*comment, `\`) {
		return ""
	}
	return "COMMENT " + my.Literal(*comment)
}

// buildTableRowCountSql counts the rows of a table. The where clause is SQL written by the
// caller and goes into the statement as it is.
func buildTableRowCountSql(schema, table string, whereClause *string) (string, error) {
	if err := checkTableName(schema, table); err != nil {
		return "", err
	}
	query := goqu.Dialect(sqlmanager_shared.MysqlDriver).
		From(my.Table(schema, table)).
		Select(goqu.COUNT("*"))
	if whereClause != nil && *whereClause != "" {
		query = query.Where(goqu.L(*whereClause))
	}
	sqlStr, _, err := query.ToSQL()
	if err != nil {
		return "", err
	}
	return sqlStr, nil
}
