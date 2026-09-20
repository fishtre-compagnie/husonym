//go:build integration

package runner

// Une clé étrangère obligatoire vers un parent que la destination ne contient pas : la
// ligne est écartée, et le run continue.
//
// Le cas vient de la recette : sur une source vivante, un parent et son enfant créés
// entre la lecture de la table parente et celle de la table enfant font que l'enfant est
// sélectionné sans son parent. Aucun ordre des tables ne l'empêche — la table parente
// était bien terminée avant que l'enfant ne démarre. Le test reproduit cet état en ne
// copiant qu'une partie des commandes, ce qui laisse deux lignes sans parent.
//
// Lancer :
//   docker compose -f worker/pkg/athanor/runner/testdata/compose.yml up -d
//   go test -tags integration -run Parent ./worker/pkg/athanor/runner/

import (
	"context"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/sqlio"
)

func passthroughMappings(table string, columns ...string) []*mgmtv1alpha1.JobMapping {
	out := make([]*mgmtv1alpha1.JobMapping, 0, len(columns))
	for _, column := range columns {
		out = append(out, &mgmtv1alpha1.JobMapping{
			Schema: "appdb", Table: table, Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{
				Config: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
				},
			},
		})
	}
	return out
}

func TestIntegration_MySQL_MandatoryParentMissing_RowIsDiscarded(t *testing.T) {
	ctx := context.Background()

	src := openWithRetry(t, srcDSN)
	defer src.Close()
	dst := openWithRetry(t, dstDSN)
	defer dst.Close()

	for _, statement := range []string{"DELETE FROM lignes", "DELETE FROM commandes"} {
		if _, err := dst.ExecContext(ctx, statement); err != nil {
			t.Fatalf("nettoyage cible (%s): %v", statement, err)
		}
	}

	// Le parent n'est copié qu'en partie : la commande 3 reste en source, comme celle
	// qu'un run n'a pas vue parce qu'elle n'existait pas encore quand il a lu la table.
	parentPlan := &tableplan.TablePlan{
		Id:      "appdb.commandes.insert",
		Schema:  "appdb",
		Table:   "commandes",
		RunType: runconfigs.RunTypeInsert,
		Query:   "SELECT `id`, `reference` FROM `appdb`.`commandes` WHERE `id` <= 2 ORDER BY `id`",
	}
	res, err := RunTablePage(ctx, src, dst, sqlio.MySQLDialect{}, &TablePage{
		Plan:      parentPlan,
		Mappings:  passthroughMappings("commandes", "id", "reference"),
		BatchSize: 10,
		Write:     WriteConfig{DisableForeignKeyChecks: true},
	})
	if err != nil {
		t.Fatalf("copie des commandes: %v", err)
	}
	if res.RowsRead != 2 {
		t.Fatalf("attendu 2 commandes copiées, obtenu %d", res.RowsRead)
	}

	// L'enfant est lu en entier : deux de ses cinq lignes pointent vers la commande 3.
	childPlan := &tableplan.TablePlan{
		Id:      "appdb.lignes.insert",
		Schema:  "appdb",
		Table:   "lignes",
		RunType: runconfigs.RunTypeInsert,
		Query:   "SELECT `id`, `commande_id`, `libelle` FROM `appdb`.`lignes` ORDER BY `id`",
		ForeignKeys: []*tableplan.ForeignKey{{
			Columns:       []string{"commande_id"},
			NotNull:       []bool{true},
			ParentSchema:  "appdb",
			ParentTable:   "commandes",
			ParentColumns: []string{"id"},
			ParentReduced: true,
		}},
	}
	res, err = RunTablePage(ctx, src, dst, sqlio.MySQLDialect{}, &TablePage{
		Plan:      childPlan,
		Mappings:  passthroughMappings("lignes", "id", "commande_id", "libelle"),
		BatchSize: 2,
		Write:     WriteConfig{DisableForeignKeyChecks: true},
	})
	if err != nil {
		t.Fatalf("la page ne doit pas échouer sur un parent absent: %v", err)
	}
	if res.RowsRead != 5 {
		t.Fatalf("attendu 5 lignes lues, obtenu %d", res.RowsRead)
	}
	if res.RowsDiscarded != 2 {
		t.Fatalf("attendu 2 lignes écartées et comptées, obtenu %d", res.RowsDiscarded)
	}

	// La destination tient debout : seules les lignes dont la commande est là sont écrites.
	var written int
	if err := dst.QueryRowContext(ctx, "SELECT count(*) FROM lignes").Scan(&written); err != nil {
		t.Fatalf("comptage cible: %v", err)
	}
	if written != 3 {
		t.Fatalf("attendu 3 lignes écrites, obtenu %d", written)
	}
	var orphans int
	if err := dst.QueryRowContext(ctx, `
		SELECT count(*) FROM lignes AS l
		LEFT JOIN commandes AS c ON c.id = l.commande_id
		WHERE c.id IS NULL`).Scan(&orphans); err != nil {
		t.Fatalf("comptage des orphelines: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("la destination porte %d ligne(s) orpheline(s)", orphans)
	}
}
