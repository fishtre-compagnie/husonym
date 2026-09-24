package connectionchecks

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

var article = &Table{Schema: "shop", Table: "ARTICLE", Columns: []string{"id", "libellé"}}

// The probes name the written columns and never run.
func Test_mysqlProbes(t *testing.T) {
	require.Equal(t, "SELECT `id`, `libellé` FROM `shop`.`ARTICLE` WHERE FALSE", article.mysqlSelect())
	require.Equal(t, "INSERT INTO `shop`.`ARTICLE` (`id`, `libellé`) VALUES (NULL, NULL)", article.mysqlInsert())
	require.Equal(t, "UPDATE `shop`.`ARTICLE` SET `id` = NULL, `libellé` = NULL WHERE FALSE", article.mysqlUpdate())
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

// expectReadOnly answers the question about read_only with the variables given, as SHOW
// GLOBAL VARIABLES lists them.
func expectReadOnly(mock sqlmock.Sqlmock, variables ...[2]string) {
	rows := sqlmock.NewRows([]string{"Variable_name", "Value"})
	for _, variable := range variables {
		rows.AddRow(variable[0], variable[1])
	}
	mock.ExpectQuery("read_only").WillReturnRows(rows)
}

// writable is what a MySQL server accepting writes answers.
var writable = [][2]string{{"read_only", "OFF"}, {"super_read_only", "OFF"}}

// expectCurrentUser answers the question about the account, asked once there is a remedy
// to write, or a definer to compare.
func expectCurrentUser(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT CURRENT_USER()")).WillReturnRows(sqlmock.NewRows([]string{"u"}).AddRow("app@%"))
}

func expectGrants(mock sqlmock.Sqlmock, lines ...string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT @@lower_case_table_names")).
		WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(0))
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
	expectCurrentUser(mock)

	findings, err := checkMysqlSource(context.Background(), db, "prod", []*Table{article})
	require.NoError(t, err)
	require.Equal(t, []*Finding{{
		Check:   CheckReadable,
		Level:   Blocking,
		Table:   "shop.ARTICLE",
		Missing: []string{"SELECT"},
		Message: `source "prod" cannot read shop.ARTICLE (missing SELECT)`,
		Remedy:  "GRANT SELECT ON `shop`.`ARTICLE` TO 'app'@'%';",
	}}, findings)
	require.NoError(t, mock.ExpectationsWereMet())
}

// MariaDB sends the refusal of an EXPLAIN SELECT after the header of its result.
func Test_checkMysqlSource_refusalAfterTheHeader(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("EXPLAIN " + article.mysqlSelect())).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).RowError(0, denied(mysqlColumnAccessDenied)))
	expectCurrentUser(mock)

	findings, err := checkMysqlSource(context.Background(), db, "prod", []*Table{article})
	require.NoError(t, err)
	require.Equal(t, []string{`source "prod" cannot read shop.ARTICLE (missing SELECT)`}, Messages(findings))
	require.NoError(t, mock.ExpectationsWereMet())
}

// MariaDB has no super_read_only: read_only answers alone.
func Test_checkMysqlDestination_readOnlyServer(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectReadOnly(mock, [2]string{"read_only", "ON"})

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*Table{article}, false, false)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Message, "read-only server")
	require.Empty(t, findings[0].Remedy)
	require.NoError(t, mock.ExpectationsWereMet(), "a read-only server says enough: nothing more is asked")
}

// What the server refuses is reported per table; TRIGGER missing means the triggers are
// out of sight, and DROP is asked only when the job empties the destination.
func Test_checkMysqlDestination_missingPrivileges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectReadOnly(mock, writable...)
	expectProbe(mock, article.mysqlSelect(), nil)
	expectProbe(mock, article.mysqlInsert(), denied(mysqlTableAccessDenied))
	expectProbe(mock, article.mysqlUpdate(), denied(mysqlColumnAccessDenied))
	expectProbe(mock, article.mysqlDelete(), nil)
	expectCurrentUser(mock)
	expectGrants(mock, "GRANT SELECT, DELETE ON `shop`.* TO `app`@`%`")

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*Table{article}, false, true)
	require.NoError(t, err)
	require.Len(t, findings, 3)
	require.Equal(t, `destination "staging" cannot write shop.ARTICLE (missing INSERT, UPDATE)`, findings[0].Message)
	require.Equal(t, "GRANT INSERT, UPDATE ON `shop`.`ARTICLE` TO 'app'@'%';", findings[0].Remedy)
	require.Contains(t, findings[1].Message, "cannot empty shop.ARTICLE before writing it (missing DROP")
	require.Equal(t, "GRANT DROP ON `shop`.`ARTICLE` TO 'app'@'%';", findings[1].Remedy)
	require.Contains(t, findings[2].Message, "cannot see the triggers of shop.ARTICLE")
	require.Equal(t, "GRANT TRIGGER ON `shop`.`ARTICLE` TO 'app'@'%';", findings[2].Remedy)
	require.NoError(t, mock.ExpectationsWereMet())
}

// A trigger comes back as its definer, which only SET_USER_ID lets an account name.
func Test_checkMysqlDestination_triggerDefiner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectReadOnly(mock, writable...)
	for _, statement := range []string{article.mysqlSelect(), article.mysqlInsert(), article.mysqlUpdate(), article.mysqlDelete()} {
		expectProbe(mock, statement, nil)
	}
	expectGrants(mock, "GRANT SELECT, INSERT, UPDATE, DELETE, TRIGGER ON `shop`.* TO `app`@`%`")
	expectCurrentUser(mock)
	mock.ExpectQuery("information_schema.TRIGGERS").WithArgs("shop", "ARTICLE").
		WillReturnRows(sqlmock.NewRows([]string{"n", "d"}).AddRow("trg_mine", "app@%").AddRow("trg_dba", "dba@localhost"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT VERSION()")).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow("8.4.2"))

	findings, err := checkMysqlDestination(context.Background(), db, "staging", []*Table{article}, false, false)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0].Message, "cannot put back the trigger trg_dba of shop.ARTICLE, whose definer is dba@localhost")
	require.Equal(t, "GRANT SET_ANY_DEFINER ON *.* TO 'app'@'%';", findings[0].Remedy, "the account is read once")
	require.NoError(t, mock.ExpectationsWereMet())
}

// A column the source gained is added by a run reconciling the schema: the probes ask about
// the columns the destination has. Without reconciliation, the column is missing.
func Test_checkMysqlDestination_columnAddedByTheRun(t *testing.T) {
	has := &Table{Schema: "shop", Table: "ARTICLE", Columns: []string{"id"}}
	for _, createsTables := range []bool{true, false} {
		t.Run(fmt.Sprint(createsTables), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			expectReadOnly(mock, writable...)
			expectProbe(mock, article.mysqlSelect(), denied(mysqlNoSuchColumn))
			mock.ExpectQuery("information_schema.COLUMNS").WithArgs("shop", "ARTICLE").
				WillReturnRows(sqlmock.NewRows([]string{"COLUMN_NAME"}).AddRow("ID"))
			for _, statement := range []string{has.mysqlSelect(), has.mysqlInsert(), has.mysqlUpdate(), has.mysqlDelete()} {
				expectProbe(mock, statement, nil)
			}
			expectGrants(mock, "GRANT ALL PRIVILEGES ON `shop`.* TO `app`@`%`")
			expectCurrentUser(mock)
			mock.ExpectQuery("information_schema.TRIGGERS").WithArgs("shop", "ARTICLE").
				WillReturnRows(sqlmock.NewRows([]string{"n", "d"}))

			findings, err := checkMysqlDestination(context.Background(), db, "staging", []*Table{article}, createsTables, false)
			require.NoError(t, err)
			if createsTables {
				require.Empty(t, findings)
			} else {
				require.Equal(t, []string{`destination "staging" has no column libellé in shop.ARTICLE that the account can see`}, Messages(findings))
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// A table the run creates is not there yet, nor maybe its database, which MySQL says with
// an error of its own: it will be the account's own.
func Test_checkMysqlDestination_tableCreatedByTheRun(t *testing.T) {
	for name, number := range map[string]uint16{"table": mysqlNoSuchTable, "database": mysqlNoSuchDatabase} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			expectReadOnly(mock, writable...)
			expectProbe(mock, article.mysqlSelect(), denied(number))
			expectGrants(mock)

			findings, err := checkMysqlDestination(context.Background(), db, "staging", []*Table{article}, true, true)
			require.NoError(t, err)
			require.Empty(t, findings)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// The privilege that lets an account name another as a trigger's definer changed name.
func Test_mysqlDefinerPrivilege(t *testing.T) {
	for version, privilege := range map[string]string{
		"8.0.36":                 "SET_USER_ID",
		"8.2.0":                  "SET_ANY_DEFINER",
		"9.1.0":                  "SET_ANY_DEFINER",
		"11.4.2-MariaDB-ubu2404": "SET USER",
	} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT VERSION()")).WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(version))
		require.Equal(t, privilege, mysqlDefinerPrivilege(context.Background(), db), version)
		db.Close()
	}
}

func Test_mysqlAccount_quoted(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT CURRENT_USER()")).WillReturnRows(sqlmock.NewRows([]string{"u"}).AddRow("o'brien@10.0.%"))
	account := &mysqlAccount{db: db}
	require.Equal(t, `'o\'brien'@'10.0.%'`, account.quoted(context.Background()))
	require.Equal(t, `'o\'brien'@'10.0.%'`, account.quoted(context.Background()), "read once")
	require.NoError(t, mock.ExpectationsWereMet())
}
