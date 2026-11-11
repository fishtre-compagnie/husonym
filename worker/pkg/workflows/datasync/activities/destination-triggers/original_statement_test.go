package destinationtriggers_activity

import (
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// A MySQL trigger read with its original statement comes back from it, unchanged, in the
// database of its table, under its sql_mode and character sets: SHOW CREATE TRIGGER gives the
// same text afterwards. Whether it is back already is asked first, on every server, since IF NOT
// EXISTS would be kept in the text.
func TestToTriggers_MysqlRestoresTheOriginalStatement(t *testing.T) {
	for _, version := range []string{"5.7.42", "8.4.11", "11.4.13-MariaDB"} {
		found := mysqlFound("trg", 1, nil)
		found.Mysql.CharacterSetClient = "utf8mb4"
		found.Mysql.OriginalStatement = "CREATE DEFINER=`app`@`%` TRIGGER `trg` AFTER INSERT ON `COMMANDE` FOR EACH ROW\n" +
			"INSERT INTO h VALUES (NEW.id)"
		triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, version, []*sqlmanager_shared.TableTrigger{found})
		require.NoError(t, err)
		require.Len(t, triggers, 1)
		trigger := triggers[0]
		require.Equal(t, "web", trigger.Database)
		require.Equal(t,
			"SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = 'web' AND TRIGGER_NAME = 'trg'",
			trigger.Exists)
		require.Equal(t, []string{
			"SET @husonym_sql_mode = @@SESSION.sql_mode, @husonym_collation = @@SESSION.collation_connection, " +
				"@husonym_character_set_client = @@SESSION.character_set_client",
			"SET SESSION sql_mode = 'NO_ENGINE_SUBSTITUTION', collation_connection = 'utf8mb4_bin', " +
				"character_set_client = 'utf8mb4'",
			found.Mysql.OriginalStatement,
		}, trigger.Restore)
		require.Equal(t,
			"SET SESSION sql_mode = @husonym_sql_mode, collation_connection = @husonym_collation, "+
				"character_set_client = @husonym_character_set_client",
			trigger.Reset)
	}
}
