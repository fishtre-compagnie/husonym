package sqlio

import (
	"testing"
	"time"

	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/consistency"
)

func TestNormalize_Auto(t *testing.T) {
	n := NewNormalizer()
	cases := []struct {
		in   any
		want any
	}{
		{[]byte("bonjour"), "bonjour"}, // []byte texte -> string
		{"déjà", "déjà"},
		{int(7), int64(7)},
		{int32(7), int64(7)},
		{uint16(7), int64(7)},
		{float32(1.5), float64(1.5)},
		{float64(1.5), float64(1.5)},
		{true, true},
		{nil, nil},
	}
	for _, c := range cases {
		got, err := n.Normalize("col", c.in)
		if err != nil {
			t.Fatalf("Normalize(%T) erreur: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("Normalize(%#v) = %#v (%T), attendu %#v (%T)", c.in, got, got, c.want, c.want)
		}
	}

	// time.Time est conservé (comparaison à part car non comparable par ==).
	now := time.Now()
	got, _ := n.Normalize("col", now)
	if gt, ok := got.(time.Time); !ok || !gt.Equal(now) {
		t.Fatalf("time.Time non conservé: %#v", got)
	}
}

func TestNormalize_Overrides(t *testing.T) {
	n := NewNormalizer().
		With("age", Integer).
		With("blob", Binary).
		With("actif", Bool)

	if got, _ := n.Normalize("age", []byte("42")); got != int64(42) {
		t.Fatalf("Integer: attendu int64(42), obtenu %#v", got)
	}
	raw := []byte{0x00, 0x01}
	if got, _ := n.Normalize("blob", raw); got == nil {
		t.Fatal("Binary doit conserver les octets")
	} else if b, ok := got.([]byte); !ok || len(b) != 2 {
		t.Fatalf("Binary: []byte attendu, obtenu %T", got)
	}
	if got, _ := n.Normalize("actif", []byte("true")); got != true {
		t.Fatalf("Bool: attendu true, obtenu %#v", got)
	}

	// Une valeur non convertible remonte une erreur.
	if _, err := n.Normalize("age", []byte("pas un nombre")); err == nil {
		t.Fatal("Integer sur du non-numérique doit échouer")
	}
}

// LE test qui justifie tout : la même valeur logique, reçue en string (façon
// PostgreSQL) ou en []byte (façon MySQL), doit produire la MÊME graine de
// cohérence après normalisation — donc la même anonymisation dans les deux SGBD.
func TestNormalize_PreservesCrossDBConsistency(t *testing.T) {
	n := NewNormalizer()

	pgValue, _ := n.Normalize("email", "alice@exemple.fr")         // PostgreSQL : string
	myValue, _ := n.Normalize("email", []byte("alice@exemple.fr")) // MySQL : []byte

	if pgValue != myValue {
		t.Fatalf("normalisation incohérente: pg=%#v my=%#v", pgValue, myValue)
	}

	dom := consistency.New([]byte("clé"), "org").Domain("person.email")
	pgSeed := dom.Seed(fmtVal(pgValue))
	mySeed := dom.Seed(fmtVal(myValue))
	if pgSeed != mySeed {
		t.Fatal("graines divergentes malgré normalisation : cohérence inter-bases cassée")
	}
}

func fmtVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
