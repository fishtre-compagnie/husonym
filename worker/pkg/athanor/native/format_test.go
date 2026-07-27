package native

import (
	"regexp"
	"testing"

	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/consistency"
	"github.com/Groupe-Hevea/neosync/worker/pkg/athanor/transform"
)

func TestEmailFaker_DeterministicAndValid(t *testing.T) {
	dom := consistency.New([]byte("clé-test"), "org").Domain("person.email")
	f := NewEmailFaker(dom, []string{"anon.test", "example.test"})
	ctx := transform.Background()

	a, _ := f.TransformValue(ctx, "jean.dupont@gmail.com")
	b, _ := f.TransformValue(ctx, "jean.dupont@gmail.com")
	if a != b {
		t.Fatalf("même entrée -> même sortie attendu, obtenu %v puis %v", a, b)
	}
	re := regexp.MustCompile(`^[0-9a-f]{32}@(anon|example)\.test$`)
	if !re.MatchString(a.(string)) {
		t.Fatalf("email invalide: %v", a)
	}
	// entrée différente -> sortie différente
	c, _ := f.TransformValue(ctx, "autre@gmail.com")
	if c == a {
		t.Fatal("deux entrées distinctes ne devraient pas collisionner")
	}
	// NULL conservé
	if v, _ := f.TransformValue(ctx, nil); v != nil {
		t.Fatalf("NULL doit rester NULL, obtenu %v", v)
	}
}

func TestPhoneFaker_DeterministicAndValid(t *testing.T) {
	dom := consistency.New([]byte("clé-test"), "org").Domain("person.phone")
	f := NewPhoneFaker(dom)
	ctx := transform.Background()

	a, _ := f.TransformValue(ctx, "0612345678")
	b, _ := f.TransformValue(ctx, "0612345678")
	if a != b {
		t.Fatalf("même entrée -> même sortie attendu, obtenu %v puis %v", a, b)
	}
	re := regexp.MustCompile(`^0[67][0-9]{8}$`)
	if !re.MatchString(a.(string)) {
		t.Fatalf("téléphone invalide: %v", a)
	}
}
