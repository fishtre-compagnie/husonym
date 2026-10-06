package connectiondata

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func readColumn(
	t *testing.T,
	db sampleQuerier,
	hasSpread bool,
	send func(map[string]any) error,
) error {
	t.Helper()
	logger, _ := capturedLogger()
	return readColumnSample(t.Context(), logger, db, idMapper{}, "SPREAD", hasSpread, "WINDOW", 20, send)
}

func Test_readColumnSample_OneDrawIsSent(t *testing.T) {
	cases := map[string]struct {
		spread    []int64
		window    []int64
		hasSpread bool
		want      []int64
	}{
		"a spread that gives enough is sent alone": {
			spread: sequence(1, 25), hasSpread: true, want: sequence(1, 20),
		},
		"a short spread is dropped for a window that gives more": {
			spread: []int64{1, 2, 3}, window: sequence(1, 10), hasSpread: true, want: sequence(1, 10),
		},
		"a window that gives as many as the spread is not preferred": {
			spread: []int64{1, 2, 3}, window: []int64{1, 2, 3}, hasSpread: true, want: []int64{1, 2, 3},
		},
		"a window that gives fewer than the spread is dropped": {
			spread: sequence(1, 8), window: []int64{50, 51}, hasSpread: true, want: sequence(1, 8),
		},
		"a spread that gives nothing leaves the window": {
			spread: []int64{}, window: []int64{7}, hasSpread: true, want: []int64{7},
		},
		"no spread reads the window only": {
			window: sequence(1, 25), want: sequence(1, 20),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db, mock := newSampleDB(t)
			if tc.hasSpread {
				mock.ExpectQuery("SPREAD").WillReturnRows(idRows(tc.spread...))
			}
			if tc.window != nil {
				mock.ExpectQuery("WINDOW").WillReturnRows(idRows(tc.window...))
			}
			sent := &sentIDs{}

			err := readColumn(t, db, tc.hasSpread, sent.send)

			require.NoError(t, err)
			require.Equal(t, tc.want, sent.ids)
		})
	}
}

func Test_readColumnSample_RefusedSpreadGivesTheWindow(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnError(errors.New("TABLESAMPLE clause can only be used with local tables"))
	mock.ExpectQuery("WINDOW").WillReturnRows(idRows(sequence(100, 5)...))
	sent := &sentIDs{}

	err := readColumn(t, db, true, sent.send)

	require.NoError(t, err)
	require.Equal(t, sequence(100, 5), sent.ids)
}

func Test_readColumnSample_FailingWindowKeepsTheSpread(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("SPREAD").WillReturnRows(idRows(1, 2, 3))
	mock.ExpectQuery("WINDOW").WillReturnError(errors.New("window refused"))
	sent := &sentIDs{}

	err := readColumn(t, db, true, sent.send)

	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, sent.ids)
}

func Test_readColumnSample_FailingWindowWithNoSpreadIsAnError(t *testing.T) {
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnError(errors.New("window refused"))
	sent := &sentIDs{}

	err := readColumn(t, db, false, sent.send)

	require.Empty(t, sent.ids)
	require.EqualError(
		t,
		wrapSampleError(err, "public.users", "postgres"),
		"error querying table public.users with database type postgres: window refused",
	)
}

func Test_readColumnSample_ErrorOfTheReceiverEndsTheSample(t *testing.T) {
	refused := errors.New("stream closed")
	db, mock := newSampleDB(t)
	mock.ExpectQuery("WINDOW").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)).AddRow(int64(2)))
	calls := 0

	err := readColumn(t, db, false, func(map[string]any) error {
		calls++
		return refused
	})

	require.Same(t, refused, wrapSampleError(err, "public.users", "postgres"))
	require.Equal(t, 1, calls)
}
