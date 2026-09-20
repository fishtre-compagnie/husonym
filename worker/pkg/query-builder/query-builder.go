package querybuilder

import (
	"fmt"
	"slices"
	"strings"

	"github.com/doug-martin/goqu/v9"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"

	// import the dialect
	_ "github.com/doug-martin/goqu/v9/dialect/mysql"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	_ "github.com/doug-martin/goqu/v9/dialect/sqlserver"
	"github.com/doug-martin/goqu/v9/exp"
)

type SubsetReferenceKey struct {
	Table         string
	Columns       []string
	OriginalTable *string
}
type SubsetColumnConstraint struct {
	Columns     []string
	NotNullable []bool
	ForeignKey  *SubsetReferenceKey
}

func getGoquDialect(driver string) goqu.DialectWrapper {
	if driver == sqlmanager_shared.PostgresDriver {
		return goqu.Dialect(sqlmanager_shared.GoquPostgresDriver)
	}
	return goqu.Dialect(driver)
}

func BuildSelectQuery(
	driver, table string,
	columns []string,
	whereClause *string,
) (string, error) {
	builder := getGoquDialect(driver)
	sqltable := goqu.I(table)

	selectColumns := make([]any, len(columns))
	for i, col := range columns {
		selectColumns[i] = col
	}
	query := builder.From(sqltable).Select(selectColumns...)

	if whereClause != nil && *whereClause != "" {
		query = query.Where(goqu.L(*whereClause))
	}
	sql, _, err := query.ToSQL()
	if err != nil {
		return "", err
	}

	return formatSqlQuery(sql), nil
}

func formatSqlQuery(sql string) string {
	return fmt.Sprintf("%s;", sql)
}

func BuildSelectLimitQuery(
	driver, table string,
	limit uint,
) (string, error) {
	builder := getGoquDialect(driver)
	sqltable := goqu.I(table)
	sql, _, err := builder.From((sqltable)).Limit(limit).ToSQL()
	if err != nil {
		return "", err
	}
	return sql, nil
}

// sampleWindowSize borne le nombre de lignes sur lesquelles porte le tirage
// aléatoire. Assez large pour que l'échantillon reste varié, assez petit pour que
// le tri soit gratuit.
const sampleWindowSize = 1000

// BuildSampledSelectLimitQuery construit une requête d'échantillonnage aléatoire.
//
// The draw happens over a bounded WINDOW, not the whole table. An
// `ORDER BY RAND() LIMIT 20` applied straight to the table forces the database to
// read every row and sort all of them to return 20: the cost grows with the table,
// unrelated to the requested sample size. Measured on a production MySQL table, the
// query went past 30 s, the client dropped the link and the PII scan failed with an
// HTTP 500.
//
// Compromis assumé : l'échantillon n'est plus uniforme sur l'ensemble de la table,
// il est tiré au hasard parmi les premières sampleWindowSize lignes. Pour
// reconnaître la NATURE d'une colonne — l'usage réel de cette fonction — la
// représentativité statistique n'apporte rien ; un échantillon obtenable en
// quelques millisecondes, si.
func BuildSampledSelectLimitQuery(
	driver, table string, limit uint,
) (string, error) {
	var randStmt string
	switch driver {
	case sqlmanager_shared.GoquPostgresDriver:
		randStmt = "RANDOM()"
	case sqlmanager_shared.MysqlDriver:
		randStmt = "RAND()"
	case sqlmanager_shared.MssqlDriver:
		randStmt = "NEWID()"
	}

	builder := getGoquDialect(driver)
	sqltable := goqu.I(table)

	// Fenêtre lue sans tri : le SGBD s'arrête dès qu'il a ses lignes.
	window := builder.From(sqltable).Limit(sampleWindowSize).As("husonym_sample")

	sql, _, err := builder.
		From(window).
		Order(goqu.L(randStmt).Asc()).
		Limit(limit).
		ToSQL()
	if err != nil {
		return "", err
	}
	return sql, nil
}

func BuildInsertQuery(
	driver, schema, table string,
	records []goqu.Record,
	onConflictDoNothing *bool,
) (sql string, args []any, err error) {
	builder := getGoquDialect(driver)
	sqltable := goqu.S(schema).Table(table)
	insert := builder.Insert(sqltable).Prepared(true).Rows(records)
	// adds on conflict do nothing to insert query
	// len(records[0]) > 0: the conflict clause names a column of the record, and a record
	// without one gives none.
	mysqlDoNothing := *onConflictDoNothing && driver == sqlmanager_shared.MysqlDriver &&
		len(records) > 0 && len(records[0]) > 0
	switch {
	case mysqlDoNothing:
		// MySQL spells "do nothing" INSERT IGNORE, which skips rows already there but
		// also downgrades every other error to a warning: values too long are truncated,
		// NULL in a NOT NULL column becomes the implicit default, invalid ENUM values become
		// ''. Assigning a column to itself on a duplicate key skips the row and nothing else.
		column := firstColumn(records[0])
		insert = insert.OnConflict(goqu.DoUpdate("", goqu.Record{
			column: exp.NewIdentifierExpression("", "", column),
		}))
	case *onConflictDoNothing:
		insert = insert.OnConflict(goqu.DoNothing())
	}

	query, args, err := insert.ToSQL()
	if err != nil {
		// check if it's a goqu encoding error and sanitize it
		if strings.Contains(err.Error(), "goqu_encode_error") {
			return "", nil, fmt.Errorf("goqu_encode_error: Unable to encode value")
		}
		return "", nil, err
	}
	if mysqlDoNothing {
		// goqu writes every MySQL conflict clause on top of INSERT IGNORE
		query = strings.Replace(query, "INSERT IGNORE INTO", "INSERT INTO", 1)
	}
	return query, args, nil
}

// firstColumn returns the first column of a record in name order, so the same rows
// always build the same query.
func firstColumn(record goqu.Record) string {
	columns := make([]string, 0, len(record))
	for column := range record {
		columns = append(columns, column)
	}
	slices.Sort(columns)
	return columns[0]
}

func BuildUpdateQuery(
	driver, schema, table string,
	insertColumns []string,
	whereColumns []string,
	columnValueMap map[string]any,
) (string, error) {
	builder := getGoquDialect(driver)
	sqltable := goqu.S(schema).Table(table)

	updateRecord := goqu.Record{}
	for _, col := range insertColumns {
		val := columnValueMap[col]
		updateRecord[col] = val
	}

	where := []exp.Expression{}
	for _, col := range whereColumns {
		val := columnValueMap[col]
		where = append(where, goqu.Ex{col: val})
	}

	update := builder.Update(sqltable).
		Set(updateRecord).
		Where(where...)

	query, _, err := update.ToSQL()
	if err != nil {
		// check if it's a goqu encoding error and sanitize it
		if strings.Contains(err.Error(), "goqu_encode_error") {
			return "", fmt.Errorf("goqu_encode_error: Unable to encode value")
		}
		return "", err
	}
	return query, nil
}

func BuildTruncateQuery(
	driver, table string,
) (string, error) {
	builder := getGoquDialect(driver)
	sqltable := goqu.I(table)
	truncate := builder.Truncate(sqltable)
	query, _, err := truncate.ToSQL()
	if err != nil {
		return "", err
	}
	return query, nil
}

func GetGoquDriverFromConnection(connection *mgmtv1alpha1.Connection) (string, error) {
	switch connection.GetConnectionConfig().GetConfig().(type) {
	case *mgmtv1alpha1.ConnectionConfig_PgConfig:
		return sqlmanager_shared.GoquPostgresDriver, nil
	case *mgmtv1alpha1.ConnectionConfig_MysqlConfig:
		return sqlmanager_shared.MysqlDriver, nil
	case *mgmtv1alpha1.ConnectionConfig_MssqlConfig:
		return sqlmanager_shared.MssqlDriver, nil
	default:
		return "", fmt.Errorf("unsupported connection type: %T for goqu", connection.GetConnectionConfig().GetConfig())
	}
}
