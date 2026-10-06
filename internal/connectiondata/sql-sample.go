package connectiondata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	querybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/query-builder"
)

const (
	// The planner's row count is as old as the last analyze, so it is scaled to the current
	// size of the table the way the planner does: rows per page times current pages.
	postgresEstimateQuery = `SELECT reltuples, relpages, pg_relation_size(oid) / current_setting('block_size')::bigint, relkind = 'p'
FROM pg_class WHERE oid = to_regclass($1)`

	// A partitioned table holds its rows in its leaf partitions. The rows and the pages
	// the planner knows are summed over the leaves that were analyzed, the current pages
	// over all of them. The last column tells whether a leaf is a foreign table.
	postgresPartitionsEstimateQuery = `SELECT
  COALESCE(SUM(c.reltuples) FILTER (WHERE c.reltuples > 0 AND c.relpages > 0), 0)::float8,
  COALESCE(SUM(c.relpages) FILTER (WHERE c.reltuples > 0 AND c.relpages > 0), 0)::bigint,
  COALESCE(SUM(pg_relation_size(c.oid) / current_setting('block_size')::bigint), 0)::bigint,
  COALESCE(BOOL_OR(c.relkind = 'f'), false)
FROM pg_partition_tree(to_regclass($1)) t
JOIN pg_class c ON c.oid = t.relid
WHERE t.isleaf`

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
// reading or converting a row; sampleSendError is a row its receiver refused. The second
// kind may quote a value of the row, so its text is never logged.
type sampleQueryError struct{ err error }

func (e *sampleQueryError) Error() string { return e.err.Error() }
func (e *sampleQueryError) Unwrap() error { return e.err }

type sampleRowError struct{ err error }

func (e *sampleRowError) Error() string { return e.err.Error() }
func (e *sampleRowError) Unwrap() error { return e.err }

type sampleSendError struct{ err error }

func (e *sampleSendError) Error() string { return e.err.Error() }
func (e *sampleSendError) Unwrap() error { return e.err }

// wrapSampleError gives an error of readSample the message SampleData reports. The error
// of the receiver of the rows is reported as it is.
func wrapSampleError(err error, schemaTable, driver string) error {
	var sendErr *sampleSendError
	if errors.As(err, &sendErr) {
		return sendErr.err
	}
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
	switch driver {
	case sqlmanager_shared.GoquPostgresDriver:
		size, ok := postgresSize(ctx, logger, db, schema, table)
		if !ok {
			return "", false
		}
		return tableSampleQuery(ctx, logger, driver, schema, table, size, numRows)
	case sqlmanager_shared.MssqlDriver:
		size, ok := sqlServerSize(ctx, logger, db, schema, table)
		if !ok {
			return "", false
		}
		return tableSampleQuery(ctx, logger, driver, schema, table, size, numRows)
	case sqlmanager_shared.MysqlDriver:
		return mysqlKeySlicesQuery(ctx, logger, db, schema, table, numRows, pick)
	default:
		return "", false
	}
}

func tableSampleQuery(
	ctx context.Context,
	logger *slog.Logger,
	driver, schema, table string,
	size querybuilder.TableSize,
	numRows uint,
) (string, bool) {
	query, ok, err := querybuilder.BuildTableSampleQuery(driver, schema, table, size, numRows)
	if err != nil {
		logger.DebugContext(ctx, "no spread sample query", "error", err)
		return "", false
	}
	return query, ok
}

// postgresSize returns the number of pages of the table and the number of rows it holds
// now, from the planner's density (rows per page) and the current number of pages. The
// figures of a partitioned table are those of its leaf partitions: the density of the
// leaves that were analyzed, and the current pages of all of them. A table that was never
// analyzed, a partitioned table with no analyzed leaf, a partitioned table with a foreign
// table among its leaves, a view, a missing relation and a failing query all mean there is
// no size.
func postgresSize(
	ctx context.Context,
	logger *slog.Logger,
	db sampleQuerier,
	schema, table string,
) (querybuilder.TableSize, bool) {
	// to_regclass reads its argument as a name written in SQL: each part is quoted.
	name := sqlident.Postgres.Qualified(schema, table)
	var reltuples float64
	var relpages, pages sql.NullInt64
	var partitioned bool
	err := db.QueryRowContext(ctx, postgresEstimateQuery, name).Scan(&reltuples, &relpages, &pages, &partitioned)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logger.DebugContext(ctx, "table size unavailable", "error_kind", fmt.Sprintf("%T", err))
		}
		return querybuilder.TableSize{}, false
	}
	if partitioned {
		var foreignLeaf bool
		err := db.QueryRowContext(ctx, postgresPartitionsEstimateQuery, name).
			Scan(&reltuples, &relpages, &pages, &foreignLeaf)
		if err != nil {
			logger.DebugContext(ctx, "partitions size unavailable", "error_kind", fmt.Sprintf("%T", err))
			return querybuilder.TableSize{}, false
		}
		// A foreign table is read whole by a sampled scan of its parent, so the sample
		// would come mostly from it.
		if foreignLeaf {
			logger.DebugContext(ctx, "table size unavailable: a partition is a foreign table")
			return querybuilder.TableSize{}, false
		}
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
	key, err := mysqlIntegerKey(ctx, db, schema, table)
	if err != nil {
		logger.DebugContext(ctx, "no spread sample query", "error", err)
		return "", false
	}

	var lo, hi sql.NullInt64
	bounds := fmt.Sprintf(
		"SELECT MIN(%[1]s), MAX(%[1]s) FROM %s",
		sqlident.MySQL.Quote(key),
		sqlident.MySQL.Qualified(schema, table),
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

	// The rows of the slices are counted on the key first: when they hold fewer than
	// SampleSlicesMinRows, the window is read.
	countQuery, err := querybuilder.BuildKeySlicesCountQuery(sqlmanager_shared.MysqlDriver, schema, table, key, slices)
	if err != nil {
		logger.DebugContext(ctx, "no spread sample query", "error", err)
		return "", false
	}
	var count int64
	if err := db.QueryRowContext(ctx, countQuery).Scan(&count); err != nil {
		logger.DebugContext(ctx, "no spread sample query: key slices not counted", "error_kind", fmt.Sprintf("%T", err))
		return "", false
	}
	if count < querybuilder.SampleSlicesMinRows {
		logger.DebugContext(ctx, "no spread sample query: the key slices hold too few rows", "rows", count)
		return "", false
	}

	query, err := querybuilder.BuildKeySlicesSampleQuery(
		sqlmanager_shared.MysqlDriver, schema, table, key, slices, numRows,
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
// integer type, whose name holds no dot.
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
	if strings.Contains(column, ".") {
		return "", errors.New("the primary key column name holds a dot")
	}
	return column, nil
}

// readSample hands the rows of a sample to send as they are read, numRows at most, one
// row being held at a time.
//
// The spread query is read first when there is one. When it ends with fewer than numRows
// rows sent, because it returned too few or failed, the window is read and gives the
// rows that are missing. The two queries do not know of each other, so a row may then be
// sent by both. A done context ends the sample with the error of the context, before the
// window is read. If the window fails after rows of the spread were sent, the sample ends
// there without an error.
//
// Errors are a *sampleQueryError, a *sampleRowError, a *sampleSendError for the error of
// send, which ends the sample at once, or the error of the context.
func readSample(
	ctx context.Context,
	logger *slog.Logger,
	db sampleQuerier,
	mapper recordMapper,
	spread string,
	hasSpread bool,
	window string,
	numRows uint,
	send func(row map[string]any) error,
) error {
	var sent uint
	if hasSpread {
		var err error
		sent, err = streamRows(ctx, db, mapper, spread, numRows, send)
		if isSendError(err) {
			return err
		}
		if sent >= numRows {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			logSampleFailure(ctx, logger, "spread sample query failed, reading the window", err)
		}
	}

	_, err := streamRows(ctx, db, mapper, window, numRows-sent, send)
	if err == nil || isSendError(err) {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if sent == 0 {
		return err
	}
	logSampleFailure(ctx, logger, "window query failed, keeping the short spread sample", err)
	return nil
}

func isSendError(err error) bool {
	var sendErr *sampleSendError
	return errors.As(err, &sendErr)
}

// streamRows runs a query and hands its rows to send as they are read, limit at most. It
// returns the number of rows sent, also when it fails.
func streamRows(
	ctx context.Context,
	db sampleQuerier,
	mapper recordMapper,
	query string,
	limit uint,
	send func(row map[string]any) error,
) (uint, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		if husonymdb.IsNoRows(err) {
			return 0, nil
		}
		return 0, &sampleQueryError{err: err}
	}
	defer rows.Close()

	var sent uint
	for sent < limit && rows.Next() {
		record, err := mapper.MapRecord(rows)
		if err != nil {
			return sent, &sampleRowError{err: err}
		}
		if err := send(record); err != nil {
			return sent, &sampleSendError{err: err}
		}
		sent++
	}
	if err := rows.Err(); err != nil {
		return sent, &sampleRowError{err: err}
	}
	return sent, nil
}

// randomInRange draws a value in [lo, hi], both ends included. It needs hi - lo to be
// less than the largest int64, which a slice of a key span always is.
func randomInRange(lo, hi int64) int64 {
	return lo + rand.Int64N(hi-lo+1) //nolint:gosec // a slice start, not a secret
}
