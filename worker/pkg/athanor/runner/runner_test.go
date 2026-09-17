package runner

import (
	"reflect"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
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

// Null doit produire un vrai NULL SQL (pas la chaîne "null"), et Default doit
// sortir la colonne de l'INSERT pour laisser la destination appliquer son défaut.
func TestSpecForTable_NullAndDefault(t *testing.T) {
	mappings := []*mgmtv1alpha1.JobMapping{
		{Schema: "public", Table: "clients", Column: "id", Transformer: passthrough()},
		{Schema: "public", Table: "clients", Column: "note", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{Nullconfig: &mgmtv1alpha1.Null{}},
			},
		}},
		{Schema: "public", Table: "clients", Column: "cree_le", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{}},
			},
		}},
	}

	cols, spec, err := SpecForTable(mappings, "public", "clients", nil)
	if err != nil {
		t.Fatalf("SpecForTable: %v", err)
	}
	if len(cols) != 2 || cols[0] != "id" || cols[1] != "note" {
		t.Fatalf("la colonne Default ne doit être ni lue ni écrite, colonnes: %v", cols)
	}
	if len(spec.Values) != 1 || spec.Values[0].Column != "note" {
		t.Fatalf("bindings inattendus: %+v", spec.Values)
	}
	out, err := spec.Values[0].T.TransformValue(transform.Background(), "texte")
	if err != nil || out != nil {
		t.Fatalf("Null doit renvoyer nil, obtenu %#v (err %v)", out, err)
	}
}

func TestSpecForTable_NoMapping(t *testing.T) {
	_, _, err := SpecForTable(nil, "public", "vide", nil)
	if err == nil {
		t.Fatal("aucun mapping pour la table doit être une erreur")
	}
}

func TestPageQuery(t *testing.T) {
	plan := &tableplan.TablePlan{
		Schema:         "web",
		Table:          "users",
		Query:          "SELECT * FROM users LIMIT 100",
		PageQuery:      "SELECT * FROM users WHERE (a > ?) OR (a = ? AND b > ?) LIMIT ?",
		PageLimit:      100,
		OrderByColumns: []string{"a", "b"},
	}

	query, args, err := pageQuery(plan, sqlio.MySQLDialect{}, nil)
	if err != nil || query != plan.Query || args != nil {
		t.Fatalf("première page : %q %v (err %v)", query, args, err)
	}

	query, args, err = pageQuery(plan, sqlio.MySQLDialect{}, []any{int64(7), "x"})
	if err != nil || query != plan.PageQuery {
		t.Fatalf("page suivante : %q (err %v)", query, err)
	}
	if want := []any{int64(7), int64(7), "x", 100}; !reflect.DeepEqual(args, want) {
		t.Fatalf("arguments MySQL : %v, attendu %v", args, want)
	}

	// SQL Server : TOP en tête de requête, donc la taille de page d'abord.
	_, args, _ = pageQuery(plan, sqlio.MSSQLDialect{}, []any{int64(7), "x"})
	if want := []any{100, int64(7), int64(7), "x"}; !reflect.DeepEqual(args, want) {
		t.Fatalf("arguments SQL Server : %v, attendu %v", args, want)
	}

	if _, _, err := pageQuery(plan, sqlio.MySQLDialect{}, []any{int64(7)}); err == nil {
		t.Fatal("un nombre de valeurs de reprise différent des colonnes de tri doit être refusé")
	}
	if _, _, err := pageQuery(&tableplan.TablePlan{Query: "SELECT 1"}, sqlio.MySQLDialect{}, []any{1}); err == nil {
		t.Fatal("une reprise sur un plan non paginé doit être refusée")
	}
}

// Deux emails qui ne diffèrent que par la casse sont deux lignes distinctes en
// source : ils doivent rester distincts pour ne pas violer une contrainte unique.
func TestDeterministicEmail_PreservesCaseDistinction(t *testing.T) {
	cfg := &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateEmailConfig{GenerateEmailConfig: &mgmtv1alpha1.GenerateEmail{}},
	}
	vt, ok := deterministicValueTransformer(consistency.New([]byte("clé-test"), "org"), cfg)
	if !ok {
		t.Fatal("GenerateEmail doit passer par le chemin déterministe")
	}
	ctx := transform.Background()
	a, _ := vt.TransformValue(ctx, "Bob@x.com")
	b, _ := vt.TransformValue(ctx, "bob@x.com")
	if a == b {
		t.Fatalf("emails de casse différente fusionnés en %v", a)
	}
}
