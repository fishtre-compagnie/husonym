package cases

import (
	"strings"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// identifierCases use the names real schemas end up with: reserved words, spaces,
// dashes, accents, mixed case, the longest name MySQL accepts, and a column named like
// the aliases the query builder generates.
func identifierCases() []*Case {
	return []*Case{identifiersQuoting()}
}

func identifiersQuoting() *Case {
	longName := strings.Repeat("colonne_tres_longue_", 3) + "64ch"
	return &Case{
		ID:       "identifiers-quoting",
		Priority: P2,
		Title:    "Identifiants : mots réservés, espaces, tirets, accents, casse mixte, 64 caractères, sous subset par FK",
		Tables: []*schema.Table{
			{
				Name: "order",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "key", Type: schema.Varchar(10)},
				},
				PrimaryKey: []string{idColumn},
			},
			{
				Name: "Order Line-é",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "order id", Type: schema.Int64()},
					{Name: "group", Type: schema.Varchar(10)},
					{Name: "select", Type: schema.Varchar(10), Nullable: true},
					{Name: "code-postal", Type: schema.Varchar(10)},
					{Name: "référence", Type: schema.Varchar(10)},
					{Name: "MixedCase", Type: schema.Varchar(10)},
					{Name: "t_0123456789abcdef", Type: schema.Varchar(10)},
					{Name: longName, Type: schema.Varchar(10)},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{{
					Name: "fk order line", Columns: []string{"order id"}, RefTable: "order", RefColumns: []string{idColumn},
				}},
			},
		},
		Job: Job{
			Where:                    map[string]string{"order": "`key` = 'garde'"},
			SubsetByForeignKeys:      true,
			SkipForeignKeyViolations: true,
		},
		Seed: func(p Params, emit Emitter) {
			emit.Row("order", []any{int64(1), "garde"}, Kept())
			emit.Row("order", []any{int64(2), "écarte"}, Dropped())
			for i := int64(1); i <= 10; i++ {
				emit.Row("Order Line-é", []any{i, int64(1), "g", nil, "13100", "réf", "Mx", "alias", "long"}, Kept())
				emit.Row("Order Line-é", []any{100 + i, int64(2), "g", "s", "13100", "réf", "Mx", "alias", "long"}, Dropped())
			}
		},
	}
}
