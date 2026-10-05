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
	// The planner's row count is as old as the last analyze, so it is scaled to the current
	// size of the table the way the planner does: rows per page times current pages.
	postgresEstimateQuery = `SELECT reltuples, relpages, pg_relation_size(oid) / current_setting('block_size')::bigint
FROM pg_class WHERE oid = to_regclass($1)`

	// The rows and the in-row data pages of the heap or of the clustered index, over every
	// partition. A login sees the tables it may read; any other table gives no size.
	sqlServerSizeQuery = `SELECT SUM(p.rows), SUM(a.data_pages)
FROM sys.schemas s
JOIN sys.tables t ON t.schema_id = s.schema_id
JOIN sys.partitions p ON p.object_id = t.object_id AND p.index_id IN (0, 1)
JOIN sys.allocation_units a ON a.container_id = p.hobt_id AND a.type = 1
WHERE s.name = @p1 AND t.name = @p2`

	// A usable key is the only column of the primary index. Unlike COLUMN_KEY, the
	// STATISTICS view does not report a unique index on a NOT NULL column as primary.
	mysqlPrimaryKeyQuery = `SELECT s.COLUMN_NAME, c.DATA_TYPE
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

// sampleQueryError is a query the database refused; sampleRowError is a failure while
// reading or converting a row. The second kind may quote a value of the row, so its text
// is never logged.
type sampleQueryError struct{ err error }

func (e *sampleQueryError) Error() string { return e.err.Error() }
func (e *sampleQueryError) Unwrap() error { return e.err }

type sampleRowError struct{ err error }

func (e *sampleRowError) Error() string { return e.err.Error() }
func (e *sampleRowError) Unwrap() error { return e.err }

// wrapSampleError gives an error of readSample the message SampleData reports.
func wrapSampleError(err error, schemaTable, driver string) error {
	var rowErr *sampleRowError
	if errors.As(err, &rowErr) {
		return fmt.Errorf(
			"unable to convert row to map for table %s with database type %s: %w",
			schemaTable, driver, rowErr.err,
		)
	}
	var queryErr *sampleQueryError
	if errors.As(err, &queryErr) {
		err = queryErr.err
	}
	return fmt.Errorf("error querying table %s with database type %s: %w", schemaTable, driver, err)
}

// logSampleFailure logs why a sample query failed. The text of an error met while
// reading a row is left out: it may hold a value of the row.
func logSampleFailure(ctx context.Context, logger *slog.Logger, msg string, err error) {
	var rowErr *sampleRowError
	if errors.As(err, &rowErr) {
		logger.DebugContext(ctx, msg, "stage", "reading a row", "error_kind", fmt.Sprintf("%T", rowErr.err))
		return
	}
	logger.DebugContext(ctx, msg, "stage", "query", "error", err)
}

// spreadSampleQuery returns a query that draws rows across the whole table, or false
// when the database cannot do it cheaply. It never fails: the reason is logged at debug
// level and the caller reads the window instead. pick draws a value in [lo, hi], both
// ends included.
func spreadSampleQuery(
	ctx context.Context,
	logger *slog.Logger,
	db sampleQuerier,
	driver, schema, table string,
	numRows uint,
	pick func(lo, hi int64) int64,
) (string, bool) {
	qualified := sqlmanager_shared.BuildTable(schema, table)
	switch driver {
	case sqlmanager_shared.GoquPostgresDriver:
		size, ok := postgresSize(ctx, logger, db, schema, table)
		if !ok {
			return "", false
		}
		return tableSampleQuery(ctx, logger, driver, qualified, size, numRows)
	case sqlmanager_shared.MssqlDriver:
		size, ok := sqlServerSize(ctx, logger, db, schema, table)
		if !ok {
			return "", false
		}
		return tableSampleQuery(ctx, logger, driver, qualified, size, numRows)
	case sqlmanager_shared.MysqlDriver:
		return mysqlKeySlicesQuery(ctx, logger, db, schema, table, numRows, pick)
	default:
		return "", false
	}
}

func tableSampleQuery(
	ctx context.Context,
	logger *slog.Logger,
	driver, qualified string,
	size querybuilder.TableSize,
	numRows uint,
) (string, bool) {
	query, ok, err := querybuilder.BuildTableSampleQuery(driver, qualified, size, numRows)
	if err != nil {
		logger.DebugContext(ctx, "no spread sample query", "error", err)
		return "", false
	}
	return query, ok
}

// postgresSize returns the number of pages of the table and the number of rows it holds
// now, from the planner's density (rows per page) and the current number of pages. A
// table that was never analyzed, a partitioned parent, a view, a missing relation and a
// failing query all mean there is no size.
func postgresSize(
	ctx context.Context,
	logger *slog.Logger,
	db sampleQuerier,
	schema, table string,
) (querybuilder.TableSize, bool) {
	name := sqlmanager_postgres.EscapePgColumn(schema) + "." + sqlmanager_postgres.EscapePgColumn(table)
	var reltuples float64
	var relpages, pages sql.NullInt64
	err := db.QueryRowContext(ctx, postgresEstimateQuery, name).Scan(&reltuples, &relpages, &pages)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logger.DebugContext(ctx, "table size unavailable", "error_kind", fmt.Sprintf("%T", err))
		}
		return querybuilder.TableSize{}, false
	}
	if reltuples <= 0 || !relpages.Valid || relpages.Int64 <= 0 || !pages.Valid || pages.Int64 <= 0 {
		return querybuilder.TableSize{}, false
	}
	return querybuilder.TableSize{
		Rows:  int64(reltuples / float64(relpages.Int64) * float64(pages.Int64)),
		Pages: pages.Int64,
	}, true
}

// sqlServerSize returns the rows and the data pages the catalog counts for the table. A
// view, a missing table, a table the login may not read, an empty table and a failing
// query all mean there is no size.
func sqlServerSize(
	ctx context.Context,
	logger *slog.Logger,
	db sampleQuerier,
	schema, table string,
) (querybuilder.TableSize, bool) {
	var rows, pages sql.NullInt64
	if err := db.QueryRowContext(ctx, sqlServerSizeQuery, schema, table).Scan(&rows, &pages); err != nil {
		logger.DebugContext(ctx, "table size unavailable", "error_kind", fmt.Sprintf("%T", err))
		return querybuilder.TableSize{}, false
	}
	if !rows.Valid || rows.Int64 <= 0 || !pages.Valid || pages.Int64 <= 0 {
		return querybuilder.TableSize{}, false
	}
	return querybuilder.TableSize{Rows: rows.Int64, Pages: pages.Int64}, true
}

func mysqlKeySlicesQuery(
	ctx context.Context,
	logger *slog.Logger,
	db sampleQuerier,
	schema, table string,
	numRows uint,
	pick func(lo, hi int64) int64,
) (string, bool) {
	qualified := sqlmanager_shared.BuildTable(schema, table)
	key, err := mysqlIntegerKey(ctx, db, schema, table)
	if err != nil {
		logger.DebugContext(ctx, "no spread sample query", "error", err)
		return "", false
	}

	var lo, hi sql.NullInt64
	bounds := fmt.Sprintf(
		"SELECT MIN(%[1]s), MAX(%[1]s) FROM %s.%s",
		sqlmanager_mysql.EscapeMysqlColumn(key),
		sqlmanager_mysql.EscapeMysqlColumn(schema),
		sqlmanager_mysql.EscapeMysqlColumn(table),
	)
	// The error of this scan may quote a key value, so only its kind is logged.
	if err := db.QueryRowContext(ctx, bounds).Scan(&lo, &hi); err != nil {
		logger.DebugContext(ctx, "no spread sample query: key bounds unreadable", "error_kind", fmt.Sprintf("%T", err))
		return "", false
	}
	// An empty table has no bounds. A range no wider than the window is served by it. A
	// negative span is a range too wide for an int64.
	if !lo.Valid || !hi.Valid || hi.Int64-lo.Int64 < querybuilder.SampleWindowSize {
		return "", false
	}

	// One slice per consecutive part of the key span, each starting at a random key of
	// its part and not leaving it, so the slices never overlap.
	parts := splitKeySpan(lo.Int64, hi.Int64, querybuilder.SampleSlices)
	slices := make([]querybuilder.KeyRange, len(parts))
	for i, part := range parts {
		slices[i] = querybuilder.KeyRange{From: pick(part.From, part.To), To: part.To}
	}
	query, err := querybuilder.BuildKeySlicesSampleQuery(
		sqlmanager_shared.MysqlDriver, qualified, key, slices, numRows,
	)
	if err != nil {
		logger.DebugContext(ctx, "no spread sample query", "error", err)
		return "", false
	}
	return query, true
}

// splitKeySpan cuts [lo, hi] into count consecutive ranges of equal width, the last one
// taking the remainder and ending at hi. It needs hi - lo to fit an int64 and to be at
// least count.
func splitKeySpan(lo, hi int64, count int) []querybuilder.KeyRange {
	width := (hi - lo) / int64(count)
	parts := make([]querybuilder.KeyRange, count)
	for i := range parts {
		from := lo + int64(i)*width
		parts[i] = querybuilder.KeyRange{From: from, To: from + width - 1}
	}
	parts[count-1].To = hi
	return parts
}

// mysqlIntegerKey returns the name of the primary key when it is a single column of an
// integer type, whose name goqu can quote.
func mysqlIntegerKey(ctx context.Context, db sampleQuerier, schema, table string) (string, error) {
	rows, err := db.QueryContext(ctx, mysqlPrimaryKeyQuery, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var column, dataType string
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&column, &dataType); err != nil {
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
	if strings.ContainsAny(column, "`.") {
		return "", errors.New("the primary key column name cannot be quoted")
	}
	return column, nil
}

// readSample reads the spread query when there is one. When it fails or returns fewer
// than numRows rows, the window is read as well and the larger result is returned. If
// the window fails after a short spread, the spread rows are returned, unless the
// context is done. Errors are a *sampleQueryError or a *sampleRowError.
func readSample(
	ctx context.Context,
	logger *slog.Logger,
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
			logSampleFailure(ctx, logger, "spread sample query failed, reading the window", err)
			spreadRows = nil
		}
		if uint(len(spreadRows)) >= numRows {
			return spreadRows, nil
		}
	}

	windowRows, err := readRows(ctx, db, mapper, window, numRows)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if len(spreadRows) == 0 {
			return nil, err
		}
		logSampleFailure(ctx, logger, "window query failed, keeping the short spread sample", err)
		return spreadRows, nil
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
			return nil, &sampleRowError{err: err}
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, &sampleRowError{err: err}
	}
	return out, nil
}

// randomInRange draws a value in [lo, hi], both ends included. It needs hi - lo to be
// less than the largest int64, which a slice of a key span always is.
func randomInRange(lo, hi int64) int64 {
	return lo + rand.Int64N(hi-lo+1) //nolint:gosec // a slice start, not a secret
}
