package cases

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// whereClauseCases exercise what the shared query builder does with the subset clause a
// user typed: it is pasted next to the conditions the builder adds itself.
func whereClauseCases() []*Case {
	return []*Case{
		whereOrPaginated(),
		whereOrTwoRoots(),
		whereUnqualifiedUnderJoin(),
	}
}

// whereOrPaginated: a top-level OR next to the page condition. Without parentheses the
// next page reads "a OR (b AND id > last)" and starts the 'a' rows over.
func whereOrPaginated() *Case {
	return &Case{
		ID:       "where-or-paginated",
		Priority: P1,
		Title:    "WHERE avec OR de premier niveau, combiné à la condition de page",
		Tables: []*schema.Table{{
			Name: "PRODUIT",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "categorie", Type: schema.Varchar(10)},
			},
			PrimaryKey: []string{idColumn},
		}},
		Job: Job{Where: map[string]string{"PRODUIT": "categorie = 'a' OR categorie = 'b'"}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(3*p.PageLimit); i++ {
				switch i % 3 {
				case 0:
					emit.Row("PRODUIT", []any{i, "a"}, Kept())
				case 1:
					emit.Row("PRODUIT", []any{i, "b"}, Kept())
				default:
					emit.Row("PRODUIT", []any{i, "c"}, Dropped())
				}
			}
		},
	}
}

const (
	stationTwin = int64(3) // second station of the subset, selected by the OR
	clientKept  = int64(1)
	clientOut   = int64(2)
)

func clientTable() *schema.Table {
	return &schema.Table{
		Name: "CLIENT",
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "actif", Type: schema.Bool()},
		},
		PrimaryKey: []string{idColumn},
	}
}

func seedClients(emit Emitter) {
	emit.Row("CLIENT", []any{clientKept, int64(1)}, Kept())
	emit.Row("CLIENT", []any{clientOut, int64(0)}, Dropped())
}

func commandeOfStationAndClient() *schema.Table {
	return &schema.Table{
		Name: commandeTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: stationIDColumn, Type: schema.Int64()},
			{Name: "client_id", Type: schema.Int64()},
		},
		PrimaryKey: []string{idColumn},
		ForeignKeys: []schema.ForeignKey{
			foreignKeyToID("fk_commande_station", stationIDColumn, "STATION"),
			foreignKeyToID("fk_commande_client", "client_id", "CLIENT"),
		},
	}
}

// whereOrTwoRoots: two subset roots, one of them with a top-level OR. Their clauses are
// ANDed without parentheses, so "s.id = 1 OR s.id = 3 AND c.actif = 1" lets through the
// orders of a kept station placed by a client left out — whichever clause comes first.
func whereOrTwoRoots() *Case {
	return &Case{
		ID:       "where-or-two-roots",
		Priority: P1,
		Title:    "WHERE avec OR sur une racine, combiné à une seconde racine : fuite hors subset",
		Tables:   []*schema.Table{stationTable(), clientTable(), commandeOfStationAndClient()},
		Job: Job{
			Where: map[string]string{
				"STATION": fmt.Sprintf("id = %d OR id = %d", stationKept, stationTwin),
				"CLIENT":  "actif = 1",
			},
			SubsetByForeignKeys:      true,
			SkipForeignKeyViolations: true,
		},
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			emit.Row("STATION", []any{stationTwin, "seconde station du subset"}, Kept())
			seedClients(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, stationKept, clientKept}, Kept())
				emit.Row(commandeTable, []any{100 + i, stationTwin, clientKept}, Kept())
				emit.Row(commandeTable, []any{200 + i, stationKept, clientOut}, Dropped())
				emit.Row(commandeTable, []any{300 + i, stationTwin, clientOut}, Dropped())
				emit.Row(commandeTable, []any{400 + i, stationDropped, clientKept}, Dropped())
			}
		},
	}
}

// whereUnqualifiedUnderJoin: the MySQL qualifier only prefixes the left side of
// comparisons. "ferme_le IS NULL" stays bare, and once the child table is joined to the
// root it names a column both tables have.
func whereUnqualifiedUnderJoin() *Case {
	return &Case{
		ID:       "where-unqualified-under-join",
		Priority: P2,
		Title:    "WHERE avec IS NULL sur une colonne présente aussi dans la table fille (colonne ambiguë)",
		Tables: []*schema.Table{
			{
				Name: "STATION",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "ferme_le", Type: schema.Date(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
			},
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "ferme_le", Type: schema.Date(), Nullable: true},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_commande_station", stationIDColumn, "STATION")},
			},
		},
		Job: Job{
			Where:                    map[string]string{"STATION": "ferme_le IS NULL"},
			SubsetByForeignKeys:      true,
			SkipForeignKeyViolations: true,
		},
		Seed: func(p Params, emit Emitter) {
			emit.Row("STATION", []any{stationKept, nil}, Kept())
			emit.Row("STATION", []any{stationDropped, "2020-01-31"}, Dropped())
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, stationKept, nil}, Kept())
				emit.Row(commandeTable, []any{100 + i, stationKept, "2023-06-30"}, Kept())
				emit.Row(commandeTable, []any{1000 + i, stationDropped, nil}, Dropped())
			}
		},
	}
}
