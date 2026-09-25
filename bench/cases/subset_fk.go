package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// subsetForeignKeyCases exercise the subset propagated along foreign keys. Station 1 is
// the subset; station 2 stands for everything that must stay out of the destination.
func subsetForeignKeyCases() []*Case {
	return []*Case{
		fkSelfReferenceNullable(),
		fkNullableOnSubsetPath(),
		fkDiamond(),
		fkCycleTwoTables(),
		fkSeveralToSameParent(),
		fkMandatoryParentOutOfSubsetWithoutSkip(),
		fkCompositePartiallyNull(),
		fkCompositeColumnOrder(),
		fkVirtual(),
		fkParentKeyCollation(),
	}
}

const (
	idColumn        = "id"
	commandeTable   = "COMMANDE"
	clientTableName = "CLIENT"
	stationIDColumn = "station_id"
	referenceColumn = "reference"

	stationKept    = int64(1)
	stationDropped = int64(2)
)

func stationTable() *schema.Table {
	return &schema.Table{
		Name: "STATION",
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "nom", Type: schema.Varchar(40)},
		},
		PrimaryKey: []string{idColumn},
	}
}

// foreignKeyToID declares a single-column foreign key to the id of another table.
func foreignKeyToID(name, column, refTable string) schema.ForeignKey {
	return schema.ForeignKey{Name: name, Columns: []string{column}, RefTable: refTable, RefColumns: []string{idColumn}}
}

func seedStations(emit Emitter) {
	emit.Row("STATION", []any{stationKept, "station du subset"}, Kept())
	emit.Row("STATION", []any{stationDropped, "station hors subset"}, Dropped())
}

func subsetOnStationJob() Job {
	return Job{
		Where:                    map[string]string{"STATION": fmt.Sprintf("id = %d", stationKept)},
		SubsetByForeignKeys:      true,
		SkipForeignKeyViolations: true,
	}
}

// fkSelfReferenceNullable reproduces the defect of the first real run
// (COMMANDE_MONTEUR.ID_INTERVENTION_GROUPEE): a nullable self-reference is never on a
// subset path, so kept rows can reference rows left out. They must be kept, with the
// reference set to NULL.
func fkSelfReferenceNullable() *Case {
	return &Case{
		ID:       "fk-self-reference-nullable",
		Priority: P1,
		Title:    "FK auto-référencée nullable vers des lignes hors subset",
		ExpectFindings: []ExpectedFinding{
			expectFinding(mgmtv1alpha1.PreflightFinding_KIND_REFERENCE_CLEARED_BY_SUBSET, findingInformation, commandeTable),
		},
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "groupe_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_commande_station", stationIDColumn, "STATION"),
					foreignKeyToID("fk_commande_groupe", "groupe_id", commandeTable),
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			// 1000+ : station hors subset. 1+ : station du subset.
			for i := int64(1); i <= 20; i++ {
				emit.Row(commandeTable, []any{1000 + i, stationDropped, nil}, Dropped())
			}
			for i := int64(1); i <= 60; i++ {
				switch {
				case i%3 == 0: // groupée avec une commande hors subset
					emit.Row(commandeTable, []any{i, stationKept, 1000 + i%20 + 1}, Kept("groupe_id"))
				case i%3 == 1 && i > 1: // groupée avec une commande du subset, écrite avant ou après elle
					emit.Row(commandeTable, []any{i, stationKept, 61 - i}, Kept())
				default:
					emit.Row(commandeTable, []any{i, stationKept, nil}, Kept())
				}
			}
		},
	}
}

// fkNullableOnSubsetPath reproduces ID_COMMANDE_ALLOPNEUS: the only path from COMMANDE
// to the subset root goes through a nullable foreign key, joined with an INNER JOIN.
// Rows holding NULL reference nothing out of the subset and must be kept; rows
// referencing a parent out of the subset are what the subset removes.
func fkNullableOnSubsetPath() *Case {
	return &Case{
		ID:       "fk-nullable-on-subset-path",
		Priority: P1,
		Title:    "FK nullable sur le chemin du subset : l'INNER JOIN exclut les lignes à NULL",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "COMMANDE_FOURNISSEUR",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_cf_station", stationIDColumn, "STATION"),
				},
			},
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "commande_fournisseur_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_commande_cf", "commande_fournisseur_id", "COMMANDE_FOURNISSEUR"),
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row("COMMANDE_FOURNISSEUR", []any{i, stationKept}, Kept())
				emit.Row("COMMANDE_FOURNISSEUR", []any{1000 + i, stationDropped}, Dropped())
			}
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, i}, Kept())
				emit.Row(commandeTable, []any{100 + i, nil}, Kept())
				emit.Row(commandeTable, []any{1000 + i, 1000 + i}, Dropped())
			}
		},
	}
}

// fkDiamond: FACTURE reaches STATION by two paths, directly and through COMMANDE, and
// the subset joins only the shortest one. An invoice of the kept station can reference
// an order of the other station: its mandatory parent is missing, so it must be dropped.
func fkDiamond() *Case {
	return &Case{
		ID:       "fk-diamond",
		Priority: P1,
		Title:    "Diamant : FK obligatoire hors du chemin du subset vers une ligne hors subset",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_commande_station", stationIDColumn, "STATION"),
				},
			},
			{
				Name: "FACTURE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "commande_id", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_facture_station", stationIDColumn, "STATION"),
					foreignKeyToID("fk_facture_commande", "commande_id", commandeTable),
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, stationKept}, Kept())
				emit.Row(commandeTable, []any{1000 + i, stationDropped}, Dropped())
			}
			for i := int64(1); i <= 10; i++ {
				emit.Row("FACTURE", []any{i, stationKept, i}, Kept())
				// Facturée par la station du subset pour une commande de l'autre station.
				emit.Row("FACTURE", []any{100 + i, stationKept, 1000 + i}, Dropped())
				emit.Row("FACTURE", []any{1000 + i, stationDropped, 1000 + i}, Dropped())
			}
		},
	}
}

// fkCycleTwoTables: EQUIPE and EMPLOYE reference each other through nullable foreign
// keys. EMPLOYE follows EQUIPE into the subset; a team of the subset can be led by an
// employee of a team left out, and must then lose its leader, not its row.
func fkCycleTwoTables() *Case {
	return &Case{
		ID:       "fk-cycle-two-tables",
		Priority: P1,
		Title:    "Cycle entre deux tables par FK nullables, vers des lignes hors subset",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "EQUIPE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "chef_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_equipe_station", stationIDColumn, "STATION"),
					foreignKeyToID("fk_equipe_chef", "chef_id", "EMPLOYE"),
				},
			},
			{
				Name: "EMPLOYE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "equipe_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_employe_equipe", "equipe_id", "EQUIPE")},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				// Équipe i du subset, équipe 1000+i hors subset, un employé chacune.
				emit.Row("EMPLOYE", []any{i, i}, Kept())
				emit.Row("EMPLOYE", []any{1000 + i, 1000 + i}, Dropped())
				emit.Row("EQUIPE", []any{1000 + i, stationDropped, 1000 + i}, Dropped())
				if i%2 == 0 {
					emit.Row("EQUIPE", []any{i, stationKept, i}, Kept())
				} else { // dirigée par un employé d'une équipe hors subset
					emit.Row("EQUIPE", []any{i, stationKept, 1000 + i}, Kept("chef_id"))
				}
			}
		},
	}
}

// fkSeveralToSameParent: three foreign keys reference STATION and the subset joins only
// the first one (station_arrivee_id, by name). The mandatory one left aside must drop
// the row when its station is out; the nullable one must only lose its value.
func fkSeveralToSameParent() *Case {
	return &Case{
		ID:       "fk-several-to-same-parent",
		Priority: P1,
		Title:    "Plusieurs FK vers le même parent : seule la première sert au subset",
		// station_retour_id only: the subset joins on station_arrivee_id, and
		// station_depart_id is mandatory.
		ExpectFindings: []ExpectedFinding{
			expectFinding(mgmtv1alpha1.PreflightFinding_KIND_REFERENCE_CLEARED_BY_SUBSET, findingInformation, "TRANSFERT"),
		},
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "TRANSFERT",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "station_arrivee_id", Type: schema.Int64()},
					{Name: "station_depart_id", Type: schema.Int64()},
					{Name: "station_retour_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_transfert_arrivee", "station_arrivee_id", "STATION"),
					foreignKeyToID("fk_transfert_depart", "station_depart_id", "STATION"),
					foreignKeyToID("fk_transfert_retour", "station_retour_id", "STATION"),
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row("TRANSFERT", []any{i, stationKept, stationKept, stationKept}, Kept())
				emit.Row("TRANSFERT", []any{100 + i, stationKept, stationKept, nil}, Kept())
				emit.Row("TRANSFERT", []any{200 + i, stationKept, stationKept, stationDropped}, Kept("station_retour_id"))
				emit.Row("TRANSFERT", []any{300 + i, stationKept, stationDropped, nil}, Dropped())
				emit.Row("TRANSFERT", []any{400 + i, stationDropped, stationKept, nil}, Dropped())
			}
		},
	}
}

// fkMandatoryParentOutOfSubsetWithoutSkip is fkSeveralToSameParent without
// skip_foreign_key_violations: a mandatory key to a parent left out of the subset leaves
// the row out just the same.
//
// The option chooses what to do with a row the destination refuses; a mandatory key to a
// parent that is not there cannot be written at all, so failing the run would only trade
// a row that has to go for a job that never finishes. A live source makes this ordinary
// rather than exceptional — a parent and its child created between the read of the parent
// table and the read of the child table select the child without its parent, and no
// ordering of the tables prevents it. The referential integrity check at the end of the
// run stays the one place that judges the destination as a whole.
func fkMandatoryParentOutOfSubsetWithoutSkip() *Case {
	return &Case{
		ID:       "fk-mandatory-parent-out-of-subset-without-skip",
		Priority: P1,
		Title:    "FK obligatoire vers un parent hors subset, sans skip_foreign_key_violations",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "TRANSFERT",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "station_arrivee_id", Type: schema.Int64()},
					{Name: "station_depart_id", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_transfert_arrivee", "station_arrivee_id", "STATION"),
					foreignKeyToID("fk_transfert_depart", "station_depart_id", "STATION"),
				},
			},
		},
		Job: Job{
			Where:                    map[string]string{"STATION": fmt.Sprintf("id = %d", stationKept)},
			SubsetByForeignKeys:      true,
			SkipForeignKeyViolations: false,
		},
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row("TRANSFERT", []any{i, stationKept, stationKept}, Kept())
				// La seconde clé, obligatoire, sort du subset : la ligne est retenue par
				// la première mais ne peut pas être écrite.
				emit.Row("TRANSFERT", []any{300 + i, stationKept, stationDropped}, Dropped())
			}
		},
	}
}

// fkCompositePartiallyNull: a two-column nullable foreign key under MATCH SIMPLE. A
// reference with one NULL column constrains nothing and must come through unchanged; a
// full reference to a lot out of the subset must be cleared.
func fkCompositePartiallyNull() *Case {
	return &Case{
		ID:       "fk-composite-partially-null",
		Priority: P1,
		Title:    "FK composite partiellement nulle (MATCH SIMPLE) et référence composite hors subset",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "LOT",
				Columns: []schema.Column{
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "numero", Type: schema.Int32()},
				},
				PrimaryKey:  []string{stationIDColumn, "numero"},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_lot_station", stationIDColumn, "STATION")},
			},
			{
				Name: "COLIS",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "lot_station_id", Type: schema.Int64(), Nullable: true},
					{Name: "lot_numero", Type: schema.Int32(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_colis_station", stationIDColumn, "STATION"),
					{
						Name: "fk_colis_lot", Columns: []string{"lot_station_id", "lot_numero"},
						RefTable: "LOT", RefColumns: []string{stationIDColumn, "numero"},
					},
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for n := int64(1); n <= 10; n++ {
				emit.Row("LOT", []any{stationKept, n}, Kept())
				emit.Row("LOT", []any{stationDropped, n}, Dropped())
			}
			for n := int64(1); n <= 10; n++ {
				emit.Row("COLIS", []any{n, stationKept, stationKept, n}, Kept())
				emit.Row("COLIS", []any{100 + n, stationKept, nil, nil}, Kept())
				// Référence incomplète : ne contraint rien, y compris vers une station hors subset.
				emit.Row("COLIS", []any{200 + n, stationKept, stationDropped, nil}, Kept())
				emit.Row("COLIS", []any{300 + n, stationKept, stationDropped, n}, Kept("lot_station_id", "lot_numero"))
			}
		},
	}
}

// fkCompositeColumnOrder: a composite key whose columns are not declared in the order
// they stand in the table. COLIS holds lot_numero before lot_station_id, and the key pairs
// lot_station_id with LOT.station_id and lot_numero with LOT.numero. Whatever reads the
// key must pair the columns in the order of the constraint: pairing them in the order of
// the table joins COLIS on numero = station_id, keeps parcels of the lot left out and
// drops parcels of the lot kept, without an error.
func fkCompositeColumnOrder() *Case {
	return &Case{
		ID:       "fk-composite-column-order",
		Priority: P1,
		Title:    "FK composite déclarée dans un autre ordre que les colonnes de la table",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "LOT",
				Columns: []schema.Column{
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "numero", Type: schema.Int64()},
				},
				PrimaryKey:  []string{stationIDColumn, "numero"},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_lot_station", stationIDColumn, "STATION")},
			},
			{
				Name: "COLIS",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "lot_numero", Type: schema.Int64()},
					{Name: "lot_station_id", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{{
					Name: "fk_colis_lot", Columns: []string{"lot_station_id", "lot_numero"},
					RefTable: "LOT", RefColumns: []string{stationIDColumn, "numero"},
				}},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for n := int64(1); n <= 10; n++ {
				emit.Row("LOT", []any{stationKept, n}, Kept())
				emit.Row("LOT", []any{stationDropped, n}, Dropped())
			}
			// Read the wrong way round, parcel n of either station points at lot 1 or 2 of
			// station n: some parcels of the lot kept would go, some of the other would stay.
			for n := int64(1); n <= 10; n++ {
				emit.Row("COLIS", []any{n, n, stationKept}, Kept())
				emit.Row("COLIS", []any{100 + n, n, stationDropped}, Dropped())
			}
		},
	}
}

// fkVirtual: the relation exists only as a virtual foreign key of the job, as legacy
// schemas force users to declare it. No constraint guards the destination, so an engine
// that relies on the database to reject orphans writes them.
func fkVirtual() *Case {
	return &Case{
		ID:       "fk-virtual",
		Priority: P1,
		Title:    "FK virtuelle nullable vers des lignes hors subset, sans contrainte en base",
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
				Name: "EVENEMENT",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "commande_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_evenement_station", stationIDColumn, "STATION"),
					{
						Name: "vfk_evenement_commande", Columns: []string{"commande_id"},
						RefTable: commandeTable, RefColumns: []string{idColumn}, Virtual: true,
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
				emit.Row("EVENEMENT", []any{i, stationKept, i}, Kept())
				emit.Row("EVENEMENT", []any{100 + i, stationKept, nil}, Kept())
				emit.Row("EVENEMENT", []any{200 + i, stationKept, 1000 + i}, Kept("commande_id"))
				emit.Row("EVENEMENT", []any{1000 + i, stationDropped, 1000 + i}, Dropped())
			}
		},
	}
}

// fkParentKeyCollation: the key of CATALOGUE is text compared with a collation of its own,
// where "REF-A-01" and "ref-a-01" are two different codes. Athanor checks in the
// destination, batch by batch, that the parent of a mandatory foreign key is there, and
// sends the keys of the batch as parameters of a derived table: the comparison must stay
// the one of the column, not the one the connection happens to use.
func fkParentKeyCollation() *Case {
	// Byte for byte on both databases: "REF-A-01" and "ref-a-01" are two codes.
	binary := map[schema.Dialect]string{schema.MySQL: "utf8mb4_bin", schema.Postgres: `"C"`}
	return &Case{
		ID:       "fk-parent-key-collation",
		Priority: P1,
		Title:    "Clé étrangère textuelle à collation propre : le parent se compare comme la base le fait",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: "CATALOGUE",
				Columns: []schema.Column{
					{Name: "code", Type: schema.Varchar(20), Collation: binary},
					{Name: stationIDColumn, Type: schema.Int64()},
				},
				PrimaryKey:  []string{"code"},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_catalogue_station", stationIDColumn, "STATION")},
			},
			{
				Name: "ARTICLE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "catalogue_code", Type: schema.Varchar(20), Collation: binary},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_article_station", stationIDColumn, "STATION"),
					{
						Name: "fk_article_catalogue", Columns: []string{"catalogue_code"},
						RefTable: "CATALOGUE", RefColumns: []string{"code"},
					},
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 5; i++ {
				emit.Row("CATALOGUE", []any{fmt.Sprintf("REF-A-%02d", i), stationKept}, Kept())
				emit.Row("CATALOGUE", []any{fmt.Sprintf("REF-B-%02d", i), stationDropped}, Dropped())
			}
			id := int64(0)
			for i := int64(1); i <= 5; i++ {
				id++
				emit.Row("ARTICLE", []any{id, stationKept, fmt.Sprintf("REF-A-%02d", i)}, Kept())
				// Le code n'existe que pour l'autre station : article retenu par le subset,
				// sans parent en destination.
				id++
				emit.Row("ARTICLE", []any{id, stationKept, fmt.Sprintf("REF-B-%02d", i)}, Dropped())
				// Même code à la casse près : deux codes différents pour cette collation,
				// donc un orphelin de la source, à écarter comme tel.
				id++
				emit.Row("ARTICLE", []any{id, stationKept, fmt.Sprintf("ref-a-%02d", i)}, Dropped())
				id++
				emit.Row("ARTICLE", []any{id, stationDropped, fmt.Sprintf("REF-B-%02d", i)}, Dropped())
			}
		},
	}
}
