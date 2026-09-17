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

// A page written without foreign key checks runs in one transaction, and checks are
// turned back on before the connection returns to the pool.
func TestInTransaction_ForeignKeyChecksDisabled(t *testing.T) {
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

	ctx := context.Background()
	require.NoError(t, InTransaction(ctx, db, MySQLDialect{}, true, func(tx Execer) error {
		w := NewSQLWriter(ctx, tx, MySQLDialect{}, "web", "users")
		return w.WriteBatch([]string{"id", "manager_id"}, [][]any{{int64(1), int64(2)}, {int64(2), nil}})
	}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInTransaction_ForeignKeyChecksRestoredOnFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET FOREIGN_KEY_CHECKS=0")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("duplicate entry"))
	mock.ExpectExec(regexp.QuoteMeta("SET FOREIGN_KEY_CHECKS=1")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	ctx := context.Background()
	err = InTransaction(ctx, db, MySQLDialect{}, true, func(tx Execer) error {
		return NewSQLWriter(ctx, tx, MySQLDialect{}, "web", "users").WriteBatch([]string{"id"}, [][]any{{int64(1)}})
	})
	require.ErrorContains(t, err, "duplicate entry")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInTransaction_ForeignKeyChecksUnsupportedDialect(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	require.Error(t, InTransaction(context.Background(), db, PostgresDialect{}, true, func(Execer) error { return nil }))
}

// Without foreign key checks to turn off, a page is still written whole or not at all:
// several batches commit together, and a failing one leaves nothing behind.
func TestInTransaction_PageIsAtomic(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO").WillReturnError(errors.New("bench: injected failure"))
	mock.ExpectRollback()

	ctx := context.Background()
	err = InTransaction(ctx, db, PostgresDialect{}, false, func(tx Execer) error {
		w := NewSQLWriter(ctx, tx, PostgresDialect{}, "public", "journal")
		if err := w.WriteBatch([]string{"message"}, [][]any{{"lot 1"}}); err != nil {
			return err
		}
		return w.WriteBatch([]string{"message"}, [][]any{{"lot 2"}})
	})
	require.ErrorContains(t, err, "injected failure")
	require.NoError(t, mock.ExpectationsWereMet())
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
