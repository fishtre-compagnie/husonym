package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// nomColumn is the column the rule cases share.
const nomColumn = "nom"

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
		rulePseudoConsistent(),
		rulePseudoMatchesNative(),
	}
}

// pseudoOnAthanorOnly: the pseudo functions derive from the consistency scope of Athanor.
// Benthos has none: its run must stop at its start, naming the rule.
func pseudoOnAthanorOnly(c *Case) *Case {
	c.ExpectRunError = sameRunError("benthos cannot run the pseudo functions")
	c.FailingEngines = []string{"benthos"}
	return c
}

// rulePseudoConsistent: a rule using the pseudo functions gives the same output for the
// same source value on the first page, on the third, and in another table — what a
// pseudonym kept from one row to the next used to give, without keeping anything.
func rulePseudoConsistent() *Case {
	const people, contacts = "CLIENT", "CONTACT"
	columns := func(name string) *schema.Table {
		return &schema.Table{
			Name: name,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: nomColumn, Type: schema.Varchar(80)},
				{Name: referenceColumn, Type: schema.Varchar(40)},
				{Name: "segment", Type: schema.Varchar(20)},
			},
			PrimaryKey: []string{idColumn},
		}
	}
	rules := []Rule{RuleConsistent, RuleNotInSourceSet}
	spec := map[string]ColumnSpec{
		nomColumn:       {Transformer: transformJavascript(`return pseudo.lastName(value);`), Rules: rules},
		referenceColumn: {Transformer: transformJavascript(`return "C-" + pseudo.hash(value, "client").slice(0, 12);`), Rules: rules},
		"segment": {Transformer: transformJavascript(`return pseudo.pick(["or", "argent", "bronze"], input.nom, "segment");`),
			Rules: []Rule{RuleConsistent}},
	}
	// 37 distinct people, spread over every page of both tables.
	const distinct = 37
	row := func(i int64) []any {
		n := i % distinct
		return []any{i, fmt.Sprintf("Nom-Source-%d", n), fmt.Sprintf("REF-SOURCE-%d", n), fmt.Sprintf("Nom-Source-%d", n)}
	}
	return pseudoOnAthanorOnly(&Case{
		ID:       "js-pseudo-consistent",
		Priority: P1,
		Title:    "Règle pseudo.* : même valeur source, même sortie, d'une page à l'autre et d'une table à l'autre",
		Tables:   []*schema.Table{columns(people), columns(contacts)},
		Job:      Job{Columns: map[string]map[string]ColumnSpec{people: spec, contacts: spec}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(5*p.PageLimit); i++ {
				emit.Row(people, row(i), Kept())
			}
			for i := int64(1); i <= int64(2*p.PageLimit); i++ {
				emit.Row(contacts, row(i), Kept())
			}
		},
	})
}

// rulePseudoMatchesNative: pseudo.<kind>(value) returns what the native transformer
// returns. Two tables hold the same rows: one goes through the native transformers, the
// other through rules; RuleConsistent compares the columns of the same name.
func rulePseudoMatchesNative() *Case {
	const native, script = "NATIF", "SCRIPT"
	table := func(name string) *schema.Table {
		return &schema.Table{
			Name: name,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "prenom", Type: schema.Varchar(80)},
				{Name: nomColumn, Type: schema.Varchar(80)},
				{Name: "email", Type: schema.Varchar(120)},
				{Name: "telephone", Type: schema.Varchar(30)},
				{Name: "ville", Type: schema.Varchar(80)},
			},
			PrimaryKey: []string{idColumn},
		}
	}
	rules := []Rule{RuleConsistent, RuleNotInSourceSet}
	nativeSpec := map[string]ColumnSpec{
		"prenom": {Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{
			GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{}}}, Rules: rules},
		nomColumn: {Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformLastNameConfig{
			TransformLastNameConfig: &mgmtv1alpha1.TransformLastName{}}}, Rules: rules},
		"email": {Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{
			TransformEmailConfig: &mgmtv1alpha1.TransformEmail{}}}, Rules: rules},
		"telephone": {Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformE164PhoneNumberConfig{
			TransformE164PhoneNumberConfig: &mgmtv1alpha1.TransformE164PhoneNumber{}}}, Rules: rules},
		"ville": {Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateCityConfig{
			GenerateCityConfig: &mgmtv1alpha1.GenerateCity{}}}, Rules: rules},
	}
	scriptSpec := map[string]ColumnSpec{
		"prenom":    {Transformer: transformJavascript(`return pseudo.firstName(value);`), Rules: rules},
		nomColumn:   {Transformer: transformJavascript(`return pseudo.lastName(value);`), Rules: rules},
		"email":     {Transformer: transformJavascript(`return pseudo.email(value);`), Rules: rules},
		"telephone": {Transformer: transformJavascript(`return pseudo.phone(value);`), Rules: rules},
		"ville":     {Transformer: transformJavascript(`return pseudo.city(value);`), Rules: rules},
	}
	return pseudoOnAthanorOnly(&Case{
		ID:       "js-pseudo-matches-native",
		Priority: P1,
		Title:    "Règle pseudo.* : exactement la sortie du transformer natif pour la même valeur",
		Tables:   []*schema.Table{table(native), table(script)},
		Job:      Job{Columns: map[string]map[string]ColumnSpec{native: nativeSpec, script: scriptSpec}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(3*p.PageLimit); i++ {
				// Two spellings of each email: the case matters to an email.
				email := fmt.Sprintf("personne.%d@source.invalid", i/2)
				if i%2 == 0 {
					email = fmt.Sprintf("Personne.%d@Source.invalid", i/2)
				}
				values := []any{i, fmt.Sprintf("Prenom-Source-%d", i), fmt.Sprintf("Nom-Source-%d", i), email,
					fmt.Sprintf("+3361%07d", i), fmt.Sprintf("Ville-Source-%d", i)}
				emit.Row(native, values, Kept())
				emit.Row(script, values, Kept())
			}
		},
	})
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
				{Name: nomColumn, Type: schema.Varchar(40)},
				{Name: "ville", Type: schema.Varchar(40)},
				{Name: "email", Type: schema.Varchar(40)},
				{Name: "telephone", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{table: {
			// The global object the functions hang from.
			nomColumn: {Transformer: keep("neosync.precedent", "neosync.precedent"), Rules: rules},
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
