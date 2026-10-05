package connectiondata

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"regexp"
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
	mysqlCount     = "SELECT COUNT\\(\\*\\) FROM \\(SELECT \\* FROM \\(SELECT `id` FROM `public`.`users`"
	pgEstimate     = "FROM pg_class WHERE"
	pgPartitions   = "FROM pg_partition_tree"
	mssqlSize      = "FROM sys.schemas"
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

var estimateColumns = []string{"reltuples", "relpages", "pages", "partitioned"}

// estimateRows is the catalog's answer for an ordinary table.
func estimateRows(reltuples float64, relpages, pages int64) *sqlmock.Rows {
	return sqlmock.NewRows(estimateColumns).AddRow(reltuples, relpages, pages, false)
}

// partitionedParentRows is the catalog's answer for a partitioned table: no page of its own.
func partitionedParentRows() *sqlmock.Rows {
	return sqlmock.NewRows(estimateColumns).AddRow(float64(-1), int64(0), int64(0), true)
}

// leavesRows is the catalog's answer for the leaf partitions: the rows and the pages of the
// analyzed ones, the current pages of all, and whether one of them is a foreign table.
func leavesRows(reltuples float64, relpages, pages int64) *sqlmock.Rows {
	return sqlmock.NewRows(leavesColumns).AddRow(reltuples, relpages, pages, false)
}

var leavesColumns = []string{"reltuples", "relpages", "pages", "has_foreign_leaf"}

func countRows(count int64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"count"}).AddRow(count)
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
	require.Contains(t, query, "TABLESAMPLE SYSTEM (2.6738) WHERE RANDOM() < 0.187 LIMIT 4000")
	require.Len(t, db.statements, 1)
	require.NotContains(t, db.statements[0], "pg_partition_tree")
}

func Test_spreadSampleQuery_PostgresSumsTheLeavesOfAPartitionedTable(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(pgEstimate).WithArgs(`"public"."users"`).WillReturnRows(partitionedParentRows())
	// Three leaves out of four were analyzed: 150 000 rows on 1 500 pages, 100 rows a page. The
	// four hold 2 000 pages now, so 200 000 rows: fifty pages are 2.5 percent and hold 5 000.
	mock.ExpectQuery(pgPartitions).WithArgs(`"public"."users"`).WillReturnRows(leavesRows(150000, 1500, 2000))

	query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

	require.True(t, ok)
	require.Contains(t, query, `FROM "public"."users" TABLESAMPLE SYSTEM (2.5) WHERE RANDOM() < 0.2 LIMIT 4000`)
	require.Len(t, db.statements, 2)
	require.NotContains(t, db.statements[1], "users", "the name is a bind parameter")
}

func Test_spreadSampleQuery_PostgresPartitionedTableWithAForeignLeaf(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(pgEstimate).WithArgs(`"public"."users"`).WillReturnRows(partitionedParentRows())
	mock.ExpectQuery(pgPartitions).WithArgs(`"public"."users"`).
		WillReturnRows(sqlmock.NewRows(leavesColumns).AddRow(150000, 1500, 2000, true))

	query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

	require.False(t, ok)
	require.Empty(t, query)
	require.Len(t, db.statements, 2, "no sample statement is issued")
}

func Test_spreadSampleQuery_PostgresPartitionedTableWithoutASize(t *testing.T) {
	cases := map[string]func(*sqlmock.ExpectedQuery){
		"no analyzed leaf": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(leavesRows(0, 0, 2000)) },
		"no page":          func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(leavesRows(150000, 1500, 0)) },
		"no leaf":          func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(leavesRows(0, 0, 0)) },
		"fits the window":  func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(leavesRows(500, 5, 10)) },
		"catalog error": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnError(errors.New("function pg_partition_tree(regclass) does not exist"))
		},
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			mock.ExpectQuery(pgEstimate).WillReturnRows(partitionedParentRows())
			respond(mock.ExpectQuery(pgPartitions).WithArgs(`"public"."users"`))

			query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

			require.False(t, ok)
			require.Empty(t, query)
			require.Len(t, db.statements, 2)
		})
	}
}

// A name holding a quote character is read from the window: no statement is issued for it.
func Test_spreadSampleQuery_NameHoldingAQuoteCharacter(t *testing.T) {
	names := map[string][2]string{
		"double quote in the table":  {"public", `us"ers`},
		"backtick in the table":      {"public", "us`ers"},
		"opening bracket in a table": {"public", "us[ers"},
		"closing bracket in a table": {"public", "users]"},
		"double quote in the schema": {`pu"blic`, "users"},
		"backtick in the schema":     {"pu`blic", "users"},
		"bracket in the schema":      {"pu]blic", "users"},
	}
	drivers := []string{
		sqlmanager_shared.GoquPostgresDriver, sqlmanager_shared.MysqlDriver, sqlmanager_shared.MssqlDriver,
	}
	for _, driver := range drivers {
		for name, schemaTable := range names {
			t.Run(driver+"/"+name, func(t *testing.T) {
				db, _ := newSampleDB(t)
				logger, logs := capturedLogger()

				query, ok := spreadSampleQuery(
					t.Context(), logger, db, driver, schemaTable[0], schemaTable[1], 20, firstOfRange)

				require.False(t, ok)
				require.Empty(t, query)
				require.Empty(t, db.statements)
				require.Contains(t, logs.String(), "level=DEBUG")
				require.NotContains(t, logs.String(), schemaTable[0])
				require.NotContains(t, logs.String(), schemaTable[1])
			})
		}
	}
}

func Test_spreadSampleQuery_PostgresScalesTheEstimateToTheCurrentSize(t *testing.T) {
	db, mock := newSampleDB(t)
	// Analyzed at 2 000 rows over 20 pages, then loaded to 1 000 times the pages: 2 000 000
	// rows on 20 000 pages. Fifty pages are 0.25 percent and hold 5 000 rows.
	mock.ExpectQuery(pgEstimate).WillReturnRows(estimateRows(2000, 20, 20000))

	query, ok := spread(t, db, sqlmanager_shared.GoquPostgresDriver, firstOfRange)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE SYSTEM (0.25) WHERE RANDOM() < 0.2 LIMIT 4000")
}

func Test_spreadSampleQuery_NoEstimate(t *testing.T) {
	cases := map[string]func(*sqlmock.ExpectedQuery){
		"never analyzed": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(-1, 0, 1870)) },
		"empty":          func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(0, 0, 0)) },
		"no pages analyzed": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(estimateRows(50000, 0, 1870))
		},
		"no current page": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(200000, 1870, 0)) },
		"no current size": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows(estimateColumns).AddRow(200000, 1870, nil, false))
		},
		"fits the window": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(estimateRows(500, 5, 5)) },
		"missing relation": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows(estimateColumns))
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

func sizeRows(rows, pages any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"rows", "pages"}).AddRow(rows, pages)
}

func Test_spreadSampleQuery_SqlServerReadsTheSizeFromTheCatalog(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(mssqlSize).WithArgs("public", "users").WillReturnRows(sizeRows(200000, 852))

	query, ok := spread(t, db, sqlmanager_shared.MssqlDriver, firstOfRange)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE (5.8685 PERCENT))")
	require.Len(t, db.statements, 1)
	require.NotContains(t, db.statements[0], "users", "the names are bind parameters")
}

func Test_spreadSampleQuery_SqlServerWithoutASize(t *testing.T) {
	cases := map[string]func(*sqlmock.ExpectedQuery){
		"unknown or hidden table": func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(sizeRows(nil, nil)) },
		"empty":                   func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(sizeRows(0, 0)) },
		"no pages":                func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(sizeRows(200000, 0)) },
		"fits the window":         func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(sizeRows(1000, 5)) },
		"catalog error":           func(q *sqlmock.ExpectedQuery) { q.WillReturnError(errors.New("permission denied")) },
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			respond(mock.ExpectQuery(mssqlSize).WithArgs("public", "users"))

			query, ok := spread(t, db, sqlmanager_shared.MssqlDriver, firstOfRange)

			require.False(t, ok)
			require.Empty(t, query)
			require.Len(t, db.statements, 1)
		})
	}
}

func Test_spreadSampleQuery_UnknownDriver(t *testing.T) {
	db, _ := newSampleDB(t)

	_, ok := spread(t, db, "oracle", firstOfRange)

	require.False(t, ok)
	require.Empty(t, db.statements)
}

var idRange = regexp.MustCompile("\\(`id` >= -?[0-9]+\\) AND \\(`id` <= -?[0-9]+\\)")

func Test_spreadSampleQuery_MysqlSlicesOnASingleIntegerKey(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(mysqlKeyLookup).WithArgs("public", "users").
		WillReturnRows(keyRows([2]string{"id", "bigint"}))
	mock.ExpectQuery(mysqlBounds).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(1000000)))
	mock.ExpectQuery(mysqlCount).WillReturnRows(countRows(1000))
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
	require.Len(t, db.statements, 3)

	// The rows are counted over the ranges the sample then reads, on the key column alone.
	count := db.statements[2]
	require.Len(t, idRange.FindAllString(count, -1), querybuilder.SampleSlices)
	require.Equal(t, idRange.FindAllString(query, -1), idRange.FindAllString(count, -1))
	require.Equal(t, querybuilder.SampleSlices, strings.Count(count, "SELECT `id` FROM `public`.`users`"))
	require.Equal(t, querybuilder.SampleSlices, strings.Count(count, "LIMIT 100"))
}

// The slices must hold half of the rows they can hold, 500, for the sample to be drawn from
// them.
func Test_spreadSampleQuery_MysqlCountsTheRowsOfTheSlices(t *testing.T) {
	require.Equal(t, 500, querybuilder.SampleSlicesMinRows)
	cases := map[string]struct {
		respond func(*sqlmock.ExpectedQuery)
		spread  bool
	}{
		"below the threshold": {func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(countRows(499)) }, false},
		"one slice only":      {func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(countRows(101)) }, false},
		"no row":              {func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(countRows(0)) }, false},
		"at the threshold":    {func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(countRows(500)) }, true},
		"above the threshold": {func(q *sqlmock.ExpectedQuery) { q.WillReturnRows(countRows(1000)) }, true},
		"count refused":       {func(q *sqlmock.ExpectedQuery) { q.WillReturnError(errors.New("refused")) }, false},
		"count unreadable": {func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(nil))
		}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			mock.ExpectQuery(mysqlKeyLookup).WillReturnRows(keyRows([2]string{"id", "int"}))
			mock.ExpectQuery(mysqlBounds).
				WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(2000000)))
			tc.respond(mock.ExpectQuery(mysqlCount))

			query, ok := spread(t, db, sqlmanager_shared.MysqlDriver, firstOfRange)

			require.Equal(t, tc.spread, ok)
			require.Equal(t, tc.spread, query != "")
			require.Len(t, db.statements, 3)
		})
	}
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

// sentIDs collects the ids of the rows a sample sends, in order.
type sentIDs struct{ ids []int64 }

func (s *sentIDs) send(row map[string]any) error {
	s.ids = append(s.ids, row["id"].(int64))
	return nil
}

func read(
	t *testing.T,
	db sampleQuerier,
	mapper recordMapper,
	hasSpread bool,
) ([]int64, error) {
	t.Helper()
	logger, _ := capturedLogger()
	sent := &sentIDs{}
	err := readSample(t.Context(), logger, db, mapper, "SPREAD", hasSpread, "WINDOW", 20, sent.send)
	return sent.ids, err
}

func Test_readSample_SpreadEnough(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(sequence(1, 25)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Equal(t, sequence(1, 20), got)
	require.Equal(t, []string{"SPREAD"}, db.statements)
}

func Test_readSample_WindowGivesTheRowsAShortSpreadMisses(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Equal(t, append([]int64{1, 2, 3}, sequence(100, 17)...), got)
	require.Equal(t, []string{"SPREAD", "WINDOW"}, db.statements)
}

// The window does not know the rows the spread sent: a row of both is sent twice.
func Test_readSample_ARowOfBothQueriesIsSentTwice(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(1, 20)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Equal(t, append([]int64{1, 2, 3}, sequence(1, 17)...), got)
}

func Test_readSample_ShortSpreadAndShortWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(sequence(1, 8)...))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(50, 51))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Equal(t, append(sequence(1, 8), 50, 51), got)
}

func Test_readSample_RefusedSpreadGivesTheRowsOfTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("TABLESAMPLE clause can only be used with local tables"))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 25)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Equal(t, sequence(100, 20), got)
}

func Test_readSample_SpreadFailingWhileReadIsCompletedFromTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3, 4).RowError(2, errors.New("connection lost")))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))

	got, err := read(t, db, idMapper{}, true)

	require.NoError(t, err)
	require.Equal(t, append([]int64{1, 2}, sequence(100, 18)...), got)
}

func Test_readSample_NoSpreadReadsTheWindowOnly(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(1, 25)...))

	got, err := read(t, db, idMapper{}, false)

	require.NoError(t, err)
	require.Equal(t, sequence(1, 20), got)
	require.Equal(t, []string{"WINDOW"}, db.statements)
}

// countingMapper counts the rows it converted.
type countingMapper struct {
	recordMapper
	calls int
}

func (m *countingMapper) MapRecord(record any) (map[string]any, error) {
	m.calls++
	return m.recordMapper.MapRecord(record)
}

// A row is sent before the next one is read, so one row is held at a time.
func Test_readSample_SendsEachRowBeforeReadingTheNext(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(100, 101, 102))
	mapper := &countingMapper{recordMapper: idMapper{}}
	logger, _ := capturedLogger()
	var readWhenSent []int
	send := func(map[string]any) error {
		readWhenSent = append(readWhenSent, mapper.calls)
		return nil
	}

	err := readSample(t.Context(), logger, db, mapper, "SPREAD", true, "WINDOW", 20, send)

	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3, 4, 5, 6}, readWhenSent)
}

func Test_readSample_WindowFailsWithNoRowSent(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("spread refused"))
	mock.ExpectQuery("WINDOW").WillReturnError(errors.New("window refused"))

	got, err := read(t, db, idMapper{}, true)

	require.Empty(t, got)
	require.Error(t, err)
	require.EqualError(
		t,
		wrapSampleError(err, "public.users", "postgres"),
		"error querying table public.users with database type postgres: window refused",
	)
}

func Test_readSample_WindowAloneFailingWhileReadIsAnError(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(1, 2, 3).RowError(2, errors.New("connection lost")))

	got, err := read(t, db, idMapper{}, false)

	require.Equal(t, []int64{1, 2}, got)
	require.EqualError(
		t,
		wrapSampleError(err, "public.users", "postgres"),
		"unable to convert row to map for table public.users with database type postgres: connection lost",
	)
}

func Test_readSample_RowsOfTheSpreadSurviveAFailingWindow(t *testing.T) {
	cases := map[string]struct {
		window func(*sqlmock.ExpectedQuery)
		want   []int64
	}{
		"window refused": {
			func(q *sqlmock.ExpectedQuery) { q.WillReturnError(errors.New("window refused")) },
			[]int64{1, 2, 3},
		},
		"window failing while read": {
			func(q *sqlmock.ExpectedQuery) {
				q.WillReturnRows(idRows(100, 101, 102).RowError(2, errors.New("connection lost")))
			},
			[]int64{1, 2, 3, 100, 101},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
			tc.window(mock.ExpectQuery("WINDOW"))
			logger, logs := capturedLogger()
			scoped := logger.With("table", "public.users")
			sent := &sentIDs{}

			err := readSample(t.Context(), scoped, db, idMapper{}, "SPREAD", true, "WINDOW", 20, sent.send)

			require.NoError(t, err)
			require.Equal(t, tc.want, sent.ids)
			require.Contains(t, logs.String(), "window query failed")
			require.Contains(t, logs.String(), "table=public.users")
			require.Contains(t, logs.String(), "level=DEBUG")
		})
	}
}

// cancellingMapper cancels its context once it has converted a row.
type cancellingMapper struct {
	recordMapper
	cancel context.CancelFunc
}

func (m cancellingMapper) MapRecord(record any) (map[string]any, error) {
	row, err := m.recordMapper.MapRecord(record)
	m.cancel()
	return row, err
}

func Test_readSample_DoneContextEndsTheSampleBeforeTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	logger, _ := capturedLogger()
	sent := &sentIDs{}

	err := readSample(ctx, logger, db, cancellingMapper{idMapper{}, cancel}, "SPREAD", true, "WINDOW", 20, sent.send)

	require.ErrorIs(t, err, context.Canceled)
	require.NotEmpty(t, sent.ids)
	require.Equal(t, []string{"SPREAD"}, db.statements)
	require.EqualError(
		t,
		wrapSampleError(err, "public.users", "postgres"),
		"error querying table public.users with database type postgres: context canceled",
	)
}

func Test_readSample_DoneContextWhileTheWindowIsRead(t *testing.T) {
	db, mock := newSampleDB(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	// The window query fails on the context before it reaches the database.
	cancelAtWindow := &cancellingDB{recordingDB: db, cancel: cancel}
	logger, _ := capturedLogger()
	sent := &sentIDs{}

	err := readSample(ctx, logger, cancelAtWindow, idMapper{}, "SPREAD", true, "WINDOW", 20, sent.send)

	require.ErrorIs(t, err, context.Canceled)
}

// cancellingDB cancels its context when the window query is issued.
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

// The error of the receiver of the rows ends the sample: nothing more is read, and the error is
// reported as it is.
func Test_readSample_ErrorOfTheReceiverEndsTheSample(t *testing.T) {
	refused := errors.New("stream closed")
	cases := map[string]struct {
		spread, window []int64
		statements     []string
	}{
		"while the spread is read":                {[]int64{1, 2, 3}, nil, []string{"SPREAD"}},
		"while the window is read":                {nil, []int64{1, 2, 3}, []string{"WINDOW"}},
		"while the window completes a short read": {[]int64{1}, []int64{1, 2, 3}, []string{"SPREAD", "WINDOW"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			if tc.spread != nil {
				mock.ExpectQuery("SPREAD").WillReturnRows(idRows(tc.spread...))
			}
			if tc.window != nil {
				mock.ExpectQuery("WINDOW").WillReturnRows(idRows(tc.window...))
			}
			logger, logs := capturedLogger()
			calls := 0
			send := func(map[string]any) error {
				calls++
				if calls == 2 {
					return refused
				}
				return nil
			}

			err := readSample(t.Context(), logger, db, idMapper{}, "SPREAD", tc.spread != nil, "WINDOW", 20, send)

			require.ErrorIs(t, err, refused)
			require.Equal(t, 2, calls)
			require.Equal(t, tc.statements, db.statements)
			require.Same(t, refused, wrapSampleError(err, "public.users", "postgres"))
			require.Empty(t, logs.String())
		})
	}
}

func Test_readSample_RowErrorsAreLoggedWithoutTheirText(t *testing.T) {
	const canary = "jane.doe@example.com"
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))
	logger, logs := capturedLogger()
	sent := &sentIDs{}

	// The spread read fails on the mapper, the window read goes through another one.
	err := readSample(t.Context(), logger, db, &switchMapper{first: failingMapper{canary}, then: idMapper{}},
		"SPREAD", true, "WINDOW", 20, sent.send)

	require.NoError(t, err)
	require.Equal(t, sequence(100, 20), sent.ids)
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
