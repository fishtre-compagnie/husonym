package connectionchecks

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

var articles = []*Table{{Schema: "shop", Table: "ARTICLE", Columns: []string{"id"}}}

const readOnlyQuery = "SELECT current_setting('transaction_read_only')"

// expectColumns answers the question about the columns of the tables: none is absent.
func expectColumns(mock sqlmock.Sqlmock, absent ...[3]string) {
	rows := sqlmock.NewRows([]string{"s", "t", "c"})
	for _, row := range absent {
		rows.AddRow(row[0], row[1], row[2])
	}
	mock.ExpectQuery("pg_attribute").WillReturnRows(rows)
}

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
	expectColumns(mock)
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
	expectColumns(mock)
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnError(&pgconn.PgError{
			Code:    pgInsufficientPrivilege,
			Message: `permission denied to set parameter "session_replication_role"`,
		})
	mock.ExpectRollback()
	expectAccount(mock, "SELECT current_user", "husonym")
	mock.ExpectQuery(regexp.QuoteMeta("server_version_num")).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(160004))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{SuspendsForeignKeys: true})
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Equal(t, CheckForeignKeySuspension, findings[0].Check)
	require.Contains(t, findings[0].Message, "cannot suspend foreign keys")
	require.NotContains(t, findings[0].Message, "permission denied", "the server's words are never quoted")
	require.NotContains(t, findings[0].Message, "make the account a superuser", "no remedy hands out more than asked")
	require.Contains(t, findings[0].Message, `GRANT SET ON PARAMETER session_replication_role TO "husonym"`)
	require.Equal(t, `GRANT SET ON PARAMETER session_replication_role TO "husonym";`, findings[0].Remedy)
	require.NoError(t, mock.ExpectationsWereMet())
}

// A probe that fails for another reason than a privilege — the connection lost, a timeout,
// a pooler — says nothing of the account: it is an error, never a finding quoting the driver.
func Test_checkPostgresDestination_foreignKeysProbeFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	expectColumns(mock)
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnError(errors.New("write tcp 10.0.3.4:41234->10.0.9.12:5432: broken pipe"))
	mock.ExpectRollback()

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{SuspendsForeignKeys: true})
	require.Error(t, err)
	require.Empty(t, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}

func Test_checkPostgresDestination_canSuspendForeignKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	expectColumns(mock)
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
	expectColumns(mock)
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
	require.Empty(t, findings[1].Remedy, "the owner's role would hand the account all it owns")
	require.NoError(t, mock.ExpectationsWereMet())
}

// Before PostgreSQL 15 no grant lets an account set session_replication_role: no remedy.
func Test_checkPostgresDestination_foreignKeysBefore15(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	expectColumns(mock)
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SET LOCAL session_replication_role = replica")).
		WillReturnError(&pgconn.PgError{Code: pgInsufficientPrivilege, Message: "permission denied"})
	mock.ExpectRollback()
	expectAccount(mock, "SELECT current_user", "husonym")
	mock.ExpectQuery(regexp.QuoteMeta("server_version_num")).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(140011))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{SuspendsForeignKeys: true})
	require.NoError(t, err)
	require.Empty(t, findings[0].Remedy)
}

// A table that is not there lacks nothing a grant could give: it is reported absent.
func Test_checkPostgresSource_absentTable(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("has_table_privilege").
		WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}).AddRow("shop", "ARTICLE", absentTable))
	expectAccount(mock, "SELECT current_user", "husonym")

	findings, err := checkPostgresSource(context.Background(), db, "prod", articles)
	require.NoError(t, err)
	require.Equal(t, []*Finding{{
		Check: CheckTableExists, Level: Blocking, Table: "shop.ARTICLE",
		Message: `source "prod" has no table shop.ARTICLE`,
	}}, findings)
}

// A column the run writes and the table lacks stops the run at its first INSERT: it is
// reported absent, in the order the job gives its columns, and no grant would add it.
func Test_checkPostgresDestination_absentColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	mock.ExpectQuery("pg_attribute").
		WithArgs(`[{"schema_name":"shop","table_name":"ARTICLE","columns":["id","libelle","Prix"]}]`).
		WillReturnRows(sqlmock.NewRows([]string{"s", "t", "c"}).
			AddRow("shop", "ARTICLE", "libelle").AddRow("shop", "ARTICLE", "Prix"))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))
	expectAccount(mock, "SELECT current_user", "husonym")

	article := []*Table{{Schema: "shop", Table: "ARTICLE", Columns: []string{"id", "libelle", "Prix"}}}
	findings, err := checkPostgresDestination(context.Background(), db, "dest", article, DestinationOptions{})
	require.NoError(t, err)
	require.Equal(t, []*Finding{{
		Check:   CheckTableExists,
		Level:   Blocking,
		Table:   "shop.ARTICLE",
		Missing: []string{"libelle", "Prix"},
		Message: `destination "dest" has no column libelle, Prix in shop.ARTICLE`,
	}}, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}

// A run that creates the tables and columns the destination lacks is not stopped by one it
// will add: the columns are not asked about.
func Test_checkPostgresDestination_createsTablesAsksNoColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta(readOnlyQuery)).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("off"))
	mock.ExpectQuery("has_table_privilege").WillReturnRows(sqlmock.NewRows([]string{"s", "t", "p"}))
	mock.ExpectQuery("pg_has_role").WillReturnRows(sqlmock.NewRows([]string{"t", "o"}))

	findings, err := checkPostgresDestination(context.Background(), db, "dest", articles,
		DestinationOptions{CreatesTables: true})
	require.NoError(t, err)
	require.Empty(t, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Tables given without columns — the server checked as a whole, or a caller that does not
// know them — have no column to look for.
func Test_postgresAbsentColumns_withoutColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	absent, err := postgresAbsentColumns(context.Background(), db,
		[]*Table{{Schema: "shop", Table: "ARTICLE"}})
	require.NoError(t, err)
	require.Empty(t, absent)
	require.NoError(t, mock.ExpectationsWereMet(), "nothing is asked")
}
