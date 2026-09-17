package cases

import (
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// destinationCases hold what a real destination has beyond empty tables.
func destinationCases() []*Case {
	return []*Case{destinationTriggerWritesSyncedTable()}
}

// destinationTriggerWritesSyncedTable: the destination keeps the application trigger
// that logs every new order into COMMANDE_HISTO, a table the job syncs too. Writing the
// orders fires it: the history gets rows of its own, which collide with the ones copied
// from the source or pile up next to them.
func destinationTriggerWritesSyncedTable() *Case {
	return &Case{
		ID:       "destination-trigger-writes-synced-table",
		Priority: P1,
		Title:    "Trigger en destination qui écrit dans une table elle aussi synchronisée",
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
		DestinationSetup: []string{
			"CREATE TRIGGER {db}.`trg_commande_histo` AFTER INSERT ON {db}.`COMMANDE` FOR EACH ROW " +
				"INSERT INTO {db}.`COMMANDE_HISTO` (`commande_id`, `action`) VALUES (NEW.`id`, 'trigger')",
		},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i}, Kept())
				emit.Row("COMMANDE_HISTO", []any{100 + i, i, "source"}, Kept())
			}
		},
	}
}
