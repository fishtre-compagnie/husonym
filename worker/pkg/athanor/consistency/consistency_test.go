package consistency

import "testing"

var projectKey = []byte("clé-de-projet-secrète-de-test-32b")

// LA propriété fondatrice (RFC §8.1) : la même valeur, sous le même type
// sémantique, produit la même graine — même si elle vient de colonnes ou de
// bases différentes. On simule « deux bases » par deux Derivers indépendants
// (comme deux workers qui ne se connaissent pas) partageant clé et scope.
func TestCrossDatabaseConsistency(t *testing.T) {
	pgWorker := New(projectKey, "org") // traite pg.users.email
	myWorker := New(projectKey, "org") // traite mysql.contacts.mail

	// Les deux colonnes sont classées person.email → même clé de domaine.
	pgSeed := pgWorker.Domain("person.email").Seed("Jean.Dupont@Exemple.FR")
	mySeed := myWorker.Domain("person.email").Seed("jean.dupont@exemple.fr")

	if pgSeed != mySeed {
		t.Fatalf("même email + même type sémantique doivent donner la même graine (cohérence inter-bases cassée)")
	}
}

// La clé de domaine vient du TYPE SÉMANTIQUE, pas du nom de colonne : deux types
// différents pour la même valeur donnent des graines différentes.
func TestDifferentSemanticTypesDiverge(t *testing.T) {
	d := New(projectKey, "org")
	email := d.Domain("person.email").Seed("dupont")
	name := d.Domain("person.last_name").Seed("dupont")
	if email == name {
		t.Fatal("types sémantiques différents doivent produire des graines différentes")
	}
}

// Isolation cryptographique : changer de scope ou de clé de projet change tout.
func TestScopeAndKeyIsolation(t *testing.T) {
	v := "alice@exemple.fr"
	org := New(projectKey, "org").Domain("person.email").Seed(v)
	proj := New(projectKey, "project:42").Domain("person.email").Seed(v)
	if org == proj {
		t.Fatal("des scopes différents doivent isoler les graines")
	}

	otherKey := New([]byte("une-autre-clé-de-projet"), "org").Domain("person.email").Seed(v)
	if org == otherKey {
		t.Fatal("des clés de projet différentes doivent isoler les graines")
	}
}

// La canonicalisation fait converger les variantes équivalentes.
func TestCanonicalization(t *testing.T) {
	dom := New(projectKey, "org").Domain("person.email")
	a := dom.Seed("  Alice@Exemple.FR ")
	b := dom.Seed("alice@exemple.fr")
	if a != b {
		t.Fatal("casse et espaces de bordure doivent être neutralisés par défaut")
	}

	// Avec PreserveCase, la casse compte de nouveau.
	cs := New(projectKey, "org").Domain("id.sensible").WithCanonicalizer(PreserveCase)
	if cs.Seed("ABC") == cs.Seed("abc") {
		t.Fatal("PreserveCase doit distinguer les casses")
	}
}

// Les primitives graine→valeur sont déterministes et bornées.
func TestSeedPrimitives(t *testing.T) {
	s := New(projectKey, "org").Domain("hr.salary").Seed("emp-123")

	if s.IntInRange(10, 10) != 10 {
		t.Fatal("IntInRange sur une plage singleton doit renvoyer la borne")
	}
	for i := 0; i < 1000; i++ {
		s2 := New(projectKey, "org").Domain("hr.salary").Seed("emp-" + string(rune('A'+i%26)))
		if v := s2.IntInRange(1, 100); v < 1 || v > 100 {
			t.Fatalf("IntInRange hors bornes : %d", v)
		}
		if idx := s2.Index(16); idx < 0 || idx >= 16 {
			t.Fatalf("Index hors bornes : %d", idx)
		}
		if f := s2.Float01(); f < 0 || f >= 1 {
			t.Fatalf("Float01 hors [0,1) : %v", f)
		}
	}

	// Déterminisme : re-dériver donne exactement la même valeur.
	again := New(projectKey, "org").Domain("hr.salary").Seed("emp-123")
	if s.IntInRange(1, 1_000_000) != again.IntInRange(1, 1_000_000) {
		t.Fatal("la dérivation doit être reproductible")
	}
}

// Démonstration bout-en-bout : un faker de prénom déterministe et cohérent.
func TestDeterministicDictionaryFaker(t *testing.T) {
	dict := []string{"Léa", "Karim", "Chloé", "Hugo", "Inès", "Noah"}
	dom := New(projectKey, "org").Domain("person.first_name")

	name := func(v string) string { return dict[dom.Seed(v).Index(len(dict))] }

	// Le même client donne toujours le même prénom anonymisé.
	if name("client-777") != name("client-777") {
		t.Fatal("faker déterministe attendu")
	}
	// Et c'est stable à travers une re-dérivation indépendante (autre worker).
	dom2 := New(projectKey, "org").Domain("person.first_name")
	if dom.Seed("client-777").Index(len(dict)) != dom2.Seed("client-777").Index(len(dict)) {
		t.Fatal("cohérence inter-worker attendue")
	}
}
