package querybuilder

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/doug-martin/goqu/v9"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"

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
	driver, schema, table string,
	columns []string,
	whereClause *string,
) (string, error) {
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return "", err
	}
	if err := checkColumns(columns); err != nil {
		return "", err
	}
	builder := getGoquDialect(driver)

	selectColumns := make([]any, len(columns))
	for i, col := range columns {
		selectColumns[i] = d.Col(col)
	}
	query := builder.From(d.Table(schema, table)).Select(selectColumns...)

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
	driver, schema, table string,
	limit uint,
) (string, error) {
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return "", err
	}
	sql, _, err := getGoquDialect(driver).From(d.Table(schema, table)).Limit(limit).ToSQL()
	if err != nil {
		return "", err
	}
	return sql, nil
}

// SampleWindowSize is the number of rows a random draw is made over. It is wide enough
// for the sample to stay varied and small enough for the random order to cost next to
// nothing.
const SampleWindowSize = 1000

// ColumnFilter makes a sampling query select one column and keep the filled values of
// it only. A nil filter leaves a sampling query as it is: whole rows.
type ColumnFilter struct {
	// Column is the one column selected. Its values are never NULL.
	Column string
	// NonEmpty also leaves out the empty string. It is for text columns only.
	NonEmpty bool
}

// check refuses a column name that no engine takes.
func (f *ColumnFilter) check() error {
	if f == nil {
		return nil
	}
	return checkColumns([]string{f.Column})
}

// conditions gives the conditions that keep the filled values of the column.
func (f *ColumnFilter) conditions(d sqlident.Dialect) []exp.Expression {
	col := d.Col(f.Column)
	conditions := []exp.Expression{col.IsNotNull()}
	if f.NonEmpty {
		conditions = append(conditions, goqu.L("(? <> '')", col))
	}
	return conditions
}

// restrict makes a select of the table read the column of the filter, filled values
// only. A nil filter gives the select as it is.
func (f *ColumnFilter) restrict(d sqlident.Dialect, rows *goqu.SelectDataset) *goqu.SelectDataset {
	if f == nil {
		return rows
	}
	return rows.Select(d.Col(f.Column)).Where(f.conditions(d)...)
}

// BuildSampledSelectLimitQuery builds a query that returns a random sample of a table.
// The draw is made over a bounded window, the first SampleWindowSize rows of the table,
// not over the whole table. It is the query that answers when a draw spread across the
// table is not possible. With a filter, the window is made of the first SampleWindowSize
// filled values of the column.
//
// The cost is bounded by the window. An `ORDER BY RAND() LIMIT n` applied straight to
// the table makes the database read every row and sort all of them to return n, so its
// cost grows with the table whatever the size of the sample. Here the window is read
// without a sort, the database stops as soon as it has its rows, and only those
// SampleWindowSize rows go through the random order.
//
// The sample is therefore not uniform over the table.
func BuildSampledSelectLimitQuery(
	driver, schema, table string, limit uint,
	filter *ColumnFilter,
) (string, error) {
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return "", err
	}
	if err := filter.check(); err != nil {
		return "", err
	}

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

	// The window is read without a sort: the database stops as soon as it has its rows.
	window := filter.restrict(d, builder.From(d.Table(schema, table))).
		Limit(SampleWindowSize).As("husonym_sample")

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
	// SampleSliceRows is the most rows read from each key slice.
	SampleSliceRows = 100
	// SampleSlicesMinRows is the least number of rows the key slices must hold together
	// for a sample to be drawn from them.
	SampleSlicesMinRows = SampleSlices * SampleSliceRows / 2
	// SampleRowsBound is the most rows a PostgreSQL table sample may hand to the random
	// order, whatever the share of pages it asks for.
	SampleRowsBound = 4 * SampleWindowSize
	// SampleMinPages is the least number of pages a table sample asks for. Pages of
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
// whole table. The database reads the pages expected to hold SampleWindowSize rows, or
// SampleMinPages pages when that is more, each page being taken on its own; a table of
// fewer pages is read whole. The random order only applies to that sample.
//
// PostgreSQL bounds the rows it hands to the random order at SampleRowsBound, for a size
// that is far from the truth. So that the bound does not keep the first pages only when
// the pages read hold more rows than the window, each row is first kept with the
// probability that leaves about SampleWindowSize of them: the rows that reach the
// random order come from every page read. SQL Server orders every row of the pages it
// reads.
//
// With a filter, the pages are the same and only the filled values of the column are
// kept. The rows are not thinned first: a thinning made before the filter would leave a
// sparse column with a fraction of its values.
//
// It supports PostgreSQL and SQL Server. ok is false when the driver has no table
// sample, when the size is unknown (no row or no page) and when the table has no more
// rows than SampleWindowSize: the window query serves it.
func BuildTableSampleQuery(
	driver, schema, table string,
	size TableSize,
	limit uint,
	filter *ColumnFilter,
) (sql string, ok bool, err error) {
	if err := checkTable(schema, table); err != nil {
		return "", false, err
	}
	if err := filter.check(); err != nil {
		return "", false, err
	}
	if size.Rows <= SampleWindowSize || size.Pages <= 0 {
		return "", false, nil
	}
	if driver != sqlmanager_shared.GoquPostgresDriver && driver != sqlmanager_shared.MssqlDriver {
		return "", false, nil
	}
	d, err := sqlident.ForDriver(driver)
	if err != nil {
		return "", false, err
	}
	percent, keep := tableSampleShare(size)
	sqltable := d.Table(schema, table)

	builder := getGoquDialect(driver)
	var inner *goqu.SelectDataset
	var randStmt string
	if driver == sqlmanager_shared.GoquPostgresDriver {
		inner = filter.restrict(d, builder.From(goqu.L("? TABLESAMPLE SYSTEM (?)", sqltable, percent)))
		if keep < 1 && filter == nil {
			inner = inner.Where(goqu.L("RANDOM() < ?", keep))
		}
		inner = inner.Limit(SampleRowsBound)
		randStmt = "RANDOM()"
	} else {
		// No bound here: a TOP on a table sample keeps the first pages read. No thinning
		// either: a random filter that names no column is computed once for the query.
		inner = filter.restrict(d, builder.From(goqu.L("? TABLESAMPLE (? PERCENT)", sqltable, percent)))
		randStmt = "NEWID()"
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
// rows to keep. The pages are those expected to hold SampleWindowSize rows, or
// SampleMinPages pages when that is more; the percentage is rounded to four decimals,
// stays above zero and does not exceed 100. keep is 1 when those pages are not expected
// to hold more than the window. size must hold rows and pages.
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
// on the key, so the cost does not grow with the table. With a filter, a slice holds the
// filled values of the column found in its range, in key order.
func BuildKeySlicesSampleQuery(
	driver, schema, table, keyColumn string,
	ranges []KeyRange,
	limit uint,
	filter *ColumnFilter,
) (string, error) {
	union, err := keySlices(driver, schema, table, keyColumn, ranges, false, filter)
	if err != nil {
		return "", err
	}
	sql, _, err := getGoquDialect(driver).
		From(union.As("husonym_sample")).
		Order(goqu.L("RAND()").Asc()).
		Limit(limit).
		ToSQL()
	if err != nil {
		return "", err
	}
	return sql, nil
}

// BuildKeySlicesCountQuery builds a MySQL query that counts the rows the slices of
// BuildKeySlicesSampleQuery hold for the same ranges. Only the key column is read, so
// the count is answered from the key: SampleSliceRows entries of it at most for each
// range.
func BuildKeySlicesCountQuery(
	driver, schema, table, keyColumn string,
	ranges []KeyRange,
) (string, error) {
	union, err := keySlices(driver, schema, table, keyColumn, ranges, true, nil)
	if err != nil {
		return "", err
	}
	sql, _, err := getGoquDialect(driver).
		From(union.As("husonym_sample")).
		Select(goqu.COUNT(goqu.Star())).
		ToSQL()
	if err != nil {
		return "", err
	}
	return sql, nil
}

// keySlices is the union of the slices of a table: for each range, its first
// SampleSliceRows rows in key order. A slice holds the whole row, or the key column
// alone when keyOnly is set, or the filled values of the column of the filter.
func keySlices(
	driver, schema, table, keyColumn string,
	ranges []KeyRange,
	keyOnly bool,
	filter *ColumnFilter,
) (*goqu.SelectDataset, error) {
	if len(ranges) == 0 {
		return nil, errors.New("at least one key range is required")
	}
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return nil, err
	}
	if err := checkColumns([]string{keyColumn}); err != nil {
		return nil, err
	}
	if err := filter.check(); err != nil {
		return nil, err
	}
	builder := getGoquDialect(driver)
	sqltable := d.Table(schema, table)
	key := d.Col(keyColumn)

	slice := func(r KeyRange) *goqu.SelectDataset {
		rows := builder.From(sqltable)
		if keyOnly {
			rows = rows.Select(key)
		}
		return filter.restrict(d, rows.Where(key.Gte(r.From), key.Lte(r.To))).
			Order(key.Asc()).
			Limit(SampleSliceRows)
	}
	union := slice(ranges[0])
	for _, r := range ranges[1:] {
		union = union.UnionAll(slice(r))
	}
	return union, nil
}

func BuildInsertQuery(
	driver, schema, table string,
	records []goqu.Record,
	onConflictDoNothing *bool,
) (sql string, args []any, err error) {
	insert, d, err := insertInto(driver, schema, table, records)
	if err != nil {
		return "", nil, err
	}
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
		column := d.Col(sortedColumns(records[0])[0])
		insert = insert.OnConflict(goqu.DoUpdate("", column.Set(column)))
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

func BuildUpdateQuery(
	driver, schema, table string,
	insertColumns []string,
	whereColumns []string,
	columnValueMap map[string]any,
) (string, error) {
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return "", err
	}
	if err := checkColumns(insertColumns); err != nil {
		return "", err
	}
	if err := checkColumns(whereColumns); err != nil {
		return "", err
	}
	value := func(column string) any { return columnValueMap[column] }

	where := []exp.Expression{}
	for _, col := range whereColumns {
		where = append(where, d.Col(col).Eq(value(col)))
	}

	update := getGoquDialect(driver).Update(d.Table(schema, table)).
		Set(setColumns(d, insertColumns, value)).
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
	driver, schema, table string,
) (string, error) {
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return "", err
	}
	query, _, err := getGoquDialect(driver).Truncate(d.Table(schema, table)).ToSQL()
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
