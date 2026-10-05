package querybuilder

import (
	"errors"
	"fmt"
	"math"
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

// SampleWindowSize borne le nombre de lignes sur lesquelles porte le tirage
// aléatoire. Assez large pour que l'échantillon reste varié, assez petit pour que
// le tri soit gratuit.
const SampleWindowSize = 1000

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
// il est tiré au hasard parmi les premières SampleWindowSize lignes. Pour
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
	window := builder.From(sqltable).Limit(SampleWindowSize).As("husonym_sample")

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

const (
	// SampleSlices is the number of key slices a MySQL sample is drawn from.
	SampleSlices = 10
	// SampleSliceRows is the number of rows read from each key slice.
	SampleSliceRows = 100
	// SampleRowsBound is the most rows a PostgreSQL table sample may hand to the random
	// order, whatever the share of pages it asks for.
	SampleRowsBound = 4 * SampleWindowSize
	// SampleMinPages is the least number of pages a table sample is drawn from. Pages of
	// narrow rows hold the window in a handful of pages, which are a handful of places
	// of the table.
	SampleMinPages = 50
)

// KeyRange is a range of key values, both ends included.
type KeyRange struct {
	From, To int64
}

// TableSize is the size of a table as its catalog tells it: the rows it holds and the
// pages they are stored on.
type TableSize struct {
	Rows, Pages int64
}

// BuildTableSampleQuery builds a query that draws rows from pages spread across the
// whole table. The table is never scanned in full: the database reads the pages
// expected to hold SampleWindowSize rows, and at least SampleMinPages of them, and the
// random order only applies to that sample.
//
// PostgreSQL bounds the rows it hands to the random order at SampleRowsBound, for a size
// that is far from the truth. So that the bound does not keep the first pages only when
// the pages read hold more rows than the window, each row is first kept with the
// probability that leaves about SampleWindowSize of them: the rows that reach the
// random order come from every page read. SQL Server orders every row of the pages it
// reads.
//
// It supports PostgreSQL and SQL Server. ok is false when the driver has no table
// sample, when the size is unknown (no row or no page) and when the table has no more
// rows than SampleWindowSize: the window query serves it.
func BuildTableSampleQuery(
	driver, table string,
	size TableSize,
	limit uint,
) (sql string, ok bool, err error) {
	if size.Rows <= SampleWindowSize || size.Pages <= 0 {
		return "", false, nil
	}
	percent, keep := tableSampleShare(size)

	builder := getGoquDialect(driver)
	var inner *goqu.SelectDataset
	var randStmt string
	switch driver {
	case sqlmanager_shared.PostgresDriver, sqlmanager_shared.GoquPostgresDriver:
		inner = builder.From(goqu.L("? TABLESAMPLE SYSTEM (?)", goqu.I(table), percent))
		if keep < 1 {
			inner = inner.Where(goqu.L("RANDOM() < ?", keep))
		}
		inner = inner.Limit(SampleRowsBound)
		randStmt = "RANDOM()"
	case sqlmanager_shared.MssqlDriver:
		// No bound here: a TOP on a table sample keeps the first pages read. No thinning
		// either: a random filter that names no column is computed once for the query.
		inner = builder.From(goqu.L("? TABLESAMPLE (? PERCENT)", goqu.I(table), percent))
		randStmt = "NEWID()"
	default:
		return "", false, nil
	}

	sql, _, err = builder.
		From(inner.As("husonym_sample")).
		Order(goqu.L(randStmt).Asc()).
		Limit(limit).
		ToSQL()
	if err != nil {
		return "", false, err
	}
	return sql, true, nil
}

// tableSampleShare gives the share of pages to read, in percent, and the share of their
// rows to keep. The pages are those expected to hold SampleWindowSize rows, and at least
// SampleMinPages; the percentage is rounded to four decimals, stays above zero and does
// not exceed 100. keep is 1 when those pages are not expected to hold more than the
// window. size must hold rows and pages.
func tableSampleShare(size TableSize) (percent, keep float64) {
	share := math.Max(
		float64(SampleWindowSize)/float64(size.Rows),
		float64(SampleMinPages)/float64(size.Pages),
	)
	share = math.Min(1, share)
	percent = math.Max(0.0001, roundTo4(100*share))
	keep = math.Min(1, roundTo4(float64(SampleWindowSize)/(share*float64(size.Rows))))
	return percent, keep
}

func roundTo4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// BuildKeySlicesSampleQuery builds a MySQL query that reads up to SampleSliceRows rows
// in key order from each range, and draws limit rows at random from their union. The
// ranges must not overlap, so no row comes twice. Each slice is a bounded range read
// on the key, so the cost does not grow with the table.
func BuildKeySlicesSampleQuery(
	driver, table, keyColumn string,
	ranges []KeyRange,
	limit uint,
) (string, error) {
	if len(ranges) == 0 {
		return "", errors.New("at least one key range is required")
	}
	builder := getGoquDialect(driver)
	sqltable := goqu.I(table)
	key := goqu.I(keyColumn)

	slice := func(r KeyRange) *goqu.SelectDataset {
		return builder.From(sqltable).
			Where(key.Gte(r.From), key.Lte(r.To)).
			Order(key.Asc()).
			Limit(SampleSliceRows)
	}
	union := slice(ranges[0])
	for _, r := range ranges[1:] {
		union = union.UnionAll(slice(r))
	}

	sql, _, err := builder.
		From(union.As("husonym_sample")).
		Order(goqu.L("RAND()").Asc()).
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
