package cases

import (
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// legacyForeignKeyCases hold what old databases accumulate: relations never declared,
// rows written with checks off, hierarchies closed on themselves.
func legacyForeignKeyCases() []*Case {
	return []*Case{
		fkVirtualOnlyPath(),
		fkSourceOrphans(),
		fkSelfReferenceNotNull(),
	}
}

// fkVirtualOnlyPath: the only link between NOTE and the subset root is a virtual foreign
// key. Without it the whole table would be copied.
func fkVirtualOnlyPath() *Case {
	return &Case{
		ID:       "fk-virtual-only-path",
		Priority: P1,
		Title:    "Table rattachée au subset uniquement par une FK virtuelle",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "NOTE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "station_ref", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{{
					Name: "vfk_note_station", Columns: []string{"station_ref"},
					RefTable: "STATION", RefColumns: []string{idColumn}, Virtual: true,
				}},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row("NOTE", []any{i, stationKept}, Kept())
				emit.Row("NOTE", []any{1000 + i, stationDropped}, Dropped())
			}
		},
	}
}

// fkSourceOrphans: the source itself holds orphans, written one day with foreign key
// checks off. No subset: the whole tables are copied. A mandatory reference to nothing
// cannot be written, a nullable one is cleared.
func fkSourceOrphans() *Case {
	return &Case{
		ID:       "fk-source-orphans",
		Priority: P1,
		Title:    "Orphelins déjà présents dans la source, sur FK obligatoire et sur FK nullable",
		Tables: []*schema.Table{
			{
				Name:       commandeTable,
				Columns:    []schema.Column{{Name: idColumn, Type: schema.Int64()}},
				PrimaryKey: []string{idColumn},
			},
			{
				Name: "LIGNE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "commande_id", Type: schema.Int64()},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_ligne_commande", "commande_id", commandeTable)},
			},
			{
				Name: "REMARQUE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "commande_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_remarque_commande", "commande_id", commandeTable)},
			},
		},
		Job: Job{SkipForeignKeyViolations: true},
		Seed: func(p Params, emit Emitter) {
			const missing = int64(9000)
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i}, Kept())
				emit.Row("LIGNE", []any{i, i}, Kept())
				emit.Row("LIGNE", []any{100 + i, missing + i}, Dropped())
				emit.Row("REMARQUE", []any{i, i}, Kept())
				emit.Row("REMARQUE", []any{100 + i, missing + i}, Kept("commande_id"))
			}
		},
	}
}

// fkSelfReferenceNotNull: a hierarchy whose root references itself, the usual way to
// keep parent_id NOT NULL. The table depends on itself without any nullable column to
// defer, which the run planner reads as an unsolvable cycle.
func fkSelfReferenceNotNull() *Case {
	return &Case{
		ID:       "fk-self-reference-not-null",
		Priority: P2,
		Title:    "FK auto-référencée NOT NULL : la racine se référence elle-même",
		Tables: []*schema.Table{{
			Name: "CATEGORIE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "parent_id", Type: schema.Int64()},
			},
			PrimaryKey:  []string{idColumn},
			ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_categorie_parent", "parent_id", "CATEGORIE")},
		}},
		Seed: func(p Params, emit Emitter) {
			emit.Row("CATEGORIE", []any{int64(1), int64(1)}, Kept())
			for i := int64(2); i <= 30; i++ {
				// Parents écrits tantôt avant, tantôt après leurs enfants.
				parent := i / 2
				if i%5 == 0 && i < 30 {
					parent = i + 1
				}
				emit.Row("CATEGORIE", []any{i, parent}, Kept())
			}
		},
	}
}
