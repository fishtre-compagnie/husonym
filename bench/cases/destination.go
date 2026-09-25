package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// destinationCases hold what a real destination has beyond empty tables.
func destinationCases() []*Case {
	return []*Case{
		destinationTriggerWritesSyncedTable(),
		destinationReplicaTriggerWritesSyncedTable(),
		destinationTriggerRestoredAfterFailedRun(),
		destinationTriggerRestoredByNextRun(),
		destinationTriggerSpecialName(),
		destinationTriggersMysqlCreation(),
		destinationDiffers("destination-column-order",
			"Destination dont les colonnes sont dans un autre ordre, avec une colonne nullable en plus",
			map[schema.Dialect][]string{
				schema.MySQL: {"ALTER TABLE {db}.`ARTICLE` MODIFY `libelle` VARCHAR(40) NOT NULL FIRST, " +
					"ADD `ajoutee` INT NULL AFTER `libelle`"},
				// PostgreSQL cannot move a column: the table is created again in the other order.
				schema.Postgres: {
					"DROP TABLE {db}.{q:ARTICLE}",
					"CREATE TABLE {db}.{q:ARTICLE} ({q:libelle} varchar(40) NOT NULL, {q:ajoutee} integer, " +
						"{q:id} bigint NOT NULL PRIMARY KEY, {q:code} varchar(10) NOT NULL)",
					"CREATE UNIQUE INDEX {q:ARTICLE_uq_article_code} ON {db}.{q:ARTICLE} ({q:code})",
				},
			}, nil),
		destinationDiffers("destination-extra-not-null-column",
			"Destination avec une colonne NOT NULL sans défaut en plus : échec explicite",
			map[schema.Dialect][]string{
				schema.MySQL:    {"ALTER TABLE {db}.{q:ARTICLE} ADD {q:obligatoire} INT NOT NULL"},
				schema.Postgres: {"ALTER TABLE {db}.{q:ARTICLE} ADD {q:obligatoire} integer NOT NULL"},
			},
			map[schema.Dialect]string{
				schema.MySQL:    "doesn't have a default value",
				schema.Postgres: `null value in column "obligatoire"`,
			}),
		onConflictUpdateOnUniqueKey(),
	}
}

// destinationTriggerWritesSyncedTable: the destination keeps the application trigger
// that logs every new order into COMMANDE_HISTO, a table the job syncs too. Writing the
// orders fires it: the history gets rows of its own, which collide with the ones copied
// from the source or pile up next to them.
func destinationTriggerWritesSyncedTable() *Case {
	c := triggerWritesSyncedTable("destination-trigger-writes-synced-table",
		"Trigger en destination qui écrit dans une table elle aussi synchronisée",
		historyTrigger(historyTriggerName))
	c.ExpectFindings = []ExpectedFinding{
		expectFinding(mgmtv1alpha1.PreflightFinding_KIND_DESTINATION_TRIGGERS, findingInformation, commandeTable),
	}
	return c
}

// destinationReplicaTriggerWritesSyncedTable: the same trigger, set ENABLE REPLICA. It fires
// only in a session whose replication role is replica — the very role Athanor writes a
// page in to suspend foreign keys, and one Benthos never takes. Left to the role, it would
// fire for one engine and not for the other.
func destinationReplicaTriggerWritesSyncedTable() *Case {
	c := triggerWritesSyncedTable("destination-replica-trigger-writes-synced-table",
		"Trigger ENABLE REPLICA en destination : il ne se déclenche qu'en rôle replica",
		map[schema.Dialect][]string{schema.Postgres: postgresHistoryTrigger(historyTriggerName, "REPLICA")})
	c.Dialects = postgresOnly
	return c
}

// destinationTriggerRestoredAfterFailedRun: the run takes the history trigger out of its
// way, then fails on a destination column the source does not fill. The trigger is
// application logic the destination keeps running after the copy: a failed run must put it
// back as surely as a successful one.
func destinationTriggerRestoredAfterFailedRun() *Case {
	c := triggerWritesSyncedTable("destination-trigger-restored-after-failed-run",
		"Run en échec après la suspension d'un trigger de destination : le trigger doit être rétabli",
		historyTrigger(historyTriggerName))
	c.DestinationSetup = []string{"ALTER TABLE {db}.{q:COMMANDE_HISTO} ADD {q:obligatoire} INT NOT NULL"}
	c.ExpectRunError = map[schema.Dialect]string{
		schema.MySQL:    "doesn't have a default value",
		schema.Postgres: `null value in column "obligatoire"`,
	}
	return c
}

// destinationTriggerRestoredByNextRun: a first run is terminated once it has taken the
// history trigger out of its way. Nothing of that run puts it back; the next run of the job
// must, whether the trigger is gone (MySQL) or disabled (PostgreSQL) when it starts.
//
// The orders span many pages, so that the first run is caught writing them. The next run
// empties the destination first: it must not trip over the rows the first one wrote.
func destinationTriggerRestoredByNextRun() *Case {
	c := triggerWritesSyncedTable("destination-trigger-restored-by-next-run",
		"Run arrêté de force triggers retirés : le run suivant du job les rétablit",
		historyTrigger(historyTriggerName))
	c.InterruptedRunFirst = true
	c.Job.TruncateBeforeInsert = true
	c.Seed = func(p Params, emit Emitter) {
		for i := int64(1); i <= int64(30*p.PageLimit); i++ {
			emit.Row(commandeTable, []any{i}, Kept())
		}
		for i := int64(1); i <= 10; i++ {
			emit.Row("COMMANDE_HISTO", []any{100 + i, i, "source"}, Kept())
		}
	}
	return c
}

// destinationTriggerSpecialName: a trigger whose name needs quoting — a dash, a dot, the
// quote characters of both databases. Taking it out of the way and putting it back must
// both name it right: a trigger dropped under its name and created again under a broken
// statement is lost.
func destinationTriggerSpecialName() *Case {
	return triggerWritesSyncedTable("destination-trigger-special-name",
		"Trigger de destination au nom à guillemets (tiret, point, guillemets) : suspendu et rétabli à l'identique",
		historyTrigger("trg-histo.`v2` \"x\""))
}

// destinationTriggersMysqlCreation: MySQL runs a trigger with the rights of its definer,
// under the sql_mode and the collation it was created with, and in its place among the
// triggers of the same event. Dropped and created again by another account, under another
// session, in another order, it would be another trigger: the history rows it writes would
// come in another order, with other rights and other checks.
func destinationTriggersMysqlCreation() *Case {
	insertAction := func(action string) string {
		return "INSERT INTO {db}.`COMMANDE_HISTO` (`commande_id`, `action`) VALUES (NEW.`id`, '" + action + "')"
	}
	c := triggerWritesSyncedTable("destination-triggers-mysql-creation",
		"Triggers MySQL : definer, sql_mode, collation et ordre d'exécution rétablis à l'identique",
		map[schema.Dialect][]string{schema.MySQL: {
			"CREATE USER IF NOT EXISTS 'bench_definer'@'%'",
			"GRANT INSERT ON {db}.* TO 'bench_definer'@'%'",
			"SET SESSION sql_mode = 'NO_ENGINE_SUBSTITUTION'",
			"SET SESSION collation_connection = 'utf8mb4_bin'",
			"CREATE DEFINER = 'bench_definer'@'%' TRIGGER {db}.`trg_premier` AFTER INSERT ON {db}.`COMMANDE` " +
				"FOR EACH ROW " + insertAction("premier"),
			"CREATE DEFINER = 'bench_definer'@'%' TRIGGER {db}.`trg_avant` AFTER INSERT ON {db}.`COMMANDE` " +
				"FOR EACH ROW PRECEDES `trg_premier` BEGIN IF NEW.`id` > 0 THEN " + insertAction("avant") + "; END IF; END",
		}})
	c.Dialects = mysqlOnly
	return c
}

// historyTriggerName is the name the history trigger usually has.
const historyTriggerName = "trg_commande_histo"

// historyTrigger logs every new order into COMMANDE_HISTO, on each database.
func historyTrigger(name string) map[schema.Dialect][]string {
	return map[schema.Dialect][]string{
		schema.MySQL: {
			"CREATE TRIGGER {db}.{q:" + name + "} AFTER INSERT ON {db}.{q:COMMANDE} FOR EACH ROW " +
				"INSERT INTO {db}.{q:COMMANDE_HISTO} ({q:commande_id}, {q:action}) VALUES (NEW.{q:id}, 'trigger')",
		},
		schema.Postgres: postgresHistoryTrigger(name, ""),
	}
}

// postgresHistoryTrigger logs every new order into COMMANDE_HISTO, as the MySQL trigger does:
// PostgreSQL runs a function where MySQL runs an inline body. enable is the state it is set
// to afterwards, "" leaving it enabled the usual way.
func postgresHistoryTrigger(name, enable string) []string {
	stmts := []string{
		"CREATE FUNCTION {db}.{q:trg_commande_histo}() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " +
			"INSERT INTO {db}.{q:COMMANDE_HISTO} ({q:commande_id}, {q:action}) VALUES (NEW.{q:id}, 'trigger'); " +
			"RETURN NEW; END $$",
		"CREATE TRIGGER {q:" + name + "} AFTER INSERT ON {db}.{q:COMMANDE} FOR EACH ROW " +
			"EXECUTE FUNCTION {db}.{q:trg_commande_histo}()",
	}
	if enable != "" {
		stmts = append(stmts, "ALTER TABLE {db}.{q:COMMANDE} ENABLE "+enable+" TRIGGER {q:"+name+"}")
	}
	return stmts
}

func triggerWritesSyncedTable(id, title string, setup map[schema.Dialect][]string) *Case {
	return &Case{
		ID:                  id,
		Priority:            P1,
		Title:               title,
		DestinationSetupFor: setup,
		Tables: []*schema.Table{
			{
				Name:       commandeTable,
				Columns:    []schema.Column{{Name: idColumn, Type: schema.Int64()}},
				PrimaryKey: []string{idColumn},
			},
			{
				Name: "COMMANDE_HISTO",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64(), AutoIncrement: true},
					{Name: "commande_id", Type: schema.Int64()},
					{Name: "action", Type: schema.Varchar(20)},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_histo_commande", "commande_id", commandeTable)},
			},
		},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i}, Kept())
				emit.Row("COMMANDE_HISTO", []any{100 + i, i, "source"}, Kept())
			}
		},
	}
}

func articleTable() *schema.Table {
	return &schema.Table{
		Name: "ARTICLE",
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "code", Type: schema.Varchar(10)},
			{Name: "libelle", Type: schema.Varchar(40)},
		},
		PrimaryKey: []string{idColumn},
		Indexes:    []schema.Index{{Name: "uq_article_code", Columns: []string{"code"}, Unique: true}},
	}
}

func seedArticles(emit Emitter) {
	for i := int64(1); i <= 20; i++ {
		emit.Row("ARTICLE", []any{i, fmt.Sprintf("C%04d", i), fmt.Sprintf("article %d", i)}, Kept())
	}
}

// destinationDiffers: the destination table is not the copy of the source one. runError
// is nil when the difference must not matter.
func destinationDiffers(id, title string, setup map[schema.Dialect][]string, runError map[schema.Dialect]string) *Case {
	return &Case{
		ID:                  id,
		Priority:            P2,
		Title:               title,
		Tables:              []*schema.Table{articleTable()},
		DestinationSetupFor: setup,
		ExpectRunError:      runError,
		Seed:                func(p Params, emit Emitter) { seedArticles(emit) },
	}
}

// onConflictUpdateOnUniqueKey: the destination already holds articles, some under the
// same primary key with an old label, one under another primary key but the same unique
// code. After an upsert the destination must hold exactly the source rows.
//
// On MySQL the upsert fires on any unique key, and settles both conflicts. PostgreSQL's
// ON CONFLICT DO UPDATE names one target and settles only that one: the conflict on the
// code has no answer a copy could give without guessing which row to keep. The run must
// then fail on it, in the database's own words, rather than drop or merge a row silently.
func onConflictUpdateOnUniqueKey() *Case {
	return &Case{
		ID:       "on-conflict-update-unique-key",
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:  "onConflict update : conflit sur la clé primaire et sur une clé unique autre que la clé primaire",
		Tables: []*schema.Table{articleTable()},
		DestinationSetup: []string{
			"INSERT INTO {db}.{q:ARTICLE} ({q:id}, {q:code}, {q:libelle}) VALUES " +
				"(1, 'C0001', 'ancien libellé'), (2, 'C0002', 'ancien libellé'), (900, 'C0003', 'même code, autre id')",
		},
		Job: Job{OnConflictUpdate: true},
		ExpectRunError: map[schema.Dialect]string{
			schema.Postgres: "duplicate key value violates unique constraint",
		},
		Seed: func(p Params, emit Emitter) { seedArticles(emit) },
	}
}
