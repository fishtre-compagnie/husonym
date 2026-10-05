package connectiondata

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	querybuilder "github.com/fishtre-compagnie/husonym/worker/pkg/query-builder"
	"github.com/stretchr/testify/require"
)

const (
	mysqlKeyLookup = "FROM information_schema.STATISTICS"
	mysqlBounds    = "SELECT MIN\\(`id`\\), MAX\\(`id`\\) FROM `public`.`users`"
	pgEstimate     = "FROM pg_class"
)

// recordingDB hands every statement to a sqlmock database and keeps a copy, so a test can
// assert on the statements issued even when the code under test swallows their errors.
type recordingDB struct {
	db         *sql.DB
	statements []string
}

func (r *recordingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.statements = append(r.statements, query)
	return r.db.QueryContext(ctx, query, args...)
}

func (r *recordingDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	r.statements = append(r.statements, query)
	return r.db.QueryRowContext(ctx, query, args...)
}

func newSampleDB(t *testing.T) (*recordingDB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})
	return &recordingDB{db: db}, mock
}

func capturedLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func firstOfRange(lo, _ int64) int64 { return lo }

func keyRows(columns ...[2]string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE"})
	for _, c := range columns {
		rows.AddRow(c[0], c[1])
	}
	return rows
}

func estimateRows(reltuples float64, relpages, pages int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"reltuples", "relpages", "pages"}).AddRow(reltuples, relpages, pages)
}

func spread(
	t *testing.T,
	db sampleQuerier,
	driver string,
	pick func(lo, hi int64) int64,
) (string, bool) {
	t.Helper()
	logger, _ := capturedLogger()
	return spreadSampleQuery(t.Context(), logger, db, driver, "public", "users", 20, pick)
}

func Test_spreadSampleQuery_PostgresUsesTheEstimate(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(pgEstimate).WithArgs(`"public"."users"`).WillReturnRows(estimateRows(200000, 1870, 1870))

	query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE SYSTEM (0.5)")
	require.Len(t, db.statements, 1)
}

func Test_spreadSampleQuery_PostgresScalesTheEstimateToTheCurrentSize(t *testing.T) {
	db, mock := newSampleDB(t)
	// Analyzed at 2 000 rows over 20 pages, then loaded to 1 000 times the pages.
	mock.ExpectQuery(pgEstimate).WillReturnRows(estimateRows(2000, 20, 20000))

	query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE SYSTEM (0.05)")
	require.Contains(t, query, "LIMIT 4000")
}

func Test_spreadSampleQuery_NoEstimate(t *testing.T) {
	cases := map[string]func(*sqlmock.ExpectedQuery){
		"never analyzed": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(-1, 0, 1870)) },
		"empty":          func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(0, 0, 0)) },
		"no pages analyzed": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(estimateRows(50000, 0, 1870))
		},
		"partitioned parent": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(200000, -1, 0)) },
		"no current size": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"reltuples", "relpages", "pages"}).AddRow(200000, 1870, nil))
		},
		"fits the window": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(500, 5, 5)) },
		"missing relation": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"reltuples", "relpages", "pages"}))
		},
		"catalog error": func(q *sqlmock.ExpectedQuery) { q.WillReturnError(errors.New("catalog unavailable")) },
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			respond(mock.ExpectQuery(pgEstimate).WithArgs(`"public"."users"`))

			query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

			require.False(t, ok)
			require.Empty(t, query)
			require.Len(t, db.statements, 1)
		})
	}
}

func Test_spreadSampleQuery_SqlServerReadsNoCatalog(t *testing.T) {
	db, _ := newSampleDB(t)

	query, ok := spread(t, db, sqlmanager_shared.MssqlDriver, firstOfRange)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE (1000 ROWS)")
	require.Empty(t, db.statements)
}

func Test_spreadSampleQuery_UnknownDriver(t *testing.T) {
	db, _ := newSampleDB(t)

	_, ok := spread(t, db, "oracle", firstOfRange)

	require.False(t, ok)
	require.Empty(t, db.statements)
}

func Test_spreadSampleQuery_MysqlSlicesOnASingleIntegerKey(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(mysqlKeyLookup).WithArgs("public", "users").
		WillReturnRows(keyRows([2]string{"id", "bigint"}))
	mock.ExpectQuery(mysqlBounds).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(1000000)))
	var picked [][2]int64
	pick := func(lo, hi int64) int64 {
		picked = append(picked, [2]int64{lo, hi})
		return lo + 1
	}

	query, ok := spread(t, db, sqlmanager_shared.MysqlDriver, pick)

	require.True(t, ok)
	require.Equal(t, querybuilder.SampleSlices, strings.Count(query, "LIMIT 100"))
	require.Len(t, picked, querybuilder.SampleSlices)
	require.Equal(t, [2]int64{1, 99999}, picked[0])
	require.Equal(t, [2]int64{899992, 1000000}, picked[len(picked)-1])
	require.Contains(t, query, "(`id` >= 2) AND (`id` <= 99999)")
	require.Contains(t, query, "(`id` >= 899993) AND (`id` <= 1000000)")
	require.Len(t, db.statements, 2)
}

// Each case must stop before the bounds are read.
func Test_spreadSampleQuery_MysqlWithoutAUsableKey(t *testing.T) {
	cases := map[string]*sqlmock.Rows{
		"composite key": keyRows([2]string{"a", "int"}, [2]string{"b", "int"}),
		"varchar key":   keyRows([2]string{"code", "varchar"}),
		"decimal key":   keyRows([2]string{"id", "decimal"}),
		"no key":        keyRows(),
		"backtick name": keyRows([2]string{"i`d", "int"}),
		"dotted name":   keyRows([2]string{"a.id", "int"}),
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			mock.ExpectQuery(mysqlKeyLookup).WithArgs("public", "users").WillReturnRows(key)

			query, ok := spread(t, db, sqlmanager_shared.MysqlDriver, firstOfRange)

			require.False(t, ok)
			require.Empty(t, query)
			require.Len(t, db.statements, 1)
			require.Contains(t, db.statements[0], "information_schema.STATISTICS")
		})
	}
}

func Test_spreadSampleQuery_MysqlWithBoundsNotWorthASpread(t *testing.T) {
	cases := map[string]*sqlmock.Rows{
		"empty table":                sqlmock.NewRows([]string{"min", "max"}).AddRow(nil, nil),
		"unsigned beyond int64":      sqlmock.NewRows([]string{"min", "max"}).AddRow("1", "18446744073709551615"),
		"table that fits the window": sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(500)),
		"span too wide for an int64": sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(-9e18), int64(9e18)),
		"span just under the window": sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(1000)),
	}
	for name, bounds := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			mock.ExpectQuery(mysqlKeyLookup).WillReturnRows(keyRows([2]string{"id", "int"}))
			mock.ExpectQuery(mysqlBounds).WillReturnRows(bounds)

			_, ok := spread(t, db, sqlmanager_shared.MysqlDriver, firstOfRange)

			require.False(t, ok)
			require.Len(t, db.statements, 2)
		})
	}
}

func Test_splitKeySpan(t *testing.T) {
	cases := map[string][2]int64{
		"round span":                {1, 1000001},
		"span not divisible by ten": {0, 1234},
		"negative keys":             {-2000, -1},
		"just above the threshold":  {1, 1001},
		"across zero":               {-5000, 7777},
	}
	for name, span := range cases {
		t.Run(name, func(t *testing.T) {
			lo, hi := span[0], span[1]

			parts := splitKeySpan(lo, hi, querybuilder.SampleSlices)

			require.Len(t, parts, querybuilder.SampleSlices)
			require.Equal(t, lo, parts[0].From)
			require.Equal(t, hi, parts[len(parts)-1].To)
			for i, part := range parts {
				require.LessOrEqual(t, part.From, part.To)
				if i > 0 {
					require.Equal(t, parts[i-1].To+1, part.From, "ranges are consecutive and disjoint")
				}
			}
		})
	}
}

// idMapper reads the first column of each row.
type idMapper struct{}

func (idMapper) MapRecord(record any) (map[string]any, error) {
	var id int64
	if err := record.(*sql.Rows).Scan(&id); err != nil {
		return nil, err
	}
	return map[string]any{"id": id}, nil
}

// failingMapper fails with an error that quotes the value it was given.
type failingMapper struct{ canary string }

func (m failingMapper) MapRecord(any) (map[string]any, error) {
	return nil, errors.New(`parsing time "` + m.canary + `"`)
}

func idRows(ids ...int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id"})
	for _, id := range ids {
		rows.AddRow(id)
	}
	return rows
}

func sequence(from, count int64) []int64 {
	ids := make([]int64, 0, count)
	for i := range count {
		ids = append(ids, from+i)
	}
	return ids
}

func read(
	t *testing.T,
	db sampleQuerier,
	mapper recordMapper,
	hasSpread bool,
) ([]map[string]any, error) {
	t.Helper()
	logger, _ := capturedLogger()
	return readSample(t.Context(), logger, db, mapper, "SPREAD", hasSpread, "WINDOW", 20)
}

func Test_readSample_ShortSpreadFallsBackOnTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Len(t, got, 20)
	require.Equal(t, int64(100), got[0]["id"])
	require.Equal(t, []string{"SPREAD", "WINDOW"}, db.statements)
}

func Test_readSample_ShortSpreadIsKeptWhenTheWindowIsShorter(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(sequence(1, 8)...))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(1, 2))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Len(t, got, 8)
}

func Test_readSample_FailingSpreadFallsBackOnTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("TABLESAMPLE clause can only be used with local tables"))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Len(t, got, 20)
}

func Test_readSample_SpreadEnough(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(sequence(1, 20)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Len(t, got, 20)
	require.Equal(t, []string{"SPREAD"}, db.statements)
}

func Test_readSample_NoSpreadReadsTheWindowOnly(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(1, 2, 3))

	got, err := read(t, db, idMapper{}, false)

	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, []string{"WINDOW"}, db.statements)
}

func Test_readSample_WindowFails(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("spread refused"))
	mock.ExpectQuery("WINDOW").WillReturnError(errors.New("window refused"))

	_, err := read(t, db, idMapper{}, true)

	require.Error(t, err)
	require.EqualError(
		t,
		wrapSampleError(err, "public.users", "postgres"),
		"error querying table public.users with database type postgres: window refused",
	)
}

func Test_readSample_ShortSpreadSurvivesAFailingWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnError(errors.New("window refused"))
	logger, logs := capturedLogger()
	scoped := logger.With("table", "public.users")

	got, err := readSample(t.Context(), scoped, db, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Contains(t, logs.String(), "table=public.users")
	require.Contains(t, logs.String(), "level=DEBUG")
}

func Test_readSample_DoneContextIsNotAnsweredWithAShortSample(t *testing.T) {
	db, mock := newSampleDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	cancelAfterSpread := &cancellingDB{recordingDB: db, cancel: cancel}
	logger, _ := capturedLogger()

	_, err := readSample(ctx, logger, cancelAfterSpread, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.ErrorIs(t, err, context.Canceled)
}

// cancellingDB cancels its context once the spread query is read.
type cancellingDB struct {
	*recordingDB
	cancel context.CancelFunc
}

func (c *cancellingDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if query == "WINDOW" {
		c.cancel()
	}
	return c.recordingDB.QueryContext(ctx, query, args...)
}

func Test_readSample_RowErrorsAreLoggedWithoutTheirText(t *testing.T) {
	const canary = "jane.doe@example.com"
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))
	logger, logs := capturedLogger()

	// The spread read fails on the mapper, the window read goes through another one.
	got, err := readSample(t.Context(), logger, db, &switchMapper{first: failingMapper{canary}, then: idMapper{}},
		"SPREAD", true, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 20)
	require.Contains(t, logs.String(), "spread sample query failed")
	require.NotContains(t, logs.String(), canary)
}

// switchMapper uses first for its first call and then for the following ones.
type switchMapper struct {
	first, then recordMapper
	calls       int
}

func (m *switchMapper) MapRecord(record any) (map[string]any, error) {
	m.calls++
	if m.calls == 1 {
		return m.first.MapRecord(record)
	}
	return m.then.MapRecord(record)
}

func Test_wrapSampleError_Mapping(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("not a number"))

	_, err := read(t, db, idMapper{}, false)

	require.Error(t, err)
	require.Contains(
		t,
		wrapSampleError(err, "public.users", "postgres").Error(),
		"unable to convert row to map for table public.users with database type postgres: ",
	)
}

func Test_randomInRange_StaysInTheRangeBothEndsIncluded(t *testing.T) {
	seen := map[int64]bool{}
	for range 1000 {
		got := randomInRange(-1, 1)
		require.GreaterOrEqual(t, got, int64(-1))
		require.LessOrEqual(t, got, int64(1))
		seen[got] = true
	}
	require.Len(t, seen, 3)
}
