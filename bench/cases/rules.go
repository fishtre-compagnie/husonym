package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// ruleCases are the rules users write themselves, in JavaScript: what a script may reach,
// how long it may run, and what it may keep from one row to the next.
func ruleCases() []*Case {
	return []*Case{
		transformerFailure("js-endless-script",
			"Script sans fin : le run échoue au bout de la limite de temps, au lieu de bloquer la table",
			"libelle", transformJavascript(`while (true) {}`), sameRunError("ran past its time limit")),
		// Before the guard, require() read the file and ran it as JavaScript: a host name
		// came back in the error message.
		transformerFailure("js-reads-host-file",
			"Script qui charge un fichier du worker : refusé, rien n'est lu",
			"libelle", transformJavascript(`return String(require("/etc/hostname"));`),
			sameRunError("scripts cannot load files")),
		ruleNoStateAcrossRows(),
	}
}

// ruleNoStateAcrossRows: a rule keeps its state for one row only. Each odd row keeps its
// source value through one way a script can hold state, and the even row after it returns
// whatever it finds there, or a value of its own. A value found there is a source value
// written into another person's row: RuleNotInSourceSet catches it. One column per way of
// holding state, over five pages.
func ruleNoStateAcrossRows() *Case {
	const table = "PERSONNE"
	keep := func(set, get string) *mgmtv1alpha1.TransformerConfig {
		return transformJavascript(fmt.Sprintf(`
			if (input.id %% 2 === 1) { %s = value; return "X-" + input.id; }
			var found = %s;
			return (typeof found === "string") ? found : "Y-" + input.id;`, set, get))
	}
	rules := []Rule{RuleNotInSourceSet, RuleUnique}
	return &Case{
		ID:       "js-no-state-across-rows",
		Priority: P1,
		Title:    "Règle JavaScript : aucun état d'une ligne à la suivante, par aucun moyen",
		Tables: []*schema.Table{{
			Name: table,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "nom", Type: schema.Varchar(40)},
				{Name: "ville", Type: schema.Varchar(40)},
				{Name: "email", Type: schema.Varchar(40)},
				{Name: "telephone", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{table: {
			// The global object the functions hang from.
			"nom": {Transformer: keep("neosync.precedent", "neosync.precedent"), Rules: rules},
			// A variable assigned without a declaration.
			"ville": {Transformer: keep("villePrecedente",
				`(typeof villePrecedente === "undefined") ? undefined : villePrecedente`), Rules: rules},
			"email": {Transformer: keep("globalThis.emailPrecedent", "globalThis.emailPrecedent"), Rules: rules},
			// A property every object inherits.
			"telephone": {Transformer: keep("Object.prototype.telPrecedent", "({}).telPrecedent"), Rules: rules},
		}}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(5*p.PageLimit); i++ {
				emit.Row(table, []any{i,
					fmt.Sprintf("Nom-Source-%d", i),
					fmt.Sprintf("Ville-Source-%d", i),
					fmt.Sprintf("email.source.%d@source.invalid", i),
					fmt.Sprintf("Tel-Source-%d", i),
				}, Kept())
			}
		},
	}
}

// sameRunError expects the same message on every database: an error of the engine, not
// one the database prints in its own words.
func sameRunError(message string) map[schema.Dialect]string {
	return map[schema.Dialect]string{schema.MySQL: message, schema.Postgres: message}
}
