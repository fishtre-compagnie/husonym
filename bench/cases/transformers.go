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
			"code", generateJavascript(`return "constante";`), "Duplicate entry"),
		transformerFailure("tr-output-longer-than-column",
			"Sortie plus longue que la colonne : échec explicite, jamais de troncature silencieuse",
			"code", generateJavascript(`return "sortie nettement plus longue que dix caractères";`), "Data too long"),
		transformerFailure("tr-null-on-not-null-column",
			"Transformer Null sur une colonne NOT NULL : échec explicite, jamais de valeur par défaut implicite",
			"libelle", nullTransformer(), "cannot be null"),
		transformerOnOrderColumn(),
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
			Name: "CLIENT",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "email", Type: schema.Varchar(120)},
				{Name: "prenom", Type: schema.Varchar(60)},
			},
			PrimaryKey: []string{idColumn},
			Indexes:    []schema.Index{{Name: "uq_client_email", Columns: []string{"email"}, Unique: true}},
		}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{"CLIENT": {
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
				emit.Row("CLIENT", []any{i, email, fmt.Sprintf("Prénom-Source-%d", i)}, Kept())
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
			Name: "CLIENT",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "commentaire", Type: schema.Varchar(60), Nullable: true},
			},
			PrimaryKey: []string{idColumn},
		}},
		Job: Job{Columns: map[string]map[string]ColumnSpec{"CLIENT": {
			"commentaire": {Transformer: nullTransformer(), Rules: []Rule{RuleNull}},
		}}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row("CLIENT", []any{i, fmt.Sprintf("remarque personnelle %d", i)}, Kept())
			}
		},
	}
}

// transformerFailure: a transformer whose output the destination must refuse. The only
// correct outcome is a failed run naming the error: a completed run means rows were
// dropped, truncated or defaulted without a word.
func transformerFailure(id, title, column string, transformer *mgmtv1alpha1.TransformerConfig, runError string) *Case {
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
				{Name: "reference", Type: schema.Varchar(20)},
			},
			PrimaryKey: []string{idColumn},
			Indexes:    []schema.Index{{Name: "uq_dossier_reference", Columns: []string{"reference"}, Unique: true}},
		}},
		Identity: map[string][]string{"DOSSIER": {"reference"}},
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
