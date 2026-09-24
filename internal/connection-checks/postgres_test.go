package connectionchecks

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

var articles = []*Table{{Schema: "shop", Table: "ARTICLE", Columns: []string{"id"}}}

const readOnlyQuery = "SELECT current_setting('transaction_read_only')"

// expectAccount answers the question about the account, asked once there is a remedy to write.
func expectAccount(mock sqlmock.Sqlmock, query, account string) {
	mock.ExpectQuery(regexp.QuoteMeta(query)).WillReturnRows(sqlmock.NewRows([]string{"u"}).AddRow(account))
}

func Test_checkPostgresDestination_readOnlyServer(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("on"))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{SuspendsForeignKeys: true})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, CheckServerWritable, findings[0].Check)
	require.Contains(t, findings[0].Message, "refuses writes")
	require.Empty(t, findings[0].Remedy, "no statement makes a standby accept writes")
	require.NoError(t, mock.ExpectationsWereMet(), "a read-only server says enough: nothing more is asked")
}

func Test_checkPostgresDestination_missingPrivileges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").
		WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}).
			AddRow("shop", "ARTICLE", "DELETE").AddRow("shop", "ARTICLE", "INSERT").AddRow("shop", "ARTICLE", "USAGE"))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	expectAccount(mock, "SELECT current_user", "husonym")

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles, DestinationOptions{})
	require.NoError(t, err)
	require.Equal(t, []*Finding{{
		Check:   CheckWritable,
		Level:   Blocking,
		Table:   "shop.ARTICLE",
		Missing: []string{"DELETE", "INSERT", "USAGE on its schema"},
		Message: `destination "dest" cannot write shop.ARTICLE (missing DELETE, INSERT, USAGE on its schema)`,
		Remedy:  `GRANT USAGE ON SCHEMA "shop" TO "husonym"; GRANT DELETE, INSERT ON TABLE "shop"."ARTICLE" TO "husonym";`,
	}}, findings)
	require.NoError(t, mock.ExpectationsWereMet(), "Benthos does not suspend foreign keys: nothing is probed")
}

func Test_checkPostgresSource_nothingMissingAsksNothingMore(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))

	findings, err := checkPostgresSource(context.Background(), db, "prod", articles)
	require.NoError(t, err)
	require.Empty(t, findings)
	require.NoError(t, mock.ExpectationsWereMet(), "the account is read only for a remedy")
}

// The probe tries the very statement Athanor writes each page with, and rolls it back.
// Refused, it names the grant that allows it.
func Test_checkPostgresDestination_cannotSuspendForeignKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnError(errors.New(`permission denied to set parameter "session_replication_role"`))
	mock.ExpectRollback()
	expectAccount(mock, "SELECT current_user", "husonym")

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{SuspendsForeignKeys: true})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, CheckForeignKeySuspension, findings[0].Check)
	require.Contains(t, findings[0].Message, "cannot suspend foreign keys")
	require.Contains(t, findings[0].Message, `GRANT SET ON PARAMETER session_replication_role TO "husonym"`)
	require.Equal(t, `GRANT SET ON PARAMETER session_replication_role TO "husonym";`, findings[0].Remedy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func Test_checkPostgresDestination_canSuspendForeignKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{SuspendsForeignKeys: true})
	require.NoError(t, err)
	require.Empty(t, findings)
	require.NoError(t, mock.ExpectationsWereMet(), "the probe is always rolled back")
}

// Emptying a table takes TRUNCATE, asked only when the job empties the destination; and a
// table holding a trigger the run must disable takes its owner, which no privilege grants —
// membership in the owner's role does.
func Test_checkPostgresDestination_truncateAndTriggers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	mock.ExpectQuery("has_table_privilege").WithArgs(sqlmock.AnyArg(), `["TRUNCATE","USAGE"]`, false).
		WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}).AddRow("shop", "ARTICLE", "TRUNCATE"))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}).AddRow("shop.ARTICLE", "app_owner"))
	expectAccount(mock, "SELECT current_user", "husonym")

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{Truncates: true})
	require.NoError(t, err)
	require.Equal(t, []string{
		`destination "dest" cannot empty shop.ARTICLE before writing it (missing TRUNCATE)`,
		`destination "dest" cannot take the triggers of shop.ARTICLE out of the way of the run: disabling a ` +
			`trigger takes the owner of the table (app_owner), a member of its role, or a superuser`,
	}, Messages(findings))
	require.Equal(t, `GRANT TRUNCATE ON TABLE "shop"."ARTICLE" TO "husonym";`, findings[0].Remedy)
	require.Equal(t, `GRANT "app_owner" TO "husonym";`, findings[1].Remedy)
	require.NoError(t, mock.ExpectationsWereMet())
}
