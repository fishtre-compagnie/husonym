package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// transformerCases check what transformers must guarantee whatever the engine: no
// personal value copied as is, constraints of the destination respected or the run
// failed out loud, and paging that follows source values, not transformed ones.
func transformerCases() []*Case {
	return []*Case{
		transformersGeneratePersonalData(),
		transformerNull(),
		transformerFailure("tr-constant-on-unique-column",
			"Transformer constant sur une colonne unique : échec explicite, jamais de ligne écartée en silence",
			"code", generateJavascript(`return "constante";`), map[schema.Dialect]string{
				schema.MySQL:    "Duplicate entry",
				schema.Postgres: "duplicate key value violates unique constraint",
			}),
		transformerFailure("tr-output-longer-than-column",
			"Sortie plus longue que la colonne : échec explicite, jamais de troncature silencieuse",
			"code", generateJavascript(`return "sortie nettement plus longue que dix caractères";`),
			map[schema.Dialect]string{
				schema.MySQL:    "Data too long",
				schema.Postgres: "value too long for type character varying",
			}),
		// The same two, with transformers of the product: the pre-flight check says it before
		// the run, which a rule's code cannot tell.
		expectingFinding(transformerFailure("tr-uuid-longer-than-column",
			"UUID généré dans une colonne de 10 caractères : avertissement au pré-vol, puis échec explicite",
			"code", &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateUuidConfig{
				GenerateUuidConfig: &mgmtv1alpha1.GenerateUuid{},
			}},
			map[schema.Dialect]string{
				schema.MySQL:    "Data too long",
				schema.Postgres: "value too long for type character varying",
			}), mgmtv1alpha1.PreflightFinding_KIND_OUTPUT_TOO_LONG),
		expectingFinding(transformerFailure("tr-single-category-on-unique-column",
			"Catégorie unique sur une colonne unique : avertissement au pré-vol, puis échec explicite",
			"code", categorical("constante"), map[schema.Dialect]string{
				schema.MySQL:    "Duplicate entry",
				schema.Postgres: "duplicate key value violates unique constraint",
			}), mgmtv1alpha1.PreflightFinding_KIND_CONSTANT_ON_UNIQUE),
		transformerFailure("tr-null-on-not-null-column",
			"Transformer Null sur une colonne NOT NULL : échec explicite, jamais de valeur par défaut implicite",
			"libelle", nullTransformer(), map[schema.Dialect]string{
				schema.MySQL:    "cannot be null",
				schema.Postgres: "violates not-null constraint",
			}),
		transformerOnOrderColumn(),
		transformerOnPrimaryKey(),
		transformedKeyOfDiscardedRow(),
		transformedKeySelfReference(),
		transformedKeysInCycle(),
	}
}

func generateJavascript(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateJavascriptConfig{
		GenerateJavascriptConfig: &mgmtv1alpha1.GenerateJavascript{Code: code},
	}}
}

func transformJavascript(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
	}}
}

func categorical(categories string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateCategoricalConfig{
		GenerateCategoricalConfig: &mgmtv1alpha1.GenerateCategorical{Categories: &categories},
	}}
}

// expectingFinding has a case of transformerFailure expect a warning of the pre-flight
// check on the column it transforms.
func expectingFinding(c *Case, kind mgmtv1alpha1.PreflightFinding_Kind) *Case {
	c.ExpectFindings = []ExpectedFinding{expectFinding(kind, findingWarning, "ARTICLE")}
	return c
}

func nullTransformer() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{Nullconfig: &mgmtv1alpha1.Null{}}}
}

// transformersGeneratePersonalData: generated emails and first names over several
// pages. No source value may survive, and the unique index on the email must hold.
// Source values are shaped so that no generator can produce them by chance.
func transformersGeneratePersonalData() *Case {
	return &Case{
		ID:       "tr-generate-personal-data",
		Priority: P1,
		Title:    "Email et prénom générés : aucune valeur source recopiée, unicité de l'email respectée",
		Tables: []*schema.Table{{
			Name: clientTableName,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "email", Type: schema.Varchar(120)},
				{Name: "prenom", Type: schema.Varchar(60)},
			},
			PrimaryKey: []string{idColumn},
			Indexes:    []schema.Index{{Name: "uq_client_email", Columns: []string{"email"}, Unique: true}},
		}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{clientTableName: {
			"email": {
				Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
					GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
				}},
				Rules: []Rule{RuleNotInSourceSet, RuleUnique},
			},
			"prenom": {
				Transformer: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{
					GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{},
				}},
				Rules: []Rule{RuleNotInSourceSet},
			},
		}}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(5*p.PageLimit); i++ {
				email := fmt.Sprintf("personne.reelle.%d@source.invalid", i)
				emit.Row(clientTableName, []any{i, email, fmt.Sprintf("Prénom-Source-%d", i)}, Kept())
			}
		},
	}
}

// transformerNull: the Null transformer on a nullable column holding values.
func transformerNull() *Case {
	return &Case{
		ID:       "tr-null-on-nullable-column",
		Priority: P1,
		Title:    "Transformer Null sur une colonne nullable : NULL réel, pas la chaîne 'null'",
		Tables: []*schema.Table{{
			Name: clientTableName,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "commentaire", Type: schema.Varchar(60), Nullable: true},
			},
			PrimaryKey: []string{idColumn},
		}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{clientTableName: {
			"commentaire": {Transformer: nullTransformer(), Rules: []Rule{RuleNull}},
		}}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row(clientTableName, []any{i, fmt.Sprintf("remarque personnelle %d", i)}, Kept())
			}
		},
	}
}

// transformerFailure: a transformer whose output the destination must refuse. The only
// correct outcome is a failed run naming the error: a completed run means rows were
// dropped, truncated or defaulted without a word.
func transformerFailure(id, title, column string, transformer *mgmtv1alpha1.TransformerConfig, runError map[schema.Dialect]string) *Case {
	return &Case{
		ID:       id,
		Priority: P1,
		Title:    title,
		Tables: []*schema.Table{{
			Name: "ARTICLE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "code", Type: schema.Varchar(10)},
				{Name: "libelle", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
			Indexes:    []schema.Index{{Name: "uq_article_code", Columns: []string{"code"}, Unique: true}},
		}},
		Job:            Job{Columns: map[string]map[string]ColumnSpec{"ARTICLE": {column: {Transformer: transformer}}}},
		ExpectRunError: runError,
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row("ARTICLE", []any{i, fmt.Sprintf("C%04d", i), fmt.Sprintf("article %d", i)}, Kept())
			}
		},
	}
}

// transformerOnOrderColumn: the primary key, which orders the pages, is transformed.
// The next page must resume after the source value of the last row: resuming after its
// transformed value, far above every source key, would end the table after one page.
func transformerOnOrderColumn() *Case {
	const shift = 1_000_000
	return &Case{
		ID:       "tr-order-column-transformed",
		Priority: P1,
		Title:    "Colonne de tri transformée : la reprise doit suivre la valeur source",
		Tables: []*schema.Table{{
			Name: "DOSSIER",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: referenceColumn, Type: schema.Varchar(20)},
			},
			PrimaryKey: []string{idColumn},
			Indexes:    []schema.Index{{Name: "uq_dossier_reference", Columns: []string{referenceColumn}, Unique: true}},
		}},
		Identity: map[string][]string{"DOSSIER": {referenceColumn}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{"DOSSIER": {
			idColumn: {
				Transformer: transformJavascript(fmt.Sprintf("return value + %d;", shift)),
				Rules:       []Rule{RuleNotInSourceSet, RuleUnique},
			},
		}}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(2*p.PageLimit+p.PageLimit/2); i++ {
				emit.Row("DOSSIER", []any{i, fmt.Sprintf("D-%06d", i)}, Kept())
			}
		},
	}
}

// transformerOnPrimaryKey: the primary key of CLIENT is transformed and the foreign keys
// to it are left in passthrough, the way users configure it: the engine must carry each
// new key to the rows referencing it, nullable references included.
func transformerOnPrimaryKey() *Case {
	follows := ColumnSpec{Rules: []Rule{RuleFollowsParent}}
	return &Case{
		ID:       "tr-primary-key-transformed",
		Priority: P1,
		Title:    "Clé primaire transformée : les FK en passthrough doivent suivre la nouvelle clé",
		Tables: []*schema.Table{
			{
				Name: clientTableName,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: referenceColumn, Type: schema.Varchar(20)},
				},
				PrimaryKey: []string{idColumn},
				Indexes:    []schema.Index{{Name: "uq_client_reference", Columns: []string{referenceColumn}, Unique: true}},
			},
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "client_id", Type: schema.Int64()},
					{Name: "parrain_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_commande_client", "client_id", clientTableName),
					foreignKeyToID("fk_commande_parrain", "parrain_id", clientTableName),
				},
			},
		},
		Identity: map[string][]string{clientTableName: {referenceColumn}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{
			clientTableName: {idColumn: {
				Transformer: transformJavascript("return value + 1000000;"),
				Rules:       []Rule{RuleNotInSourceSet, RuleUnique},
			}},
			commandeTable: {"client_id": follows, "parrain_id": follows},
		}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row(clientTableName, []any{i, fmt.Sprintf("CL-%04d", i)}, Kept())
			}
			for i := int64(1); i <= 60; i++ {
				var parrain any
				if i%3 == 0 {
					parrain = i%20 + 1
				}
				emit.Row(commandeTable, []any{i, (i-1)%20 + 1, parrain}, Kept())
			}
		},
	}
}

// transformedKeyOfDiscardedRow: FACTURE holds a transformed primary key, and its own
// mandatory foreign key to COMMANDE is a diamond — the subset keeps invoices of the
// station whose order belongs to the other station, and those rows are left out at write
// time because their order is not in the destination. Their new key must not be published:
// a line following it would then point to an invoice the destination never received.
func transformedKeyOfDiscardedRow() *Case {
	follows := ColumnSpec{Rules: []Rule{RuleFollowsParent}}
	return &Case{
		ID:       "tr-key-of-discarded-row",
		Priority: P1,
		Title:    "Clé transformée d'une ligne écartée à l'écriture : la fille ne doit pas la suivre",
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
				Name: "FACTURE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: referenceColumn, Type: schema.Varchar(20)},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "commande_id", Type: schema.Int64()},
				},
				PrimaryKey: []string{idColumn},
				Indexes:    []schema.Index{{Name: "uq_facture_reference", Columns: []string{referenceColumn}, Unique: true}},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_facture_station", stationIDColumn, "STATION"),
					foreignKeyToID("fk_facture_commande", "commande_id", commandeTable),
				},
			},
			{
				Name: "LIGNE",
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: "facture_id", Type: schema.Int64(), Nullable: true},
					{Name: "libelle", Type: schema.Varchar(40)},
				},
				PrimaryKey:  []string{idColumn},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_ligne_facture", "facture_id", "FACTURE")},
			},
		},
		// FACTURE is identified by its reference: its key is transformed.
		Identity: map[string][]string{"FACTURE": {referenceColumn}},
		Job: Job{
			Where:                    map[string]string{"STATION": fmt.Sprintf("id = %d", stationKept)},
			SubsetByForeignKeys:      true,
			SkipForeignKeyViolations: true,
			Columns: map[string]map[string]ColumnSpec{
				"FACTURE": {idColumn: {
					Transformer: transformJavascript("return value + 1000000;"),
					Rules:       []Rule{RuleNotInSourceSet, RuleUnique},
				}},
				"LIGNE": {"facture_id": follows},
			},
		},
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 10; i++ {
				emit.Row(commandeTable, []any{i, stationKept}, Kept())
				emit.Row(commandeTable, []any{1000 + i, stationDropped}, Dropped())
			}
			for i := int64(1); i <= 10; i++ {
				// Facturée par la station du subset, pour une commande de la station du subset.
				emit.Row("FACTURE", []any{i, fmt.Sprintf("FA-%04d", i), stationKept, i}, Kept())
				// Facturée par la station du subset, pour une commande de l'autre station :
				// retenue par le subset, écartée à l'écriture faute de commande parente.
				emit.Row("FACTURE", []any{100 + i, fmt.Sprintf("FA-%04d", 100+i), stationKept, 1000 + i}, Dropped())
				// Hors subset de bout en bout.
				emit.Row("FACTURE", []any{1000 + i, fmt.Sprintf("FA-%04d", 1000+i), stationDropped, 1000 + i}, Dropped())
			}
			for i := int64(1); i <= 10; i++ {
				emit.Row("LIGNE", []any{i, i, "ligne d'une facture copiée"}, Kept())
				// Sa facture est écartée à l'écriture : la ligne reste, sans facture.
				emit.Row("LIGNE", []any{100 + i, 100 + i, "ligne d'une facture écartée"}, Kept("facture_id"))
				emit.Row("LIGNE", []any{1000 + i, 1000 + i, "ligne hors subset"}, Dropped())
				emit.Row("LIGNE", []any{2000 + i, nil, "ligne sans facture"}, Kept())
			}
		},
	}
}

// transformedKeySelfReference: CATEGORIE transforms its primary key, and its parent_id,
// nullable, references another category — read before it or after it, on another page. A
// single pass cannot know the new key of a parent read later: the reference is written
// empty, then filled in once every category has published its new key.
func transformedKeySelfReference() *Case {
	const table = "CATEGORIE"
	return &Case{
		ID:       "tr-key-self-reference",
		Priority: P1,
		Title:    "Clé transformée et auto-référence nullable : la FK suit la nouvelle clé d'un parent lu avant ou après",
		Tables: []*schema.Table{{
			Name: table,
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: referenceColumn, Type: schema.Varchar(20)},
				{Name: "parent_id", Type: schema.Int64(), Nullable: true},
			},
			PrimaryKey:  []string{idColumn},
			Indexes:     []schema.Index{{Name: "uq_categorie_reference", Columns: []string{referenceColumn}, Unique: true}},
			ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_categorie_parent", "parent_id", table)},
		}},
		Identity: map[string][]string{table: {referenceColumn}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{table: {
			idColumn: {
				Transformer: transformJavascript("return value + 1000000;"),
				Rules:       []Rule{RuleNotInSourceSet, RuleUnique},
			},
			"parent_id": {Rules: []Rule{RuleFollowsParent}},
		}}},
		Seed: func(p Params, emit Emitter) {
			rows := int64(2*p.PageLimit + p.PageLimit/2)
			for i := int64(1); i <= rows; i++ {
				var parent any
				if i%5 != 0 {
					parent = (i*7)%rows + 1 // before or after, on this page or another
				}
				emit.Row(table, []any{i, fmt.Sprintf("CAT-%05d", i), parent}, Kept())
			}
		},
	}
}

// transformedKeysInCycle: CLIENT and COMMANDE reference each other through nullable keys,
// and both transform their primary key. Neither table can wait for the other: each
// reference is written empty, then filled in once both tables have published their keys.
func transformedKeysInCycle() *Case {
	transformed := ColumnSpec{
		Transformer: transformJavascript("return value + 1000000;"),
		Rules:       []Rule{RuleNotInSourceSet, RuleUnique},
	}
	follows := ColumnSpec{Rules: []Rule{RuleFollowsParent}}
	return &Case{
		ID:       "tr-keys-in-cycle",
		Priority: P1,
		Title:    "Clés transformées dans un cycle de FK nullables : chaque FK suit la nouvelle clé de l'autre table",
		Tables: []*schema.Table{
			{
				Name: clientTableName,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: referenceColumn, Type: schema.Varchar(20)},
					{Name: "commande_preferee_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey:  []string{idColumn},
				Indexes:     []schema.Index{{Name: "uq_client_reference", Columns: []string{referenceColumn}, Unique: true}},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_client_commande", "commande_preferee_id", commandeTable)},
			},
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: referenceColumn, Type: schema.Varchar(20)},
					{Name: "client_id", Type: schema.Int64(), Nullable: true},
				},
				PrimaryKey:  []string{idColumn},
				Indexes:     []schema.Index{{Name: "uq_commande_reference", Columns: []string{referenceColumn}, Unique: true}},
				ForeignKeys: []schema.ForeignKey{foreignKeyToID("fk_commande_client", "client_id", clientTableName)},
			},
		},
		Identity: map[string][]string{clientTableName: {referenceColumn}, commandeTable: {referenceColumn}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{
			clientTableName: {idColumn: transformed, "commande_preferee_id": follows},
			commandeTable:   {idColumn: transformed, "client_id": follows},
		}},
		Seed: func(p Params, emit Emitter) {
			clients, orders := int64(p.PageLimit+30), int64(2*p.PageLimit+40)
			for i := int64(1); i <= clients; i++ {
				var preferee any
				if i%4 != 0 {
					preferee = (i*3)%orders + 1
				}
				emit.Row(clientTableName, []any{i, fmt.Sprintf("CL-%05d", i), preferee}, Kept())
			}
			for j := int64(1); j <= orders; j++ {
				var client any
				if j%6 != 0 {
					client = (j*5)%clients + 1
				}
				emit.Row(commandeTable, []any{j, fmt.Sprintf("CMD-%05d", j), client}, Kept())
			}
		},
	}
}
