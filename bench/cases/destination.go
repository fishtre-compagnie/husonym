package cases

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// destinationCases hold what a real destination has beyond empty tables.
func destinationCases() []*Case {
	return []*Case{
		destinationTriggerWritesSyncedTable(),
		destinationReplicaTriggerWritesSyncedTable(),
		// Both tell a MySQL message apart, and reorder columns, which PostgreSQL cannot.
		destinationDiffers("destination-column-order",
			"Destination dont les colonnes sont dans un autre ordre, avec une colonne nullable en plus",
			"", "ALTER TABLE {db}.`ARTICLE` MODIFY `libelle` VARCHAR(40) NOT NULL FIRST, ADD `ajoutee` INT NULL AFTER `libelle`"),
		destinationDiffers("destination-extra-not-null-column",
			"Destination avec une colonne NOT NULL sans défaut en plus : échec explicite",
			"doesn't have a default value", "ALTER TABLE {db}.`ARTICLE` ADD `obligatoire` INT NOT NULL"),
		onConflictUpdateOnUniqueKey(),
	}
}

// destinationTriggerWritesSyncedTable: the destination keeps the application trigger
// that logs every new order into COMMANDE_HISTO, a table the job syncs too. Writing the
// orders fires it: the history gets rows of its own, which collide with the ones copied
// from the source or pile up next to them.
func destinationTriggerWritesSyncedTable() *Case {
	return triggerWritesSyncedTable("destination-trigger-writes-synced-table",
		"Trigger en destination qui écrit dans une table elle aussi synchronisée",
		map[schema.Dialect][]string{
			schema.MySQL: {
				"CREATE TRIGGER {db}.`trg_commande_histo` AFTER INSERT ON {db}.`COMMANDE` FOR EACH ROW " +
					"INSERT INTO {db}.`COMMANDE_HISTO` (`commande_id`, `action`) VALUES (NEW.`id`, 'trigger')",
			},
			schema.Postgres: postgresHistoryTrigger(""),
		})
}

// destinationReplicaTriggerWritesSyncedTable: the same trigger, set ENABLE REPLICA. It fires
// only in a session whose replication role is replica — the very role Athanor writes a
// page in to suspend foreign keys, and one Benthos never takes. Left to the role, it would
// fire for one engine and not for the other.
func destinationReplicaTriggerWritesSyncedTable() *Case {
	c := triggerWritesSyncedTable("destination-replica-trigger-writes-synced-table",
		"Trigger ENABLE REPLICA en destination : il ne se déclenche qu'en rôle replica",
		map[schema.Dialect][]string{schema.Postgres: postgresHistoryTrigger("REPLICA")})
	c.Dialects = postgresOnly
	return c
}

// postgresHistoryTrigger logs every new order into COMMANDE_HISTO, as the MySQL trigger does:
// PostgreSQL runs a function where MySQL runs an inline body. enable is the state it is set
// to afterwards, "" leaving it enabled the usual way.
func postgresHistoryTrigger(enable string) []string {
	stmts := []string{
		"CREATE FUNCTION {db}.{q:trg_commande_histo}() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN " +
			"INSERT INTO {db}.{q:COMMANDE_HISTO} ({q:commande_id}, {q:action}) VALUES (NEW.{q:id}, 'trigger'); " +
			"RETURN NEW; END $$",
		"CREATE TRIGGER {q:trg_commande_histo} AFTER INSERT ON {db}.{q:COMMANDE} FOR EACH ROW " +
			"EXECUTE FUNCTION {db}.{q:trg_commande_histo}()",
	}
	if enable != "" {
		stmts = append(stmts, "ALTER TABLE {db}.{q:COMMANDE} ENABLE "+enable+" TRIGGER {q:trg_commande_histo}")
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
// is empty when the difference must not matter.
func destinationDiffers(id, title, runError, alter string) *Case {
	return &Case{
		ID:               id,
		Priority:         P2,
		Title:            title,
		Tables:           []*schema.Table{articleTable()},
		Dialects:         mysqlOnly,
		DestinationSetup: []string{alter},
		ExpectRunError:   map[schema.Dialect]string{schema.MySQL: runError},
		Seed:             func(p Params, emit Emitter) { seedArticles(emit) },
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
