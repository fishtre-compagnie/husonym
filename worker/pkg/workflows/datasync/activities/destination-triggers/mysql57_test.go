package destinationtriggers_activity

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// MySQL before 8.0.29 takes no CREATE TRIGGER IF NOT EXISTS: the trigger is created
// without it, behind a count of the triggers bearing its name.
func TestToTriggers_MysqlWithoutCreateIfNotExists(t *testing.T) {
	for _, version := range []string{"5.7.42-log", "8.0.28"} {
		t.Run(version, func(t *testing.T) {
			triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, version,
				[]*sqlmanager_shared.TableTrigger{mysqlFound("trg-a`b", 1, nil)})
			require.NoError(t, err)
			require.Len(t, triggers, 1)
			trigger := triggers[0]
			require.Equal(t, "DROP TRIGGER IF EXISTS `web`.`trg-a``b`", trigger.Suspend)
			require.Equal(t,
				"SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = 'web' AND TRIGGER_NAME = 'trg-a`b'",
				trigger.Exists)
			require.Equal(t, []string{
				"SET @husonym_sql_mode = @@SESSION.sql_mode, @husonym_collation = @@SESSION.collation_connection",
				"SET SESSION sql_mode = 'NO_ENGINE_SUBSTITUTION', collation_connection = 'utf8mb4_bin'",
				"CREATE DEFINER = `app`@`%` TRIGGER `web`.`trg-a``b` AFTER INSERT ON `web`.`COMMANDE` " +
					"FOR EACH ROW INSERT INTO h VALUES (NEW.id)",
			}, trigger.Restore)
			require.Equal(t, "SET SESSION sql_mode = @husonym_sql_mode, collation_connection = @husonym_collation", trigger.Reset)
		})
	}

	t.Run("the trigger schema is the one counted in", func(t *testing.T) {
		own := "autre"
		triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, "5.7.42-log",
			[]*sqlmanager_shared.TableTrigger{mysqlFound("t", 1, &own)})
		require.NoError(t, err)
		require.Contains(t, triggers[0].Exists, "TRIGGER_SCHEMA = 'autre'")
	})

	t.Run("servers that take it keep IF NOT EXISTS and no count", func(t *testing.T) {
		for _, version := range []string{"8.0.29", "8.4.3", "10.11.6-MariaDB", ""} {
			triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, version,
				[]*sqlmanager_shared.TableTrigger{mysqlFound("t", 1, nil)})
			require.NoError(t, err)
			require.Empty(t, triggers[0].Exists, version)
			require.Contains(t, triggers[0].Restore[2], "TRIGGER IF NOT EXISTS", version)
		}
	})
}

func guardedTrigger() *Trigger {
	return &Trigger{
		Schema: "web", Table: "COMMANDE", Name: "t",
		Exists:  "SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = 'web' AND TRIGGER_NAME = 't'",
		Restore: []string{"SET @a = 1", "CREATE TRIGGER `web`.`t` AFTER INSERT ON `web`.`COMMANDE` FOR EACH ROW SET @b = 1"},
		Reset:   "SET @a = NULL",
	}
}

// A trigger the count finds is back already: nothing is run.
func TestRestoreOne_SkipsATriggerAlreadyBack(t *testing.T) {
	db, sqlMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()
	trigger := guardedTrigger()
	sqlMock.ExpectBegin()
	sqlMock.ExpectQuery(trigger.Exists).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(1))
	sqlMock.ExpectCommit()

	require.NoError(t, restoreOne(context.Background(), db, trigger))
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// A trigger the count does not find is created, and the session given back its settings.
func TestRestoreOne_CreatesATriggerNotThere(t *testing.T) {
	db, sqlMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()
	trigger := guardedTrigger()
	sqlMock.ExpectBegin()
	sqlMock.ExpectQuery(trigger.Exists).WillReturnRows(sqlmock.NewRows([]string{"COUNT(*)"}).AddRow(0))
	for _, stmt := range trigger.Restore {
		sqlMock.ExpectExec(stmt).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	sqlMock.ExpectExec(trigger.Reset).WillReturnResult(sqlmock.NewResult(0, 0))
	sqlMock.ExpectCommit()

	require.NoError(t, restoreOne(context.Background(), db, trigger))
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// A count that fails stops the restore before anything is run.
func TestRestoreOne_CountFails(t *testing.T) {
	db, sqlMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()
	trigger := guardedTrigger()
	sqlMock.ExpectBegin()
	sqlMock.ExpectQuery(trigger.Exists).WillReturnError(errors.New("boom"))
	sqlMock.ExpectRollback()

	require.ErrorContains(t, restoreOne(context.Background(), db, trigger), "boom")
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

// A trigger recorded without a count, by MySQL 8.0.29+ or by an earlier version of the
// worker, is restored as before.
func TestRestoreOne_WithoutCount(t *testing.T) {
	db, sqlMock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()
	trigger := guardedTrigger()
	trigger.Exists = ""
	sqlMock.ExpectBegin()
	for _, stmt := range trigger.Restore {
		sqlMock.ExpectExec(stmt).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	sqlMock.ExpectExec(trigger.Reset).WillReturnResult(sqlmock.NewResult(0, 0))
	sqlMock.ExpectCommit()

	require.NoError(t, restoreOne(context.Background(), db, trigger))
	require.NoError(t, sqlMock.ExpectationsWereMet())
}

func TestServerVersion(t *testing.T) {
	db, sqlMock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	sqlMock.ExpectQuery(regexp.QuoteMeta("SELECT VERSION()")).
		WillReturnRows(sqlmock.NewRows([]string{"VERSION()"}).AddRow("5.7.42-log"))
	version, err := serverVersion(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "5.7.42-log", version)
}
