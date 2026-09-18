package runprivileges_activity

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

var article = &jobTable{Schema: "shop", Table: "ARTICLE", Columns: []string{"id", "libellé"}}

// The probes name the written columns and never run.
func Test_mysqlProbes(t *testing.T) {
	require.Equal(t, "SELECT `id`, `libellé` FROM `shop`.`ARTICLE` WHERE FALSE", article.mysqlSelect())
	require.Equal(t, "INSERT INTO `shop`.`ARTICLE` (`id`, `libellé`) VALUES (NULL, NULL)", article.mysqlInsert())
	require.Equal(t, "UPDATE `shop`.`ARTICLE` SET `id` = DEFAULT, `libellé` = DEFAULT WHERE FALSE", article.mysqlUpdate())
	require.Equal(t, "DELETE FROM `shop`.`ARTICLE` WHERE FALSE", article.mysqlDelete())
}

func denied(number uint16) error { return &mysql.MySQLError{Number: number, Message: "denied"} }

func expectProbe(mock sqlmock.Sqlmock, statement string, err error) {
	query := mock.ExpectQuery(regexp.QuoteMeta("EXPLAIN " + statement))
	if err != nil {
		query.WillReturnError(err)
		return
	}
	query.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
}

func expectGrants(mock sqlmock.Sqlmock, lines ...string) {
	rows := sqlmock.NewRows([]string{"Grants"})
	for _, line := range lines {
		rows.AddRow(line)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SHOW GRANTS")).WillReturnRows(rows)
}

func Test_checkMysqlSource(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectProbe(mock, article.mysqlSelect(), denied(mysqlColumnAccessDenied))

	findings, err := checkMysqlSource(context.Background(), db, "prod", []*jobTable{article})
	require.NoError(t, err)
	require.Equal(t, []string{`source "prod" cannot read shop.ARTICLE (missing SELECT)`}, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}

func Test_checkMysqlDestination_readOnlyServer(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("read_only").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(true))

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*jobTable{article}, false, false)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "read-only server")
	require.NoError(t, mock.ExpectationsWereMet(), "a read-only server says enough: nothing more is asked")
}

// What the server refuses is reported per table; TRIGGER missing means the triggers are
// out of sight, and DROP is asked only when the job empties the destination.
func Test_checkMysqlDestination_missingPrivileges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("read_only").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(false))
	expectProbe(mock, article.mysqlSelect(), nil)
	expectProbe(mock, article.mysqlInsert(), denied(mysqlTableAccessDenied))
	expectProbe(mock, article.mysqlUpdate(), denied(mysqlColumnAccessDenied))
	expectProbe(mock, article.mysqlDelete(), nil)
	expectGrants(mock, "GRANT SELECT, DELETE ON `shop`.* TO `app`@`%`")

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*jobTable{article}, false, true)
	require.NoError(t, err)
	require.Len(t, findings, 3)
	require.Equal(t, `destination "staging" cannot write shop.ARTICLE (missing INSERT, UPDATE)`, findings[0])
	require.Contains(t, findings[1], "cannot empty shop.ARTICLE before writing it (missing DROP")
	require.Contains(t, findings[2], "cannot see the triggers of shop.ARTICLE")
	require.NoError(t, mock.ExpectationsWereMet())
}

// A trigger comes back as its definer, which only SET_USER_ID lets an account name.
func Test_checkMysqlDestination_triggerDefiner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("read_only").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(false))
	for _, statement := range []string{article.mysqlSelect(), article.mysqlInsert(), article.mysqlUpdate(), article.mysqlDelete()} {
		expectProbe(mock, statement, nil)
	}
	expectGrants(mock, "GRANT SELECT, INSERT, UPDATE, DELETE, TRIGGER ON `shop`.* TO `app`@`%`")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT CURRENT_USER()")).WillReturnRows(sqlmock.NewRows([]string{"u"}).AddRow("app@%"))
	mock.ExpectQuery("information_schema.TRIGGERS").WithArgs("shop", "ARTICLE").
		WillReturnRows(sqlmock.NewRows([]string{"n", "d"}).AddRow("trg_mine", "app@%").AddRow("trg_dba", "dba@localhost"))

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*jobTable{article}, false, false)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "cannot put back the trigger trg_dba of shop.ARTICLE, whose definer is dba@localhost")
	require.NoError(t, mock.ExpectationsWereMet())
}

// A table the run creates is not there yet: it will be the account's own.
func Test_checkMysqlDestination_tableCreatedByTheRun(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("read_only").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(false))
	expectProbe(mock, article.mysqlSelect(), denied(mysqlNoSuchTable))
	expectGrants(mock)

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*jobTable{article}, true, true)
	require.NoError(t, err)
	require.Empty(t, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}
