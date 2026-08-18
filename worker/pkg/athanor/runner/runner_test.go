package runner

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/native"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

func generateFirstName() *mgmtv1alpha1.JobMappingTransformer {
	return &mgmtv1alpha1.JobMappingTransformer{
		Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{
				GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{},
			},
		},
	}
}

func passthrough() *mgmtv1alpha1.JobMappingTransformer {
	return &mgmtv1alpha1.JobMappingTransformer{
		Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
				PassthroughConfig: &mgmtv1alpha1.Passthrough{},
			},
		},
	}
}

func transformInt64() *mgmtv1alpha1.JobMappingTransformer {
	min, max := int64(1), int64(100)
	return &mgmtv1alpha1.JobMappingTransformer{
		Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformInt64Config{
				TransformInt64Config: &mgmtv1alpha1.TransformInt64{
					RandomizationRangeMin: &min,
					RandomizationRangeMax: &max,
				},
			},
		},
	}
}

func TestSpecForTable(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "public", Table: "clients", Column: "id", Transformer: passthrough()},
		{Schema: "public", Table: "clients", Column: "age", Transformer: transformInt64()},
		{Schema: "public", Table: "autre", Column: "x", Transformer: transformInt64()}, // autre table : ignorée
	}

	cols, spec, err := SpecForTable(mappings, "public", "clients", nil)
	if err != nil {
		t.Fatalf("SpecForTable: %v", err)
	}

	// Les deux colonnes de la table figurent dans le schéma...
	if len(cols) != 2 || cols[0] != "id" || cols[1] != "age" {
		t.Fatalf("colonnes inattendues: %v", cols)
	}
	// ...mais seule la colonne non-passthrough reçoit un binding.
	if len(spec.Values) != 1 || spec.Values[0].Column != "age" {
		t.Fatalf("bindings inattendus: %+v", spec.Values)
	}

	// Le plan produit doit compiler contre le schéma.
	if _, err := engine.Compile(cols, spec); err != nil {
		t.Fatalf("le plan issu des mappings doit compiler: %v", err)
	}
}

// Avec un deriver, un GenerateFirstName est routé vers un DictFaker DÉTERMINISTE :
// la même entrée donne toujours la même sortie (RFC §8), et la valeur est bien
// anonymisée.
func TestSpecForTable_Deterministic(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "public", Table: "clients", Column: "prenom", Transformer: generateFirstName()},
	}
	d := consistency.New([]byte("clé-test"), "org")
	_, spec, err := SpecForTable(mappings, "public", "clients", d)
	if err != nil {
		t.Fatalf("SpecForTable: %v", err)
	}
	if _, ok := spec.Values[0].T.(*native.DictFaker); !ok {
		t.Fatalf("attendu un DictFaker déterministe, obtenu %T", spec.Values[0].T)
	}
	ctx := transform.Background()
	a, _ := spec.Values[0].T.TransformValue(ctx, "Jean")
	b, _ := spec.Values[0].T.TransformValue(ctx, "Jean")
	if a != b {
		t.Fatalf("même entrée -> même sortie attendu, obtenu %v puis %v", a, b)
	}
	if a == "Jean" || a == "" {
		t.Fatalf("la valeur doit être anonymisée, obtenu %v", a)
	}
}

// Sans deriver, on retombe sur l'adaptateur Benthos (aléatoire) — pas un DictFaker.
func TestSpecForTable_NoDeriverFallsBack(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "public", Table: "clients", Column: "prenom", Transformer: generateFirstName()},
	}
	_, spec, err := SpecForTable(mappings, "public", "clients", nil)
	if err != nil {
		t.Fatalf("SpecForTable: %v", err)
	}
	if _, ok := spec.Values[0].T.(*native.DictFaker); ok {
		t.Fatal("sans deriver, ne doit pas être un DictFaker")
	}
}

func TestSpecForTable_NoMapping(t *testing.T) {
	_, _, err := SpecForTable(nil, "public", "vide", nil)
	if err == nil {
		t.Fatal("aucun mapping pour la table doit être une erreur")
	}
}

func TestBuildSelect(t *testing.T) {
	got := buildSelect(sqlio.PostgresDialect{}, "public", "clients", []string{"id", "age"}, "")
	want := `SELECT "id", "age" FROM "public"."clients"`
	if got != want {
		t.Fatalf("buildSelect:\n  obtenu %q\n  voulu  %q", got, want)
	}

	got = buildSelect(sqlio.MySQLDialect{}, "", "clients", []string{"id"}, "")
	want = "SELECT `id` FROM `clients`"
	if got != want {
		t.Fatalf("buildSelect mysql:\n  obtenu %q\n  voulu  %q", got, want)
	}

	// Avec subsetting : la clause WHERE est ajoutée telle quelle.
	got = buildSelect(sqlio.PostgresDialect{}, "public", "clients", []string{"id"}, "id > 100 AND actif = true")
	want = `SELECT "id" FROM "public"."clients" WHERE id > 100 AND actif = true`
	if got != want {
		t.Fatalf("buildSelect where:\n  obtenu %q\n  voulu  %q", got, want)
	}
}

func TestWhereForTable(t *testing.T) {
	where := "age >= 18"
	source := &mgmtv1alpha1.JobSource{
		Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
				Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
					Schemas: []*mgmtv1alpha1.MysqlSourceSchemaOption{
						{
							Schema: "demo",
							Tables: []*mgmtv1alpha1.MysqlSourceTableOption{
								{Table: "clients", WhereClause: &where},
								{Table: "commandes"},
							},
						},
					},
				},
			},
		},
	}

	if got := WhereForTable(source, "demo", "clients"); got != where {
		t.Fatalf("clients: attendu %q, obtenu %q", where, got)
	}
	if got := WhereForTable(source, "demo", "commandes"); got != "" {
		t.Fatalf("commandes: attendu \"\", obtenu %q", got)
	}
	if got := WhereForTable(source, "demo", "inconnue"); got != "" {
		t.Fatalf("table inconnue: attendu \"\", obtenu %q", got)
	}
	if got := WhereForTable(nil, "demo", "clients"); got != "" {
		t.Fatalf("source nil: attendu \"\", obtenu %q", got)
	}
}
