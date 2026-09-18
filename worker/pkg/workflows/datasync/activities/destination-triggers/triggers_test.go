package destinationtriggers_activity

import (
	"encoding/json"
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
	require.Equal(t, "DROP TRIGGER IF EXISTS `autre`.`trg_histo`", triggers[0].Suspend)
	require.Contains(t, triggers[0].Restore, "CREATE TRIGGER")

	empty := ""
	for _, schema := range []*string{nil, &empty} {
		triggers = toTriggers(sqlmanager_shared.MysqlDriver, found(schema))
		require.Len(t, triggers, 1)
		require.Equal(t, "web", triggers[0].Schema, "the table schema stands in for a missing trigger schema")
	}
}

// PostgreSQL disables a trigger instead of dropping it, and puts back the state it was in.
// A trigger the user had disabled is left alone: enabling it after the run would undo that.
func TestToTriggers_Postgres(t *testing.T) {
	var found []*sqlmanager_shared.TableTrigger
	for name, state := range map[string]string{"origine": "O", "replica": "R", "toujours": "A", "coupe": "D"} {
		found = append(found, &sqlmanager_shared.TableTrigger{
			Schema: "web", Table: "COMMANDE", TriggerName: name, EnabledState: state,
		})
	}
	restore := map[string]string{}
	for _, trigger := range toTriggers(sqlmanager_shared.PostgresDriver, found) {
		require.Equal(t, `ALTER TABLE "web"."COMMANDE" DISABLE TRIGGER "`+trigger.Name+`"`, trigger.Suspend)
		restore[trigger.Name] = trigger.Restore
	}
	require.Equal(t, map[string]string{
		"origine":  `ALTER TABLE "web"."COMMANDE" ENABLE TRIGGER "origine"`,
		"replica":  `ALTER TABLE "web"."COMMANDE" ENABLE REPLICA TRIGGER "replica"`,
		"toujours": `ALTER TABLE "web"."COMMANDE" ENABLE ALWAYS TRIGGER "toujours"`,
	}, restore)
}

func TestToTriggers_SqlServerLeftAlone(t *testing.T) {
	own := "dbo"
	require.Empty(t, toTriggers(sqlmanager_shared.MssqlDriver, found(&own)))
}

// A record written when a trigger could only be dropped and recreated still restores it.
func TestTrigger_readsOlderRecords(t *testing.T) {
	var trigger Trigger
	require.NoError(t, json.Unmarshal([]byte(`{"schema":"web","name":"t","table":"C","create":"CREATE TRIGGER …"}`), &trigger))
	require.Equal(t, "CREATE TRIGGER …", trigger.Restore)
}

func TestQuoteMysql(t *testing.T) {
	require.Equal(t, "`tri``cky`", quoteMysql("tri`cky"))
}
