package sqlio

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/engine"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/native"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

// fakeReader reproduit la sémantique de *database/sql.Rows : Next() avance le
// curseur, Scan() lit la ligne courante. Permet de tester sans base réelle.
type fakeReader struct {
	cols []string
	data [][]any
	pos  int
	cur  []any
}

func (r *fakeReader) Columns() ([]string, error) { return r.cols, nil }
func (r *fakeReader) Next() bool {
	if r.pos >= len(r.data) {
		return false
	}
	r.cur = r.data[r.pos]
	r.pos++
	return true
}
func (r *fakeReader) Scan(dest ...any) error {
	for i := range dest {
		*(dest[i].(*any)) = r.cur[i]
	}
	return nil
}
func (r *fakeReader) Err() error   { return nil }
func (r *fakeReader) Close() error { return nil }

// captureWriter accumule les lignes reçues.
type captureWriter struct {
	cols []string
	rows [][]any
}

func (w *captureWriter) WriteBatch(cols []string, rows [][]any) error {
	w.cols = cols
	w.rows = append(w.rows, rows...)
	return nil
}

func TestPipeline_EndToEnd(t *testing.T) {
	reader := &fakeReader{
		cols: []string{"id", "email"},
		data: [][]any{
			{int64(1), "a@x.fr"},
			{int64(2), "b@x.fr"},
			{int64(3), "a@x.fr"}, // même email source que la ligne 1
			{int64(4), "c@x.fr"},
			{int64(5), "b@x.fr"}, // même email source que la ligne 2
		},
	}

	dom := consistency.New([]byte("clé-test"), "org").Domain("person.email")
	spec := engine.Spec{
		Values: []engine.ValueBinding{
			{Column: "email", T: native.NewDictFaker(dom, []string{"u1@anon.test", "u2@anon.test", "u3@anon.test", "u4@anon.test"})},
		},
	}

	w := &captureWriter{}
	// batchSize = 2 pour forcer plusieurs batches (5 lignes -> 3 batches).
	if err := Pipeline(transform.Background(), reader, 2, spec, w); err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	if len(w.rows) != 5 {
		t.Fatalf("attendu 5 lignes écrites, obtenu %d", len(w.rows))
	}

	// Colonne email = index 1. Les ids ne doivent pas bouger (passthrough implicite).
	emailOf := map[int64]string{}
	for _, row := range w.rows {
		id := row[0].(int64)
		email := row[1].(string)
		emailOf[id] = email
		if email == "a@x.fr" || email == "b@x.fr" || email == "c@x.fr" {
			t.Fatalf("email non anonymisé pour id %d: %v", id, email)
		}
	}

	// Cohérence à travers les batches : même email source -> même sortie, même
	// si les lignes tombent dans des batches différents.
	if emailOf[1] != emailOf[3] {
		t.Fatalf("cohérence inter-batch cassée: id1=%q id3=%q", emailOf[1], emailOf[3])
	}
	if emailOf[2] != emailOf[5] {
		t.Fatalf("cohérence inter-batch cassée: id2=%q id5=%q", emailOf[2], emailOf[5])
	}
	// NB : on n'exige PAS que des emails source différents donnent des sorties
	// différentes. Un faker par dictionnaire (4 entrées ici) peut légitimement
	// faire collisionner deux entrées ; l'unicité garantie relèvera du mapping
	// store (RFC §8.3), pas du déterminisme cryptographique.
}

// Scénario réel : deux sources représentant le même contenu logique, l'une en
// string (façon PostgreSQL), l'autre en []byte (façon MySQL). Après passage dans
// le Pipeline (donc après normalisation), l'anonymisation doit être IDENTIQUE.
func TestPipeline_CrossDriverConsistency(t *testing.T) {
	dom := consistency.New([]byte("clé-test"), "org").Domain("person.email")
	dict := []string{"u1@anon.test", "u2@anon.test", "u3@anon.test", "u4@anon.test"}
	makeSpec := func() engine.Spec {
		return engine.Spec{Values: []engine.ValueBinding{
			{Column: "email", T: native.NewDictFaker(dom, dict)},
		}}
	}

	pgReader := &fakeReader{cols: []string{"email"}, data: [][]any{
		{"alice@x.fr"}, {"bob@x.fr"},
	}}
	myReader := &fakeReader{cols: []string{"email"}, data: [][]any{
		{[]byte("alice@x.fr")}, {[]byte("bob@x.fr")}, // même contenu, type driver différent
	}}

	pgOut, myOut := &captureWriter{}, &captureWriter{}
	if err := Pipeline(transform.Background(), pgReader, 10, makeSpec(), pgOut); err != nil {
		t.Fatalf("pipeline pg: %v", err)
	}
	if err := Pipeline(transform.Background(), myReader, 10, makeSpec(), myOut); err != nil {
		t.Fatalf("pipeline my: %v", err)
	}

	for i := range pgOut.rows {
		if pgOut.rows[i][0] != myOut.rows[i][0] {
			t.Fatalf("ligne %d : anonymisation divergente pg=%v my=%v", i, pgOut.rows[i][0], myOut.rows[i][0])
		}
	}
}

func TestPipeline_CompileErrorPropagates(t *testing.T) {
	reader := &fakeReader{cols: []string{"id"}, data: [][]any{{int64(1)}}}
	spec := engine.Spec{Values: []engine.ValueBinding{{Column: "colonne_absente", T: nil}}}
	if err := Pipeline(transform.Background(), reader, 10, spec, &captureWriter{}); err == nil {
		t.Fatal("une colonne absente doit faire échouer le pipeline à la compilation")
	}
}

func TestPipeline_BadBatchSize(t *testing.T) {
	reader := &fakeReader{cols: []string{"id"}, data: [][]any{{int64(1)}}}
	if err := Pipeline(transform.Background(), reader, 0, engine.Spec{}, &captureWriter{}); err == nil {
		t.Fatal("batchSize=0 doit être rejeté")
	}
}
