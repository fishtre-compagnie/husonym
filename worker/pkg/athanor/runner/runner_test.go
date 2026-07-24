package runner

import (
	"testing"

	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/engine"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/sqlio"
)

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

	cols, spec, err := SpecForTable(mappings, "public", "clients")
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

func TestSpecForTable_NoMapping(t *testing.T) {
	_, _, err := SpecForTable(nil, "public", "vide")
	if err == nil {
		t.Fatal("aucun mapping pour la table doit être une erreur")
	}
}

func TestBuildSelect(t *testing.T) {
	got := buildSelect(sqlio.PostgresDialect{}, "public", "clients", []string{"id", "age"})
	want := `SELECT "id", "age" FROM "public"."clients"`
	if got != want {
		t.Fatalf("buildSelect:\n  obtenu %q\n  voulu  %q", got, want)
	}

	got = buildSelect(sqlio.MySQLDialect{}, "", "clients", []string{"id"})
	want = "SELECT `id` FROM `clients`"
	if got != want {
		t.Fatalf("buildSelect mysql:\n  obtenu %q\n  voulu  %q", got, want)
	}
}
