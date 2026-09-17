package sqlio

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// A batch written without foreign key checks runs in one transaction, and checks are
// turned back on before the connection returns to the pool.
func TestSQLWriter_ForeignKeyChecksDisabled(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET FOREIGN_KEY_CHECKS=0")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `web`.`users` (`id`, `manager_id`) VALUES (?, ?), (?, ?)")).
		WithArgs(int64(1), int64(2), int64(2), nil).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("SET FOREIGN_KEY_CHECKS=1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	w := NewSQLWriter(context.Background(), db, MySQLDialect{}, "web", "users", WithForeignKeyChecksDisabled(db))
	require.NoError(t, w.WriteBatch([]string{"id", "manager_id"}, [][]any{{int64(1), int64(2)}, {int64(2), nil}}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLWriter_ForeignKeyChecksRestoredOnFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET FOREIGN_KEY_CHECKS=0")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("duplicate entry"))
	mock.ExpectExec(regexp.QuoteMeta("SET FOREIGN_KEY_CHECKS=1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	w := NewSQLWriter(context.Background(), db, MySQLDialect{}, "web", "users", WithForeignKeyChecksDisabled(db))
	err = w.WriteBatch([]string{"id"}, [][]any{{int64(1)}})
	require.ErrorContains(t, err, "duplicate entry")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSQLWriter_ForeignKeyChecksUnsupportedDialect(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	w := NewSQLWriter(context.Background(), db, PostgresDialect{}, "public", "users", WithForeignKeyChecksDisabled(db))
	require.Error(t, w.WriteBatch([]string{"id"}, [][]any{{int64(1)}}))
}

func TestToDriverValue(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in, want any
	}{
		{nil, nil},
		{"texte", "texte"},
		{int64(4), int64(4)},
		{[]byte{0xff}, []byte{0xff}},
		{now, now},
		{map[string]any{"a": 1}, []byte(`{"a":1}`)},
		{[]any{"x", 2}, []byte(`["x",2]`)},
	}
	for _, c := range cases {
		got, err := toDriverValue(c.in)
		require.NoError(t, err)
		require.Equal(t, c.want, got)
	}
}
