//go:build integration

package runner

// Test d'intégration bout-en-bout du moteur Athanor contre deux vraies bases
// MySQL (voir testdata/compose.yml). Il exécute runner.RunTable de la source vers
// la cible et vérifie que les données sont anonymisées.
//
// Lancer :
//   docker compose -f worker/pkg/athanor/runner/testdata/compose.yml up -d
//   go test -tags integration ./worker/pkg/athanor/runner/
//   docker compose -f worker/pkg/athanor/runner/testdata/compose.yml down -v

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	mgmtv1alpha1 "github.com/Groupe-Hevea/neosync/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/sqlio"
)

const (
	srcDSN = "root:root@tcp(127.0.0.1:3307)/appdb"
	dstDSN = "root:root@tcp(127.0.0.1:3308)/appdb"
)

func openWithRetry(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", dsn, err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		if err = db.Ping(); err == nil {
			return db
		}
		if time.Now().After(deadline) {
			t.Fatalf("MySQL %s injoignable: %v (compose up ?)", dsn, err)
		}
		time.Sleep(2 * time.Second)
	}
}

func mappings() []*mgmtv1alpha1.JobMapping {
	return []*mgmtv1alpha1.JobMapping{
		{Schema: "appdb", Table: "clients", Column: "id", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}}}}},
		{Schema: "appdb", Table: "clients", Column: "prenom", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateFirstNameConfig{GenerateFirstNameConfig: &mgmtv1alpha1.GenerateFirstName{}}}}},
		{Schema: "appdb", Table: "clients", Column: "nom", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_GenerateLastNameConfig{GenerateLastNameConfig: &mgmtv1alpha1.GenerateLastName{}}}}},
		{Schema: "appdb", Table: "clients", Column: "email", Transformer: &mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{Config: &mgmtv1alpha1.TransformerConfig_TransformEmailConfig{TransformEmailConfig: &mgmtv1alpha1.TransformEmail{}}}}},
	}
}

func TestIntegration_MySQL_RunTable(t *testing.T) {
	ctx := context.Background()

	src := openWithRetry(t, srcDSN)
	defer src.Close()
	dst := openWithRetry(t, dstDSN)
	defer dst.Close()

	// Idempotence : on vide la cible pour permettre les ré-exécutions.
	if _, err := dst.ExecContext(ctx, "DELETE FROM clients"); err != nil {
		t.Fatalf("nettoyage cible: %v", err)
	}

	// Originaux, pour vérifier l'anonymisation.
	type row struct{ prenom, nom, email string }
	orig := map[int64]row{}
	rs, err := src.QueryContext(ctx, "SELECT id, prenom, nom, email FROM clients")
	if err != nil {
		t.Fatalf("lecture source: %v", err)
	}
	for rs.Next() {
		var id int64
		var r row
		if err := rs.Scan(&id, &r.prenom, &r.nom, &r.email); err != nil {
			t.Fatalf("scan source: %v", err)
		}
		orig[id] = r
	}
	rs.Close()
	if len(orig) != 5 {
		t.Fatalf("attendu 5 lignes source, obtenu %d", len(orig))
	}

	// EXÉCUTION DU MOTEUR ATHANOR, source -> cible, batch de 2.
	if err := RunTable(ctx, src, dst, sqlio.MySQLDialect{}, mappings(), "appdb", "clients", "", 2); err != nil {
		t.Fatalf("RunTable: %v", err)
	}

	// Vérification de la cible.
	rs2, err := dst.QueryContext(ctx, "SELECT id, prenom, nom, email FROM clients ORDER BY id")
	if err != nil {
		t.Fatalf("lecture cible: %v", err)
	}
	defer rs2.Close()

	n := 0
	for rs2.Next() {
		var id int64
		var r row
		if err := rs2.Scan(&id, &r.prenom, &r.nom, &r.email); err != nil {
			t.Fatalf("scan cible: %v", err)
		}
		n++
		o := orig[id]

		// id préservé (Passthrough).
		if _, ok := orig[id]; !ok {
			t.Fatalf("id %d absent de la source", id)
		}
		// email anonymisé : différent de l'original, et toujours une adresse.
		if r.email == o.email {
			t.Errorf("id %d : email non anonymisé (%q)", id, r.email)
		}
		if !containsAt(r.email) {
			t.Errorf("id %d : email cible mal formé (%q)", id, r.email)
		}
		// prénom/nom générés (non vides).
		if r.prenom == "" || r.nom == "" {
			t.Errorf("id %d : prénom/nom vides", id)
		}
		t.Logf("id %d : %s %s <%s>  ->  %s %s <%s>", id, o.prenom, o.nom, o.email, r.prenom, r.nom, r.email)
	}
	if n != 5 {
		t.Fatalf("attendu 5 lignes cible, obtenu %d", n)
	}
}

func containsAt(s string) bool {
	for i := range s {
		if s[i] == '@' {
			return true
		}
	}
	return false
}
