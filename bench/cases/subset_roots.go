package cases

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// subsetRootsCases exercise subsets with several filtered tables: a row must satisfy
// every root it can reach.
func subsetRootsCases() []*Case {
	return []*Case{
		subsetTwoRoots(),
		subsetParentFilteredTwice(),
	}
}

func twoRootsJob() Job {
	return Job{
		Where: map[string]string{
			"STATION":       fmt.Sprintf("id = %d", stationKept),
			clientTableName: "actif = 1",
		},
		SubsetByForeignKeys:      true,
		SkipForeignKeyViolations: true,
	}
}

// subsetTwoRoots: an order references both roots and must belong to both subsets.
func subsetTwoRoots() *Case {
	return &Case{
		ID:       "subset-two-roots",
		Priority: P1,
		Title:    "Deux racines de subset : une ligne doit appartenir aux deux",
		Tables:   []*schema.Table{stationTable(), clientTable(), commandeOfStationAndClient()},
		Job:      twoRootsJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			seedClients(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, stationKept, clientKept}, Kept())
				emit.Row(commandeTable, []any{100 + i, stationKept, clientOut}, Dropped())
				emit.Row(commandeTable, []any{200 + i, stationDropped, clientKept}, Dropped())
			}
		},
	}
}

// subsetParentFilteredTwice: CONTRAT is filtered by both roots. AVENANT reaches STATION
// through CONTRAT but CLIENT directly, so its query never applies the client filter to
// its contract: an amendment signed by a kept client on a contract of a client left out
// is selected while its mandatory parent is not. "The foreign key on the subset path is
// guaranteed by the join" does not hold with two roots.
func subsetParentFilteredTwice() *Case {
	return &Case{
		ID:       "subset-parent-filtered-twice",
		Priority: P1,
		Title:    "Parent filtré par deux racines, enfant qui ne le rejoint que par une",
		Tables: []*schema.Table{
			stationTable(),
			clientTable(),
			{
				Name: "CONTRAT",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "client_id", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_contrat_station", stationIDColumn, "STATION"),
					foreignKeyToID("fk_contrat_client", "client_id", clientTableName),
				},
			},
			{
				Name: "AVENANT",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "contrat_id", Type: schema.Int64()},
					{Name: "client_id", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_avenant_contrat", "contrat_id", "CONTRAT"),
					foreignKeyToID("fk_avenant_client", "client_id", clientTableName),
				},
			},
		},
		Job: twoRootsJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			seedClients(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row("CONTRAT", []any{i, stationKept, clientKept}, Kept())
				emit.Row("CONTRAT", []any{100 + i, stationKept, clientOut}, Dropped())
				emit.Row("AVENANT", []any{i, i, clientKept}, Kept())
				// Contrat d'un client hors subset, avenant signé par un client du subset.
				emit.Row("AVENANT", []any{100 + i, 100 + i, clientKept}, Dropped())
			}
		},
	}
}
