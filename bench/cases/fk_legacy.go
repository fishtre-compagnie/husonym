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
		fkSourceOrphansDestinationKept(),
		fkSelfReferenceNotNull(),
		fkSentinelZeroVirtual(),
		fkParentOutsideJob(),
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
// cannot be written, a nullable one is cleared. The run empties the destination first, so
// it may repair what it finds there.
func fkSourceOrphans() *Case {
	c := sourceOrphansCase("fk-source-orphans",
		"Orphelins déjà présents dans la source, sur FK obligatoire et sur FK nullable")
	c.Job.TruncateBeforeInsert = true
	return c
}

// fkSourceOrphansDestinationKept: same orphans, but the run does not empty the
// destination. It cannot tell its rows from the ones already there, so it must not repair
// anything: the only correct outcome is a failed run listing the orphans.
func fkSourceOrphansDestinationKept() *Case {
	c := sourceOrphansCase("fk-source-orphans-destination-kept",
		"Orphelins de la source et destination non vidée : échec listant les orphelins, aucune réparation")
	c.ExpectRunError = "referential integrity check failed"
	return c
}

func sourceOrphansCase(id, title string) *Case {
	return &Case{
		ID:       id,
		Priority: P1,
		Title:    title,
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

// fkSentinelZeroVirtual: parent_id NOT NULL DEFAULT 0, where 0 means "no parent", and a
// relation the database never declared. The user describes it as a virtual foreign key
// to get the subset right. Rows holding the sentinel are legitimate business rows: they
// must be kept as they are, neither dropped for lack of a parent 0 nor counted as orphans.
func fkSentinelZeroVirtual() *Case {
	return &Case{
		ID:       "fk-sentinel-zero-virtual",
		Priority: P1,
		Title:    "Sentinelle parent_id = 0 sur FK virtuelle NOT NULL : lignes métier à garder telles quelles",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_commande_station", stationIDColumn, "STATION")},
			},
			{
				Name: "AVOIR",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "commande_id", Type: schema.Int64(), Default: "0"},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_avoir_station", stationIDColumn, "STATION"),
					{
						Name: "vfk_avoir_commande", Columns: []string{"commande_id"},
						RefTable: commandeTable, RefColumns: []string{idColumn}, Virtual: true, Sentinel: "0",
					},
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, stationKept}, Kept())
				emit.Row(commandeTable, []any{1000 + i, stationDropped}, Dropped())
				emit.Row("AVOIR", []any{i, stationKept, i}, Kept())
				emit.Row("AVOIR", []any{100 + i, stationKept, int64(0)}, Kept())
				emit.Row("AVOIR", []any{1000 + i, stationDropped, int64(0)}, Dropped())
			}
		},
	}
}

// fkParentOutsideJob: PAYS is a reference table the user leaves out of the job, already
// filled at the destination by other means. The mandatory foreign key to it must not
// keep its children from being written.
func fkParentOutsideJob() *Case {
	return &Case{
		ID:       "fk-parent-outside-job",
		Priority: P1,
		Title:    "FK obligatoire vers une table absente du job, déjà remplie en destination",
		Tables: []*schema.Table{
			{
				Name:       "PAYS",
				Columns:    []schema.Column{{Name: idColumn, Type: schema.Int64()}, {Name: "nom", Type: schema.Varchar(40)}},
				PrimaryKey: []string{idColumn},
			},
			{
				Name: clientTableName,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "pays_id", Type: schema.Int64()},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_client_pays", "pays_id", "PAYS")},
			},
		},
		Job:              Job{ExcludedTables: []string{"PAYS"}},
		DestinationSetup: []string{"INSERT INTO {db}.`PAYS` (`id`, `nom`) VALUES (1, 'France'), (2, 'Belgique')"},
		Seed: func(p Params, emit Emitter) {
			emit.Row("PAYS", []any{int64(1), "France"}, Kept())
			emit.Row("PAYS", []any{int64(2), "Belgique"}, Kept())
			for i := int64(1); i <= 20; i++ {
				emit.Row(clientTableName, []any{i, i%2 + 1}, Kept())
			}
		},
	}
}
