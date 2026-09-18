package destinationtriggers_activity

import (
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func found(triggerSchema *string) []*sqlmanager_shared.TableTrigger {
	return []*sqlmanager_shared.TableTrigger{{
		Schema: "web", Table: "COMMANDE", TriggerName: "trg_histo", TriggerSchema: triggerSchema,
		Definition: "CREATE TRIGGER IF NOT EXISTS web.trg_histo AFTER INSERT ON `web`.`COMMANDE` FOR EACH ROW …",
	}}
}

// A trigger is dropped from the schema holding it, which is not always the one of the
// table it fires on.
func TestToTriggers_TriggerSchema(t *testing.T) {
	own := "autre"
	triggers := toTriggers(sqlmanager_shared.MysqlDriver, found(&own))
	require.Len(t, triggers, 1)
	require.Equal(t, "autre", triggers[0].Schema)
	require.Equal(t, "COMMANDE", triggers[0].Table)
	require.Equal(t, "DROP TRIGGER IF EXISTS `autre`.`trg_histo`", triggers[0].DropStatement())
	require.Contains(t, triggers[0].Create, "CREATE TRIGGER")

	empty := ""
	for _, schema := range []*string{nil, &empty} {
		triggers = toTriggers(sqlmanager_shared.MysqlDriver, found(schema))
		require.Len(t, triggers, 1)
		require.Equal(t, "web", triggers[0].Schema, "the table schema stands in for a missing trigger schema")
	}
}

// PostgreSQL and SQL Server can disable a trigger without dropping it: nothing is taken
// away there.
func TestToTriggers_OnlyMysql(t *testing.T) {
	own := "web"
	require.Empty(t, toTriggers(sqlmanager_shared.PostgresDriver, found(&own)))
	require.Empty(t, toTriggers(sqlmanager_shared.MssqlDriver, found(&own)))
}

func TestQuoteMysql(t *testing.T) {
	require.Equal(t, "`tri``cky`", quoteMysql("tri`cky"))
}
