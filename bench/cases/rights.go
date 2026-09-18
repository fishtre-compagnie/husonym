package cases

import (
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// rightsCases run with accounts that lack what their role needs. The decision is that a
// run stops at its start on a blocking privilege check, with a message saying which
// privilege is missing; failing later on the first refused statement, or retrying it for
// minutes, is a gap. (The read-only source is not a case: every run of the bench reads a
// source set to refuse writes.)
func rightsCases() []*Case {
	return []*Case{rightsDestinationReadOnlyAccount(), rightsDestinationCannotSuspendForeignKeys()}
}

func articlesToWrite() (tables []*schema.Table, seed func(p Params, emit Emitter)) {
	tables = []*schema.Table{{
		Name: "ARTICLE",
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "libelle", Type: schema.Varchar(40)},
		},
		PrimaryKey: []string{idColumn},
	}}
	seed = func(p Params, emit Emitter) {
		for i := int64(1); i <= 10; i++ {
			emit.Row("ARTICLE", []any{i, "article"}, Kept())
		}
	}
	return tables, seed
}

func rightsDestinationReadOnlyAccount() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:       "rights-destination-read-only-account",
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:  "Destination sans droit d'écriture : arrêt au démarrage du run sur le contrôle des droits",
		Tables: tables,
		DestinationGrants: map[schema.Dialect][]string{
			schema.MySQL: {"GRANT SELECT ON {db}.* TO {user}"},
			schema.Postgres: {
				"GRANT USAGE ON SCHEMA {db} TO {user}",
				"GRANT SELECT ON ALL TABLES IN SCHEMA {db} TO {user}",
			},
		},
		ExpectRunError: map[schema.Dialect]string{
			schema.MySQL:    "privilege check",
			schema.Postgres: "privilege check",
		},
		Seed: seed,
	}
}

// rightsDestinationCannotSuspendForeignKeys: the account may write every table but is not
// allowed to suspend foreign keys, which on PostgreSQL takes a superuser or GRANT SET ON
// PARAMETER session_replication_role. Athanor writes a table in one pass with the keys
// suspended, so it cannot run there and must say so before writing anything. Benthos does
// not suspend them: the same account is enough for it, and the check must not stop it.
func rightsDestinationCannotSuspendForeignKeys() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:       "rights-destination-cannot-suspend-foreign-keys",
		Dialects: postgresOnly,
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:  "Destination sans le droit de suspendre les FK : Athanor s'arrête au démarrage, Benthos passe",
		Tables: tables,
		DestinationGrants: map[schema.Dialect][]string{
			schema.Postgres: {
				"GRANT USAGE ON SCHEMA {db} TO {user}",
				"GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA {db} TO {user}",
			},
		},
		ExpectRunError: map[schema.Dialect]string{schema.Postgres: "cannot suspend foreign keys"},
		FailingEngines: []string{"athanor"},
		Seed:           seed,
	}
}
