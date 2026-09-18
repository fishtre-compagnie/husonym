package cases

import (
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// rightsCases run with accounts that lack what their role needs. The decision is that a
// run stops at its start on a blocking privilege check, with a message saying which
// privilege is missing; failing later on the first refused statement, or retrying it for
// minutes, is a gap. (The read-only source is not a case: every run of the bench reads a
// source set to super_read_only.)
func rightsCases() []*Case {
	return []*Case{rightsDestinationReadOnlyAccount()}
}

func rightsDestinationReadOnlyAccount() *Case {
	return &Case{
		ID:       "rights-destination-read-only-account",
		Dialects: mysqlOnly,
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title: "Destination sans droit d'écriture : arrêt au démarrage du run sur le contrôle des droits",
		Tables: []*schema.Table{{
			Name: "ARTICLE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "libelle", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		DestinationGrants: []string{"GRANT SELECT ON {db}.* TO {user}"},
		ExpectRunError:    "privilege check",
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 10; i++ {
				emit.Row("ARTICLE", []any{i, "article"}, Kept())
			}
		},
	}
}
