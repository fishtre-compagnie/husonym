package destinationtriggers_activity

import (
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func mysqlFound(name string, order int64, triggerSchema *string) *sqlmanager_shared.TableTrigger {
	return &sqlmanager_shared.TableTrigger{
		Schema: "web", Table: "COMMANDE", TriggerName: name, TriggerSchema: triggerSchema,
		Mysql: &sqlmanager_shared.MysqlTrigger{
			Timing: "AFTER", Event: "INSERT", Orientation: "ROW", Statement: "INSERT INTO h VALUES (NEW.id)",
			ActionOrder: order, Definer: "app@%", SqlMode: "NO_ENGINE_SUBSTITUTION", CollationConnection: "utf8mb4_bin",
		},
	}
}

// A MySQL trigger comes back as it was created: its definer, its sql_mode and collation,
// on a session whose own settings are given back afterwards.
func TestToTriggers_MysqlRestoresTheCreation(t *testing.T) {
	triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, []*sqlmanager_shared.TableTrigger{mysqlFound("trg-a`b", 1, nil)})
	require.NoError(t, err)
	require.Len(t, triggers, 1)
	trigger := triggers[0]
	require.Equal(t, "DROP TRIGGER IF EXISTS `web`.`trg-a``b`", trigger.Suspend)
	require.Equal(t, []string{
		"SET @husonym_sql_mode = @@SESSION.sql_mode, @husonym_collation = @@SESSION.collation_connection",
		"SET SESSION sql_mode = 'NO_ENGINE_SUBSTITUTION', collation_connection = 'utf8mb4_bin'",
		"CREATE DEFINER = `app`@`%` TRIGGER IF NOT EXISTS `web`.`trg-a``b` AFTER INSERT ON `web`.`COMMANDE` " +
			"FOR EACH ROW INSERT INTO h VALUES (NEW.id)",
	}, trigger.Restore)
	require.Equal(t, "SET SESSION sql_mode = @husonym_sql_mode, collation_connection = @husonym_collation", trigger.Reset)
}

// Triggers of the same event come back in the order they fire in: the one created last
// fires last.
func TestToTriggers_MysqlKeepsTheFiringOrder(t *testing.T) {
	triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, []*sqlmanager_shared.TableTrigger{
		mysqlFound("second", 2, nil), mysqlFound("premier", 1, nil),
	})
	require.NoError(t, err)
	require.Equal(t, "premier", triggers[0].Name)
	require.Equal(t, "second", triggers[1].Name)
}

// A trigger is dropped from the schema holding it, which is not always the one of the
// table it fires on.
func TestToTriggers_TriggerSchema(t *testing.T) {
	own := "autre"
	triggers, err := toTriggers(sqlmanager_shared.MysqlDriver, []*sqlmanager_shared.TableTrigger{mysqlFound("t", 1, &own)})
	require.NoError(t, err)
	require.Equal(t, "DROP TRIGGER IF EXISTS `autre`.`t`", triggers[0].Suspend)

	empty := ""
	for _, schema := range []*string{nil, &empty} {
		triggers, err = toTriggers(sqlmanager_shared.MysqlDriver, []*sqlmanager_shared.TableTrigger{mysqlFound("t", 1, schema)})
		require.NoError(t, err)
		require.Equal(t, "web", triggers[0].Schema, "the table schema stands in for a missing trigger schema")
	}
}

func TestMysqlAccount(t *testing.T) {
	account, err := mysqlAccount("user@with@host")
	require.NoError(t, err)
	require.Equal(t, "`user@with`@`host`", account)
	_, err = mysqlAccount("nobody")
	require.Error(t, err)
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
	triggers, err := toTriggers(sqlmanager_shared.PostgresDriver, found)
	require.NoError(t, err)
	restore := map[string][]string{}
	for _, trigger := range triggers {
		require.Equal(t, `ALTER TABLE "web"."COMMANDE" DISABLE TRIGGER "`+trigger.Name+`"`, trigger.Suspend)
		require.Empty(t, trigger.Reset)
		restore[trigger.Name] = trigger.Restore
	}
	require.Equal(t, map[string][]string{
		"origine":  {`ALTER TABLE "web"."COMMANDE" ENABLE TRIGGER "origine"`},
		"replica":  {`ALTER TABLE "web"."COMMANDE" ENABLE REPLICA TRIGGER "replica"`},
		"toujours": {`ALTER TABLE "web"."COMMANDE" ENABLE ALWAYS TRIGGER "toujours"`},
	}, restore)
}

func TestToTriggers_SqlServerLeftAlone(t *testing.T) {
	triggers, err := toTriggers(sqlmanager_shared.MssqlDriver, []*sqlmanager_shared.TableTrigger{mysqlFound("t", 1, nil)})
	require.NoError(t, err)
	require.Empty(t, triggers)
}

// What an earlier attempt or run recorded wins over what is read now: read after the
// trigger was taken out of the way, it is gone (MySQL) or disabled (PostgreSQL).
func TestMerge(t *testing.T) {
	earlier := &Trigger{Schema: "web", Table: "COMMANDE", Name: "t", Restore: []string{"earlier"}}
	recorded := []*suspended{{ConnectionID: "c1", Triggers: []*Trigger{earlier}}}

	now := &Trigger{Schema: "web", Table: "COMMANDE", Name: "t", Restore: []string{"now"}}
	other := &Trigger{Schema: "web", Table: "LIGNE", Name: "t", Restore: []string{"other"}}
	recorded = merge(recorded, "c1", []*Trigger{now, other})
	require.Len(t, recorded, 1)
	require.Equal(t, []*Trigger{earlier, other}, recorded[0].Triggers)

	recorded = merge(recorded, "c2", nil)
	require.Len(t, recorded, 1, "a destination without triggers records nothing")
	recorded = merge(recorded, "c2", []*Trigger{now})
	require.Len(t, recorded, 2)
}

func TestQuoteMysql(t *testing.T) {
	require.Equal(t, "`tri``cky`", quoteMysql("tri`cky"))
}
