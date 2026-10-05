package connectiondata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"

	sqlmanager_mysql "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mysql"
	sqlmanager_postgres "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/postgres"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	querybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/query-builder"
)

const (
	postgresEstimateQuery = `SELECT reltuples FROM pg_class WHERE oid = to_regclass($1)`

	// A usable key is the only column of the primary index. Unlike COLUMN_KEY, the
	// STATISTICS view does not report a unique index on a NOT NULL column as primary.
	mysqlPrimaryKeyQuery = `SELECT s.COLUMN_NAME, c.DATA_TYPE, c.COLUMN_TYPE
FROM information_schema.STATISTICS s
JOIN information_schema.COLUMNS c
  ON c.TABLE_SCHEMA = s.TABLE_SCHEMA AND c.TABLE_NAME = s.TABLE_NAME AND c.COLUMN_NAME = s.COLUMN_NAME
WHERE s.TABLE_SCHEMA = ? AND s.TABLE_NAME = ? AND s.INDEX_NAME = 'PRIMARY'
ORDER BY s.SEQ_IN_INDEX`
)

var mysqlIntegerTypes = map[string]struct{}{
	"tinyint": {}, "smallint": {}, "mediumint": {}, "int": {}, "bigint": {},
}

// sampleQuerier is the part of a database handle that sampling needs.
type sampleQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// recordMapper converts the current row of a result set to a map.
type recordMapper interface {
	MapRecord(record any) (map[string]any, error)
}

// sampleQueryError and sampleMapError tell SampleData which of its error messages applies.
type sampleQueryError struct{ err error }

func (e *sampleQueryError) Error() string { return e.err.Error() }
func (e *sampleQueryError) Unwrap() error { return e.err }

type sampleMapError struct{ err error }

func (e *sampleMapError) Error() string { return e.err.Error() }
func (e *sampleMapError) Unwrap() error { return e.err }

// wrapSampleError gives an error of readSample the message SampleData reports.
func wrapSampleError(err error, schemaTable, driver string) error {
	var mapErr *sampleMapError
	if errors.As(err, &mapErr) {
		return fmt.Errorf(
			"unable to convert row to map for table %s with database type %s: %w",
			schemaTable, driver, mapErr.err,
		)
	}
	var queryErr *sampleQueryError
	if errors.As(err, &queryErr) {
		err = queryErr.err
	}
	return fmt.Errorf("error querying table %s with database type %s: %w", schemaTable, driver, err)
}

// spreadSampleQuery returns a query that draws rows across the whole table, or false
// when the database cannot do it cheaply. It never fails: the reason is logged at debug
// level and the caller reads the window instead. pick draws a value in [lo, hi].
func spreadSampleQuery(
	ctx context.Context,
	db sampleQuerier,
	driver, schema, table string,
	numRows uint,
	pick func(lo, hi int64) int64,
) (string, bool) {
	qualified := sqlmanager_shared.BuildTable(schema, table)
	switch driver {
	case sqlmanager_shared.GoquPostgresDriver:
		estimate, ok := postgresEstimate(ctx, db, schema, table)
		if !ok {
			return "", false
		}
		return tableSampleQuery(ctx, driver, qualified, estimate, numRows)
	case sqlmanager_shared.MssqlDriver:
		return tableSampleQuery(ctx, driver, qualified, 0, numRows)
	case sqlmanager_shared.MysqlDriver:
		return mysqlKeySlicesQuery(ctx, db, schema, table, numRows, pick)
	default:
		return "", false
	}
}

func tableSampleQuery(
	ctx context.Context,
	driver, qualified string,
	estimatedRows int64,
	numRows uint,
) (string, bool) {
	query, ok, err := querybuilder.BuildTableSampleQuery(driver, qualified, estimatedRows, numRows)
	if err != nil {
		slog.DebugContext(ctx, "no spread sample query", "table", qualified, "error", err)
		return "", false
	}
	return query, ok
}

// postgresEstimate reads the row count the planner holds. A negative count (never
// analyzed), a missing relation and a failing query all mean there is no estimate.
func postgresEstimate(ctx context.Context, db sampleQuerier, schema, table string) (int64, bool) {
	name := sqlmanager_postgres.EscapePgColumn(schema) + "." + sqlmanager_postgres.EscapePgColumn(table)
	var reltuples float64
	err := db.QueryRowContext(ctx, postgresEstimateQuery, name).Scan(&reltuples)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.DebugContext(ctx, "row estimate unavailable", "table", name, "error", err)
		}
		return 0, false
	}
	if reltuples <= 0 {
		return 0, false
	}
	return int64(reltuples), true
}

func mysqlKeySlicesQuery(
	ctx context.Context,
	db sampleQuerier,
	schema, table string,
	numRows uint,
	pick func(lo, hi int64) int64,
) (string, bool) {
	qualified := sqlmanager_shared.BuildTable(schema, table)
	key, err := mysqlIntegerKey(ctx, db, schema, table)
	if err != nil {
		slog.DebugContext(ctx, "no spread sample query", "table", qualified, "error", err)
		return "", false
	}

	var lo, hi sql.NullInt64
	bounds := fmt.Sprintf(
		"SELECT MIN(%[1]s), MAX(%[1]s) FROM %s.%s",
		sqlmanager_mysql.EscapeMysqlColumn(key),
		sqlmanager_mysql.EscapeMysqlColumn(schema),
		sqlmanager_mysql.EscapeMysqlColumn(table),
	)
	if err := db.QueryRowContext(ctx, bounds).Scan(&lo, &hi); err != nil {
		slog.DebugContext(ctx, "no spread sample query", "table", qualified, "error", err)
		return "", false
	}
	// An empty table has no bounds. A range no wider than the window is served by it. A
	// negative span is a range too wide for an int64.
	if !lo.Valid || !hi.Valid || hi.Int64-lo.Int64 < querybuilder.SampleWindowSize {
		return "", false
	}

	starts := make([]int64, querybuilder.SampleSlices)
	for i := range starts {
		starts[i] = pick(lo.Int64, hi.Int64)
	}
	query, err := querybuilder.BuildKeySlicesSampleQuery(
		sqlmanager_shared.MysqlDriver, qualified, key, starts, numRows,
	)
	if err != nil {
		slog.DebugContext(ctx, "no spread sample query", "table", qualified, "error", err)
		return "", false
	}
	return query, true
}

// mysqlIntegerKey returns the name of the primary key when it is a single column of an
// integer type.
func mysqlIntegerKey(ctx context.Context, db sampleQuerier, schema, table string) (string, error) {
	rows, err := db.QueryContext(ctx, mysqlPrimaryKeyQuery, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var column, dataType, columnType string
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&column, &dataType, &columnType); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count != 1 {
		return "", fmt.Errorf("the primary key has %d columns, one is needed", count)
	}
	if _, ok := mysqlIntegerTypes[strings.ToLower(dataType)]; !ok {
		return "", fmt.Errorf("the primary key is of type %s, an integer is needed", dataType)
	}
	return column, nil
}

// readSample reads the spread query when there is one. When it fails or returns fewer
// than numRows rows, the window is read as well and the larger result is returned.
// Errors are a *sampleQueryError or a *sampleMapError.
func readSample(
	ctx context.Context,
	db sampleQuerier,
	mapper recordMapper,
	spread string,
	hasSpread bool,
	window string,
	numRows uint,
) ([]map[string]any, error) {
	var spreadRows []map[string]any
	if hasSpread {
		var err error
		spreadRows, err = readRows(ctx, db, mapper, spread, numRows)
		if err != nil {
			slog.DebugContext(ctx, "spread sample query failed, reading the window", "error", err)
			spreadRows = nil
		}
		if uint(len(spreadRows)) >= numRows {
			return spreadRows, nil
		}
	}

	windowRows, err := readRows(ctx, db, mapper, window, numRows)
	if err != nil {
		if len(spreadRows) > 0 {
			return spreadRows, nil
		}
		return nil, err
	}
	if len(spreadRows) > len(windowRows) {
		return spreadRows, nil
	}
	return windowRows, nil
}

func readRows(
	ctx context.Context,
	db sampleQuerier,
	mapper recordMapper,
	query string,
	numRows uint,
) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if husonymdb.IsNoRows(err) {
			return nil, nil
		}
		return nil, &sampleQueryError{err: err}
	}
	defer rows.Close()

	var out []map[string]any
	for uint(len(out)) < numRows && rows.Next() {
		record, err := mapper.MapRecord(rows)
		if err != nil {
			return nil, &sampleMapError{err: err}
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, &sampleQueryError{err: err}
	}
	return out, nil
}

// randomInRange draws the start of a slice in [lo, hi). It needs lo < hi and a span that
// fits an int64, which the caller checked.
func randomInRange(lo, hi int64) int64 {
	return lo + rand.Int64N(hi-lo) //nolint:gosec // a slice start, not a secret
}
