package mssql_queries

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type pair struct {
	ID   int64
	Name string
}

func scanPair(rows *sql.Rows, p *pair) error {
	return rows.Scan(&p.ID, &p.Name)
}

func Test_queryRows(t *testing.T) {
	t.Parallel()

	t.Run("gives one item per row, in order", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		mock.ExpectQuery("SELECT").WithArgs("x").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a").AddRow(2, "b"))

		items, err := queryRows(t.Context(), db, "SELECT", scanPair, "x")

		require.NoError(t, err)
		require.Equal(t, []*pair{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}, items)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("gives an empty list, not nil, for no row", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		mock.ExpectQuery("SELECT").WillReturnRows(sqlmock.NewRows([]string{"id", "name"}))

		items, err := queryRows(t.Context(), db, "SELECT", scanPair)

		require.NoError(t, err)
		require.NotNil(t, items)
		require.Empty(t, items)
	})

	t.Run("gives the error of the query", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		failure := errors.New("permission denied")
		mock.ExpectQuery("SELECT").WillReturnError(failure)

		_, err = queryRows(t.Context(), db, "SELECT", scanPair)

		require.ErrorIs(t, err, failure)
	})

	t.Run("gives the error of a row that does not scan", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow("not a number", "a"))

		_, err = queryRows(t.Context(), db, "SELECT", scanPair)

		require.Error(t, err)
	})

	t.Run("gives the error that ends the rows", func(t *testing.T) {
		t.Parallel()
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer db.Close()
		failure := errors.New("connection lost")
		mock.ExpectQuery("SELECT").
			WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a").AddRow(2, "b").RowError(1, failure))

		_, err = queryRows(t.Context(), db, "SELECT", scanPair)

		require.ErrorIs(t, err, failure)
	})
}

func Test_jsonParam(t *testing.T) {
	t.Parallel()

	t.Run("a list of ids is one parameter", func(t *testing.T) {
		t.Parallel()
		param, err := jsonParam("ids", []int64{3, 1, 2})
		require.NoError(t, err)
		require.Equal(t, "ids", param.Name)
		require.Equal(t, "[3,1,2]", param.Value)
	})

	t.Run("an empty list is an empty array", func(t *testing.T) {
		t.Parallel()
		empty, err := jsonParam("ids", []int64{})
		require.NoError(t, err)
		require.Equal(t, "[]", empty.Value)
		none, err := jsonParam("ids", []int64(nil))
		require.NoError(t, err)
		require.Equal(t, "[]", none.Value)
	})

	t.Run("names keep their quotes, brackets, dots and commas", func(t *testing.T) {
		t.Parallel()
		param, err := jsonParam("schemas", []string{`it's`, `a.b`, `we]ird`, `x, y`, `say "hi"`, `back\slash`})
		require.NoError(t, err)
		require.Equal(t, `["it's","a.b","we]ird","x, y","say \"hi\"","back\\slash"]`, param.Value)
	})
}
