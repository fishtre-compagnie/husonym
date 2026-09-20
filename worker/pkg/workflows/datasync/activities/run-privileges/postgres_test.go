package runprivileges_activity

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

var articles = []*jobTable{{Schema: "shop", Table: "ARTICLE", Columns: []string{"id"}}}

const readOnlyQuery = "SELECT current_setting('transaction_read_only')"

func Test_checkPostgresDestination_readOnlyServer(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("on"))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles, false, false, true)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "refuses writes")
	require.NoError(t, mock.ExpectationsWereMet(), "a read-only server says enough: nothing more is asked")
}

func Test_checkPostgresDestination_missingPrivileges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").
		WillReturnRows(sqlmock.NewRows([]string{"t", "p"}).
			AddRow("shop.ARTICLE", "DELETE").AddRow("shop.ARTICLE", "INSERT").AddRow("shop.ARTICLE", "USAGE"))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles, false, false, false)
	require.NoError(t, err)
	require.Equal(t, []string{
		`destination "dest" cannot write shop.ARTICLE (missing DELETE, INSERT, USAGE on its schema)`,
	}, findings)
	require.NoError(t, mock.ExpectationsWereMet(), "Benthos does not suspend foreign keys: nothing is probed")
}

// The probe tries the very statement Athanor writes each page with, and rolls it back.
// Refused, it names the grant that allows it.
func Test_checkPostgresDestination_cannotSuspendForeignKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"t", "p"}))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnError(errors.New(`permission denied to set parameter "session_replication_role"`))
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT current_user")).WillReturnRows(sqlmock.NewRows([]string{"u"}).AddRow("husonym"))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles, false, false, true)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "cannot suspend foreign keys")
	require.Contains(t, findings[0], `GRANT SET ON PARAMETER session_replication_role TO "husonym"`)
	require.NoError(t, mock.ExpectationsWereMet())
}

func Test_checkPostgresDestination_canSuspendForeignKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"t", "p"}))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles, false, false, true)
	require.NoError(t, err)
	require.Empty(t, findings)
	require.NoError(t, mock.ExpectationsWereMet(), "the probe is always rolled back")
}

// Emptying a table takes TRUNCATE, asked only when the job empties the destination; and a
// table holding a trigger the run must disable takes its owner, which no privilege grants.
func Test_checkPostgresDestination_truncateAndTriggers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"t", "p"}))
	mock.ExpectQuery("has_table_privilege").WithArgs(sqlmock.AnyArg(), `["TRUNCATE","USAGE"]`, false).
		WillReturnRows(sqlmock.NewRows([]string{"t", "p"}).AddRow("shop.ARTICLE", "TRUNCATE"))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}).AddRow("shop.ARTICLE", "app_owner"))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles, false, true, false)
	require.NoError(t, err)
	require.Equal(t, []string{
		`destination "dest" cannot empty shop.ARTICLE before writing it (missing TRUNCATE)`,
		`destination "dest" cannot take the triggers of shop.ARTICLE out of the way of the run: disabling a ` +
			`trigger takes the owner of the table (app_owner), a member of its role, or a superuser`,
	}, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}
