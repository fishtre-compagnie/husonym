package engine

import (
	"testing"

	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/consistency"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/native"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/transform"
)

var schema = []string{"id", "sexe", "prenom", "email"}

func newTestBatch() *Batch {
	b := NewBatch(schema, 3)
	// 3 lignes ; les lignes 0 et 2 partagent le même email source.
	b.Cols["id"] = []any{int64(1), int64(2), int64(3)}
	b.Cols["sexe"] = []any{"F", "H", "F"}
	b.Cols["prenom"] = []any{"?", "?", "?"}
	b.Cols["email"] = []any{"a@x.fr", "b@x.fr", "a@x.fr"}
	return b
}

// Bout-en-bout : le moteur (M3) exécute un plan combinant un transformer natif
// déterministe (M1+M2, DictFaker sur email) et un transformer multi-colonnes
// (portée Row, FirstNameBySex).
func TestEngine_EndToEnd(t *testing.T) {
	dom := consistency.New([]byte("clé-test"), "org").Domain("person.email")
	emailFaker := native.NewDictFaker(dom, []string{"u1@anon.test", "u2@anon.test", "u3@anon.test", "u4@anon.test"})

	spec := Spec{
		Values: []ValueBinding{{Column: "email", T: emailFaker}},
		Rows:   []transform.RowTransformer{transform.FirstNameBySex{SexColumn: "sexe", NameColumn: "prenom"}},
	}
	plan, err := Compile(schema, spec)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	b := newTestBatch()
	if err := plan.Execute(transform.Background(), b); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Cohérence : lignes 0 et 2 avaient le même email source → même sortie.
	if b.Cols["email"][0] != b.Cols["email"][2] {
		t.Fatalf("cohérence cassée : %v != %v", b.Cols["email"][0], b.Cols["email"][2])
	}
	// Emails réellement anonymisés (issus du dictionnaire).
	if b.Cols["email"][0] == "a@x.fr" {
		t.Fatal("email non transformé")
	}
	// Prénom cohérent avec le sexe (portée Row).
	if b.Cols["prenom"][0] != "Léa" || b.Cols["prenom"][1] != "Karim" {
		t.Fatalf("row transform inattendu : %v", b.Cols["prenom"])
	}

	// Déterminisme : re-exécuter sur un batch neuf donne le même résultat.
	b2 := newTestBatch()
	_ = plan.Execute(transform.Background(), b2)
	if b.Cols["email"][0] != b2.Cols["email"][0] {
		t.Fatal("exécution non déterministe")
	}
}

func TestCompile_UnknownColumn(t *testing.T) {
	_, err := Compile(schema, Spec{Values: []ValueBinding{{Column: "inexistante", T: nil}}})
	if err == nil {
		t.Fatal("colonne inconnue doit échouer à la compilation")
	}
}

func TestCompile_WriteConflict(t *testing.T) {
	// Un binding valeur et un row transform écrivent tous deux "prenom".
	spec := Spec{
		Values: []ValueBinding{{Column: "prenom", T: nil}},
		Rows:   []transform.RowTransformer{transform.FirstNameBySex{SexColumn: "sexe", NameColumn: "prenom"}},
	}
	if _, err := Compile(schema, spec); err == nil {
		t.Fatal("conflit d'écriture doit échouer à la compilation")
	}
}

// Deux transformers ligne mutuellement dépendants → cycle détecté.
type rwStub struct{ reads, writes []string }

func (t rwStub) Reads() []string                                 { return t.reads }
func (t rwStub) Writes() []string                                { return t.writes }
func (t rwStub) TransformRow(transform.Ctx, transform.Row) error { return nil }

func TestCompile_Cycle(t *testing.T) {
	spec := Spec{Rows: []transform.RowTransformer{
		rwStub{reads: []string{"prenom"}, writes: []string{"email"}}, // A: prenom -> email
		rwStub{reads: []string{"email"}, writes: []string{"prenom"}}, // B: email -> prenom
	}}
	if _, err := Compile(schema, spec); err == nil {
		t.Fatal("cycle de dépendances doit échouer à la compilation")
	}
}

// Le streaming garde le résultat correct sur plusieurs batches.
func TestRun_Streaming(t *testing.T) {
	dom := consistency.New([]byte("clé-test"), "org").Domain("person.email")
	spec := Spec{Values: []ValueBinding{{Column: "email", T: native.NewDictFaker(dom, []string{"x@anon.test"})}}}
	plan, err := Compile(schema, spec)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	remaining := 3
	src := func() (*Batch, bool) {
		if remaining == 0 {
			return nil, false
		}
		remaining--
		return newTestBatch(), true
	}
	count := 0
	err = Run(transform.Background(), src, plan, func(b *Batch) error { count += b.N; return nil })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if count != 9 { // 3 batches x 3 lignes
		t.Fatalf("attendu 9 lignes traitées, obtenu %d", count)
	}
}
