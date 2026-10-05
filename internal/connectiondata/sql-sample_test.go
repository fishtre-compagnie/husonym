package connectiondata

import (
	"database/sql"
	"errors"
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
	pgEstimate     = "FROM pg_class WHERE oid = to_regclass"
)

func newSampleDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})
	return db, mock
}

func fixedPick(lo, _ int64) int64 { return lo + 1 }

func keyRows(columns ...[2]string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"COLUMN_NAME", "DATA_TYPE", "COLUMN_TYPE"})
	for _, c := range columns {
		rows.AddRow(c[0], c[1], c[1])
	}
	return rows
}

func Test_spreadSampleQuery_PostgresUsesTheEstimate(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(pgEstimate).
		WithArgs(`"public"."users"`).
		WillReturnRows(sqlmock.NewRows([]string{"reltuples"}).AddRow(float32(200000)))

	query, ok := spreadSampleQuery(t.Context(), db, sqlmanager_shared.GoquPostgresDriver, "public", "users", 20, fixedPick)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE SYSTEM (0.5)")
}

func Test_spreadSampleQuery_NoEstimate(t *testing.T) {
	cases := map[string]func(*sqlmock.ExpectedQuery){
		"never analyzed": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"reltuples"}).AddRow(float32(-1)))
		},
		"empty": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"reltuples"}).AddRow(float32(0)))
		},
		"missing relation": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnRows(sqlmock.NewRows([]string{"reltuples"}))
		},
		"catalog error": func(q *sqlmock.ExpectedQuery) {
			q.WillReturnError(errors.New("catalog unavailable"))
		},
	}
	for name, respond := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			respond(mock.ExpectQuery(pgEstimate).WithArgs(`"public"."users"`))

			query, ok := spreadSampleQuery(t.Context(), db, sqlmanager_shared.GoquPostgresDriver, "public", "users", 20, fixedPick)

			require.False(t, ok)
			require.Empty(t, query)
		})
	}
}

func Test_spreadSampleQuery_SqlServerReadsNoCatalog(t *testing.T) {
	db, _ := newSampleDB(t) // no expectation: any query fails the test

	query, ok := spreadSampleQuery(t.Context(), db, sqlmanager_shared.MssqlDriver, "dbo", "users", 20, fixedPick)

	require.True(t, ok)
	require.Contains(t, query, "TABLESAMPLE (1000 ROWS)")
}

func Test_spreadSampleQuery_MysqlSlicesOnASingleIntegerKey(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery(mysqlKeyLookup).WithArgs("public", "users").
		WillReturnRows(keyRows([2]string{"id", "bigint"}))
	mock.ExpectQuery(mysqlBounds).
		WillReturnRows(sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(1000000)))

	query, ok := spreadSampleQuery(t.Context(), db, sqlmanager_shared.MysqlDriver, "public", "users", 20, fixedPick)

	require.True(t, ok)
	require.Equal(t, querybuilder.SampleSlices, strings.Count(query, "LIMIT 100"))
	require.Contains(t, query, "`id` >= 2")
}

func Test_spreadSampleQuery_MysqlWithoutAUsableKey(t *testing.T) {
	cases := map[string]struct {
		key    *sqlmock.Rows
		bounds *sqlmock.Rows
	}{
		"composite key": {
			key: keyRows([2]string{"a", "int"}, [2]string{"b", "int"}),
		},
		"varchar key": {
			key: keyRows([2]string{"code", "varchar"}),
		},
		"decimal key": {
			key: keyRows([2]string{"id", "decimal"}),
		},
		"no key": {
			key: keyRows(),
		},
		"empty table": {
			key:    keyRows([2]string{"id", "int"}),
			bounds: sqlmock.NewRows([]string{"min", "max"}).AddRow(nil, nil),
		},
		"unsigned bounds beyond int64": {
			key:    keyRows([2]string{"id", "bigint"}),
			bounds: sqlmock.NewRows([]string{"min", "max"}).AddRow("1", "18446744073709551615"),
		},
		"table that fits the window": {
			key:    keyRows([2]string{"id", "int"}),
			bounds: sqlmock.NewRows([]string{"min", "max"}).AddRow(int64(1), int64(500)),
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			mock.ExpectQuery(mysqlKeyLookup).WithArgs("public", "users").WillReturnRows(c.key)
			if c.bounds != nil {
				mock.ExpectQuery(mysqlBounds).WillReturnRows(c.bounds)
			}

			query, ok := spreadSampleQuery(t.Context(), db, sqlmanager_shared.MysqlDriver, "public", "users", 20, fixedPick)

			require.False(t, ok)
			require.Empty(t, query)
		})
	}
}

func Test_spreadSampleQuery_UnknownDriver(t *testing.T) {
	db, _ := newSampleDB(t)

	_, ok := spreadSampleQuery(t.Context(), db, "oracle", "public", "users", 20, fixedPick)

	require.False(t, ok)
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

func Test_readSample_ShortSpreadFallsBackOnTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))

	got, err := readSample(t.Context(), db, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 20)
	require.Equal(t, int64(100), got[0]["id"])
}

func Test_readSample_ShortSpreadIsKeptWhenTheWindowIsShorter(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(sequence(1, 8)...))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(1, 2))

	got, err := readSample(t.Context(), db, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 8)
}

func Test_readSample_FailingSpreadFallsBackOnTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("TABLESAMPLE clause can only be used with local tables"))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 20)...))

	got, err := readSample(t.Context(), db, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 20)
}

func Test_readSample_SpreadEnough(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(sequence(1, 20)...))

	got, err := readSample(t.Context(), db, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 20)
}

func Test_readSample_NoSpreadReadsTheWindowOnly(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(1, 2, 3))

	got, err := readSample(t.Context(), db, idMapper{}, "", false, "WINDOW", 20)

	require.NoError(t, err)
	require.Len(t, got, 3)
}

func Test_readSample_WindowFails(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("spread refused"))
	mock.ExpectQuery("WINDOW").WillReturnError(errors.New("window refused"))

	_, err := readSample(t.Context(), db, idMapper{}, "SPREAD", true, "WINDOW", 20)

	require.Error(t, err)
	require.EqualError(
		t,
		wrapSampleError(err, "public.users", "postgres"),
		"error querying table public.users with database type postgres: window refused",
	)
}

func Test_wrapSampleError_Mapping(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("not a number"))

	_, err := readSample(t.Context(), db, idMapper{}, "", false, "WINDOW", 20)

	require.Error(t, err)
	require.Contains(
		t,
		wrapSampleError(err, "public.users", "postgres").Error(),
		"unable to convert row to map for table public.users with database type postgres: ",
	)
}

func Test_randomInRange_StaysInTheRange(t *testing.T) {
	for range 1000 {
		got := randomInRange(-5, 2000)
		require.GreaterOrEqual(t, got, int64(-5))
		require.Less(t, got, int64(2000))
	}
}
