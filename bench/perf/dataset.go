// Package perf holds the measured comparison of the two engines: one schema at scale, run
// by each engine in turn, with the time, the throughput and the memory each of them takes.
//
// The correctness bench answers "does the engine do the right thing"; it never answers
// "how fast", since its cases hold a few dozen rows and its jobs run side by side. Here a
// single job is run alone, on a worker restarted before each run, and what is measured is
// the work itself.
package perf

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// Tables of the dataset. They cover, at scale, what a real job spends its time on: a table
// copied whole, a subset propagated over two levels of mandatory foreign keys, a nullable
// foreign key filtered off the subset path, a table without a key read in one stream, wide
// rows, and a transformed key the tables referencing it must follow.
const (
	// idColumn is the key every table but the journal is identified by.
	idColumn = "id"

	ReferentielTable = "REFERENTIEL"
	ClientTable      = "CLIENT"
	CommandeTable    = "COMMANDE"
	LigneTable       = "LIGNE_COMMANDE"
	AdresseTable     = "ADRESSE"
	JournalTable     = "JOURNAL"
	DocumentTable    = "DOCUMENT"
	PatientTable     = "PATIENT"
	VisiteTable      = "VISITE"
)

// rowsPerUnit is how many rows each table holds at scale 1. The scale multiplies them,
// and its default of 100 gives the dataset of the comparison, about three million rows: a
// small scale keeps the same shape for a quick pass.
var rowsPerUnit = map[string]int{
	ReferentielTable: 500,
	ClientTable:      2_000,
	CommandeTable:    10_000,
	LigneTable:       10_000,
	AdresseTable:     2_000,
	JournalTable:     3_000,
	DocumentTable:    500,
	PatientTable:     1_000,
	VisiteTable:      2_000,
}

// DefaultScale gives the dataset the comparison is made on.
const DefaultScale = 100

// Rows returns how many rows a table holds at the given scale.
func Rows(table string, scale int) int {
	return rowsPerUnit[table] * max(scale, 1)
}

// TotalRows returns how many rows the whole dataset holds at the given scale.
func TotalRows(scale int) int {
	total := 0
	for table := range rowsPerUnit {
		total += Rows(table, scale)
	}
	return total
}

// keptRegions are the regions the subset keeps: half of the clients, so that the subset
// really filters instead of copying everything.
const (
	regions     = 4
	keptRegions = 2
)

// Dataset returns the job the two engines run, as a bench case: the same type carries the
// tables, the mappings and the options, so the perf mode creates its job exactly the way
// the correctness cases do. Its rows are not loaded from the seed, which no oracle
// records at this scale, but by Load.
func Dataset() *cases.Case {
	return &cases.Case{
		ID:       "perf",
		Priority: cases.P3,
		Title:    "Comparaison mesurée des moteurs, schéma combiné à l'échelle",
		Tables: []*schema.Table{
			referentiel(),
			client(),
			commande(),
			ligne(),
			adresse(),
			journal(),
			document(),
			patient(),
			visite(),
		},
		Job: cases.Job{
			Where:                    map[string]string{ClientTable: fmt.Sprintf("region < %d", keptRegions)},
			SubsetByForeignKeys:      true,
			SkipForeignKeyViolations: true,
			TruncateBeforeInsert:     true,
			Columns:                  transformers(),
		},
		Identity: map[string][]string{
			PatientTable: {"dossier"},
			JournalTable: {idColumn},
		},
		Seed: func(cases.Params, cases.Emitter) {},
	}
}

// transformers anonymize what a real job anonymizes: the personal columns of the clients
// and of the patients, whose key the visits must follow, and a rule of the user's own on
// the largest table, which measures what a JavaScript transformer costs a run.
func transformers() map[string]map[string]cases.ColumnSpec {
	return map[string]map[string]cases.ColumnSpec{
		ClientTable: {
			"nom":       {Transformer: generateLastName()},
			"email":     {Transformer: generateEmail()},
			"telephone": {Transformer: generatePhone()},
		},
		CommandeTable: {
			"reference": {Transformer: transformJavascript("return value.toUpperCase();")},
		},
		PatientTable: {
			// The key the visits follow, published through redis.
			idColumn: {Transformer: transformJavascript("return value + 1000000;")},
			"nom":    {Transformer: generateLastName()},
		},
	}
}

func generateLastName() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateLastNameConfig{
		GenerateLastNameConfig: &mgmtv1alpha1.GenerateLastName{},
	}}
}

func generateEmail() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{
		GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{},
	}}
}

func generatePhone() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateStringPhoneNumberConfig{
		GenerateStringPhoneNumberConfig: &mgmtv1alpha1.GenerateStringPhoneNumber{},
	}}
}

func transformJavascript(code string) *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
		TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: code},
	}}
}

func referentiel() *schema.Table {
	return &schema.Table{
		Name: ReferentielTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "code", Type: schema.Varchar(32)},
			{Name: "libelle", Type: schema.Varchar(120)},
			{Name: "actif", Type: schema.Bool()},
		},
		PrimaryKey: []string{idColumn},
	}
}

func client() *schema.Table {
	return &schema.Table{
		Name: ClientTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "region", Type: schema.Int32()},
			{Name: "nom", Type: schema.Varchar(80)},
			{Name: "email", Type: schema.Varchar(120)},
			{Name: "telephone", Type: schema.Varchar(30)},
			{Name: "cree_le", Type: schema.DateTime(6)},
		},
		PrimaryKey: []string{idColumn},
		Indexes:    []schema.Index{{Name: "idx_client_region", Columns: []string{"region"}}},
	}
}

func commande() *schema.Table {
	return &schema.Table{
		Name: CommandeTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "client_id", Type: schema.Int64()},
			{Name: "reference", Type: schema.Varchar(40)},
			{Name: "montant", Type: schema.Decimal(12, 2)},
			{Name: "passee_le", Type: schema.DateTime(0)},
		},
		PrimaryKey: []string{idColumn},
		ForeignKeys: []schema.ForeignKey{
			{Name: "fk_commande_client", Columns: []string{"client_id"}, RefTable: ClientTable, RefColumns: []string{idColumn}},
		},
	}
}

func ligne() *schema.Table {
	return &schema.Table{
		Name: LigneTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "commande_id", Type: schema.Int64()},
			{Name: "referentiel_id", Type: schema.Int64()},
			{Name: "quantite", Type: schema.Int32()}, //nolint:misspell // colonne nommée en français
			{Name: "prix", Type: schema.Decimal(10, 2)},
		},
		PrimaryKey: []string{idColumn},
		ForeignKeys: []schema.ForeignKey{
			{Name: "fk_ligne_commande", Columns: []string{"commande_id"}, RefTable: CommandeTable, RefColumns: []string{idColumn}},
			{Name: "fk_ligne_referentiel", Columns: []string{"referentiel_id"}, RefTable: ReferentielTable, RefColumns: []string{idColumn}},
		},
	}
}

// adresse holds a nullable foreign key off the subset path: the engines read it through
// the EXISTS filter that clears what the subset leaves out.
func adresse() *schema.Table {
	return &schema.Table{
		Name: AdresseTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "client_id", Type: schema.Int64()},
			{Name: "facture_client_id", Type: schema.Int64(), Nullable: true},
			{Name: "ligne1", Type: schema.Varchar(120)},
			{Name: "ville", Type: schema.Varchar(80)},
		},
		PrimaryKey: []string{idColumn},
		ForeignKeys: []schema.ForeignKey{
			{Name: "fk_adresse_client", Columns: []string{"client_id"}, RefTable: ClientTable, RefColumns: []string{idColumn}},
			{Name: "fk_adresse_facture", Columns: []string{"facture_client_id"}, RefTable: ClientTable, RefColumns: []string{idColumn}},
		},
	}
}

// journal has no key at all: it is read in one stream, never paged.
func journal() *schema.Table {
	return &schema.Table{
		Name: JournalTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "horodatage", Type: schema.DateTime(6)},
			{Name: "message", Type: schema.Varchar(200)},
		},
	}
}

// document carries the payload: wide rows with text, json, decimals and bytes.
func document() *schema.Table {
	columns := []schema.Column{
		{Name: idColumn, Type: schema.Int64()},
		{Name: "client_id", Type: schema.Int64()},
		{Name: "corps", Type: schema.Text()},
		{Name: "meta", Type: schema.JSON()},
		{Name: "empreinte", Type: schema.Binary(16)},
		{Name: "montant", Type: schema.Decimal(18, 6)},
		{Name: "recu_le", Type: schema.DateTime(6)},
	}
	for i := 1; i <= 20; i++ {
		columns = append(columns, schema.Column{Name: fmt.Sprintf("champ_%02d", i), Type: schema.Varchar(60), Nullable: true})
	}
	return &schema.Table{
		Name:       DocumentTable,
		Columns:    columns,
		PrimaryKey: []string{idColumn},
		ForeignKeys: []schema.ForeignKey{
			{Name: "fk_document_client", Columns: []string{"client_id"}, RefTable: ClientTable, RefColumns: []string{idColumn}},
		},
	}
}

// patient holds a transformed primary key, published to redis for the visits to follow.
func patient() *schema.Table {
	return &schema.Table{
		Name: PatientTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "dossier", Type: schema.Varchar(32)},
			{Name: "client_id", Type: schema.Int64()},
			{Name: "nom", Type: schema.Varchar(80)},
		},
		PrimaryKey: []string{idColumn},
		Indexes:    []schema.Index{{Name: "uq_patient_dossier", Columns: []string{"dossier"}, Unique: true}},
		ForeignKeys: []schema.ForeignKey{
			{Name: "fk_patient_client", Columns: []string{"client_id"}, RefTable: ClientTable, RefColumns: []string{idColumn}},
		},
	}
}

func visite() *schema.Table {
	return &schema.Table{
		Name: VisiteTable,
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "patient_id", Type: schema.Int64()},
			{Name: "faite_le", Type: schema.DateTime(0)},
			{Name: "motif", Type: schema.Varchar(120)},
		},
		PrimaryKey: []string{idColumn},
		ForeignKeys: []schema.ForeignKey{
			{Name: "fk_visite_patient", Columns: []string{"patient_id"}, RefTable: PatientTable, RefColumns: []string{idColumn}},
		},
	}
}
