package connectionchecks

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func Test_majorOf(t *testing.T) {
	for _, tt := range []struct {
		engine, raw, want string
	}{
		{enginePostgres, "160004", "16"},
		{enginePostgres, "90624", "9"},
		{enginePostgres, "0", ""},
		{enginePostgres, "16.4", ""},
		{enginePostgres, "", ""},
		{enginePostgres, "99990000", ""},
		{engineMysql, "8.0.36-log", "8.0"},
		{engineMysql, "8.4.2", "8.4"},
		{engineMysql, "10.11.6-MariaDB-1:10.11.6+maria~ubu2204", "10.11"},
		{engineMysql, "8", ""},
		{engineMysql, "eight.zero", ""},
		{engineMysql, "8.0; drop", "8.0"},
		{engineMysql, "8.0000", ""},
		{engineMysql, "", ""},
		{"oracle", "19.3", ""},
		{"", "160004", ""},
	} {
		t.Run(tt.engine+" "+tt.raw, func(t *testing.T) {
			require.Equal(t, tt.want, majorOf(tt.engine, tt.raw))
		})
	}
}

func Test_VersionMajor(t *testing.T) {
	const (
		postgresQuery = "SELECT current_setting('server_version_num')::int"
		mysqlQuery    = "SELECT VERSION()"
	)

	t.Run("postgres", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery(regexp.QuoteMeta(postgresQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(160004))

		require.Equal(t, "16", VersionMajor(context.Background(), db, Postgres))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("mysql", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery(regexp.QuoteMeta(mysqlQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("8.0.36-log"))

		require.Equal(t, "8.0", VersionMajor(context.Background(), db, MySQL))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("a read that fails tells nothing", func(t *testing.T) {
		for _, tt := range []struct {
			dialect Dialect
			query   string
		}{{Postgres, postgresQuery}, {MySQL, mysqlQuery}} {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			mock.ExpectQuery(regexp.QuoteMeta(tt.query)).WillReturnError(errors.New("connection lost"))

			require.Empty(t, VersionMajor(context.Background(), db, tt.dialect))
			require.NoError(t, mock.ExpectationsWereMet())
		}
	})

	t.Run("another database is not asked", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)

		require.Empty(t, VersionMajor(context.Background(), db, Dialect(0)))
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
