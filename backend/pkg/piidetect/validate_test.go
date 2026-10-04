package piidetect

import (
	"fmt"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Valeurs issues du jeu de test scripts/testdata (clés de contrôle réelles).
var (
	validNIRs = []string{
		"180017511600146", "275126938800281", "165083305500471",
		"292066401900374", "177021234500679", "288114567800902",
	}
	validIBANs = []string{
		"FR7630006000011234567890189",
		"FR1420041010050500013M02606",
		"FR8810278073000002056360189",
	}
	validSirets = []string{"44320098000011", "56200515000023", "39150672000039"}
	validCards  = []string{"4970100123456788", "5555123400001234", "4000000123456784"}
)

// The checks are handed out in the order ClassifyValues applies them, each under the
// category it gives, and each is the check ClassifyValues runs.
func TestValueDetectors(t *testing.T) {
	passing := map[string]string{
		"password_hash": "$2b$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
		"email":         "jean.dupont@example.org",
		"iban":          validIBANs[0],
		"nir":           validNIRs[0],
		"siret":         validSirets[0],
		"credit_card":   validCards[0],
		"ip_address":    "192.0.2.17",
		"phone_number":  "06 12 34 56 78",
		"gender":        "Mme",
	}
	want := []string{"password_hash", "email", "iban", "nir", "siret", "credit_card", "ip_address", "phone_number", "gender"}

	detectors := ValueDetectors()
	got := make([]string, 0, len(detectors))
	for _, detector := range detectors {
		got = append(got, detector.Category)
		value, known := passing[detector.Category]
		if !known {
			t.Fatalf("a check of an unexpected category: %q", detector.Category)
		}
		if !detector.Match(value) {
			t.Errorf("the %s check refuses %q", detector.Category, value)
		}
		if detector.Match("not a formatted value") {
			t.Errorf("the %s check accepts a plain sentence", detector.Category)
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("checks = %v, want %v", got, want)
	}
}

func TestIsNIR(t *testing.T) {
	for _, v := range validNIRs {
		if !IsNIR(v) {
			t.Errorf("IsNIR(%q) = false, attendu true", v)
		}
	}
	// Le même NIR avec séparateurs doit rester valide.
	if !IsNIR("1 80 01 75 116 001 46") {
		t.Error("IsNIR avec espaces = false, attendu true")
	}
	bad := []string{
		"180017511600147", // clé fausse
		"1800175116001",   // 13 chiffres : clé absente
		"380017511600146", // sexe invalide
		"181317511600146", // mois 13
		"",
		"abcdefghijklmno",
	}
	for _, v := range bad {
		if IsNIR(v) {
			t.Errorf("IsNIR(%q) = true, attendu false", v)
		}
	}
}

func TestIsIBAN(t *testing.T) {
	for _, v := range validIBANs {
		if !IsIBAN(v) {
			t.Errorf("IsIBAN(%q) = false, attendu true", v)
		}
	}
	if !IsIBAN("FR76 3000 6000 0112 3456 7890 189") {
		t.Error("IsIBAN avec espaces = false, attendu true")
	}
	for _, v := range []string{"FR7730006000011234567890189", "FR76", "0630006000011234567890189", ""} {
		if IsIBAN(v) {
			t.Errorf("IsIBAN(%q) = true, attendu false", v)
		}
	}
}

func TestIsCreditCard(t *testing.T) {
	for _, v := range validCards {
		if !IsCreditCard(v) {
			t.Errorf("IsCreditCard(%q) = false, attendu true", v)
		}
	}
	for _, v := range []string{"4970100123456789", "1234567812345678", "497010012345", ""} {
		if IsCreditCard(v) {
			t.Errorf("IsCreditCard(%q) = true, attendu false", v)
		}
	}
}

func TestIsFrenchPhone(t *testing.T) {
	good := []string{"0710203040", "06 11 23 37 45", "+33 7 10 20 30 40", "0033710203040", "01.23.45.67.89"}
	for _, v := range good {
		if !IsFrenchPhone(v) {
			t.Errorf("IsFrenchPhone(%q) = false, attendu true", v)
		}
	}
	for _, v := range []string{"0010203040", "071020304", "07102030401", "1234567890", ""} {
		if IsFrenchPhone(v) {
			t.Errorf("IsFrenchPhone(%q) = true, attendu false", v)
		}
	}
}

func TestIsFrenchPostalCode(t *testing.T) {
	for _, v := range []string{"33000", "75001", "01000", "98000"} {
		if !IsFrenchPostalCode(v) {
			t.Errorf("IsFrenchPostalCode(%q) = false, attendu true", v)
		}
	}
	for _, v := range []string{"00123", "99000", "3300", "330000", ""} {
		if IsFrenchPostalCode(v) {
			t.Errorf("IsFrenchPostalCode(%q) = true, attendu false", v)
		}
	}
}

// Le cas qui motive tout ce fichier : deux des six NIR du jeu de test passent
// Luhn par hasard, ce qui fait que Presidio les annonce CREDIT_CARD à 1.00.
// L'ordre des validateurs doit donner le NIR, pas la carte.
func TestClassifyValues_NirNePasConfondreAvecCarte(t *testing.T) {
	got, ok := ClassifyValues(validNIRs, "text")
	if !ok {
		t.Fatal("ClassifyValues sur des NIR = non détecté")
	}
	if got.Category != "nir" {
		t.Errorf("catégorie = %q, attendu \"nir\" (collision Luhn non arbitrée)", got.Category)
	}
	if got.Confidence != mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED {
		t.Errorf("confiance = %v, attendu CONFIRMED", got.Confidence)
	}
	if got.Method != mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CHECKSUM {
		t.Errorf("méthode = %v, attendu CHECKSUM", got.Method)
	}
}

func TestClassifyValues_Categories(t *testing.T) {
	cases := []struct {
		name     string
		values   []string
		wantCat  string
		wantConf mgmtv1alpha1.PiiConfidence
	}{
		{
			name:     "emails",
			values:   []string{"a.b@example.fr", "c@d.com", "e.f@g.co.uk", "h@i.fr"},
			wantCat:  "email",
			wantConf: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
		},
		{
			name:     "ibans",
			values:   validIBANs,
			wantCat:  "iban",
			wantConf: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
		},
		{
			name:     "sirets",
			values:   validSirets,
			wantCat:  "siret",
			wantConf: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
		},
		{
			name:     "telephones",
			values:   []string{"0710203040", "0611233745", "0123456789", "0698765432"},
			wantCat:  "phone_number",
			wantConf: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
		},
		{
			name:     "ips",
			values:   []string{"192.168.1.10", "10.0.0.1", "8.8.8.8", "::1"},
			wantCat:  "ip_address",
			wantConf: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ClassifyValues(tc.values, "text")
			if !ok {
				t.Fatalf("ClassifyValues = non détecté")
			}
			if got.Category != tc.wantCat {
				t.Errorf("catégorie = %q, attendu %q", got.Category, tc.wantCat)
			}
			if got.Confidence != tc.wantConf {
				t.Errorf("confiance = %v, attendu %v", got.Confidence, tc.wantConf)
			}
			if got.Evidence == "" {
				t.Error("Evidence vide : l'infobulle n'aurait rien à afficher")
			}
		})
	}
}

func TestClassifyValues_RienSurDonneesQuelconques(t *testing.T) {
	cases := [][]string{
		{"alpha", "beta", "gamma", "delta"},
		{"1", "2", "3", "4"},
		{"Jean Dupont", "Marie Martin", "Pierre Bernard"}, // du ressort du NER
		{},
		{"a@b.fr"}, // sous minSamples : pas de conclusion statistique
	}
	for i, values := range cases {
		t.Run(fmt.Sprintf("cas_%d", i), func(t *testing.T) {
			if got, ok := ClassifyValues(values, "text"); ok {
				t.Errorf("ClassifyValues = %+v, attendu aucune détection", got)
			}
		})
	}
}

// Une colonne majoritairement valide mais pas totalement doit être signalée sans
// être confirmée : c'est exactement le cas du badge orange.
func TestClassifyValues_MajoriteSeulementDonneNeedsReview(t *testing.T) {
	values := []string{
		"a@b.fr", "c@d.fr", "e@f.fr", "g@h.fr", "i@j.fr", "k@l.fr",
		"non renseigne", "n/a", "-", "inconnu",
	}
	got, ok := ClassifyValues(values, "text")
	if !ok {
		t.Fatal("ClassifyValues = non détecté")
	}
	if got.Confidence != mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW {
		t.Errorf("confiance = %v, attendu NEEDS_REVIEW (6/10 valides)", got.Confidence)
	}
}

func TestClassifyValues_TelephoneNumeriqueChoisitLaVarianteInt(t *testing.T) {
	got, ok := ClassifyValues([]string{"0710203040", "0611233745", "0123456789"}, "bigint")
	if !ok {
		t.Fatal("ClassifyValues = non détecté")
	}
	if got.Suggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER {
		t.Errorf("transformer = %v, attendu GENERATE_INT64_PHONE_NUMBER", got.Suggested)
	}
}

// Un salaire annuel a exactement la forme d'un code postal français. Aucun
// validateur de contenu ne doit se déclencher là-dessus : le code postal se
// détecte par le nom de colonne, pas par la forme des valeurs.
func TestClassifyValues_SalaireNestPasUnCodePostal(t *testing.T) {
	salaires := []string{"28000", "29450", "30900", "32350", "33800"}
	if got, ok := ClassifyValues(salaires, "int"); ok {
		t.Errorf("ClassifyValues(salaires) = %+v, attendu aucune détection", got)
	}
	// Le contrôle unitaire reste disponible pour valider une valeur ponctuelle.
	if !IsFrenchPostalCode("33000") {
		t.Error("IsFrenchPostalCode ne doit pas avoir été supprimé")
	}
}

// The encodings password hashing schemes store their output in. A bare hexadecimal
// digest is not one: it may be the hash of anything.
func TestIsPasswordHash(t *testing.T) {
	for _, v := range []string{
		"$2b$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
		"$2y$10$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
		"$2a$04$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
		"$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$RdescudvJCsgt3ub+b+dWRWJTmaaJObG",
		"$argon2i$v=19$m=16,t=2,p=1$c29tZXNhbHQ$u1eU6mZFG4/OOoTdAtM5SQ",
		"$scrypt$ln=16,r=8,p=1$aM15713r3Xsvxbi31lqr1Q$nFNh2CVHVjNldFVKDHDlm4CbdRSCdEBsjjJxD+iCs5E",
		"$pbkdf2-sha256$29000$N2YMIWQsBWBMae09x1jrPQ$1t8iyB2A.WF/Z5JZv.lfCIhXXN33N23OSgQYThBYRfk",
		"pbkdf2_sha256$260000$sIbDS1bDWp1mxJKmJqgAxO$O3UUmRPhrW5aUNzMVjWxDwYNYNrCrGwWz2kVn0GQx4E=",
		"$6$rounds=5000$usesomesillystri$D4IrlXatmP7rx3P3InaxBeoomnAihCKRVQP22JkLQ4hAGBrKwJzFzs1k2sTUYhW0sAFSJ.qNFTOvjN4sk7.EO1",
		"$5$saltsalt$5B8vYYiY.CVt1RlTTf8KbXBH3hsxY/GNooZaBBGWEc5",
		"$1$saltsalt$qjXMvbEw8oaL.CzflDtaK/",
		"$P$BWQ4EyG4lqVM8p0bqg1vE7xTdy2pQ6.",
	} {
		if !IsPasswordHash(v) {
			t.Errorf("IsPasswordHash(%q) = false, want true", v)
		}
	}
	for _, v := range []string{
		"", "password", "hunter2", "$2b$12$tooshort", "5f4dcc3b5aa765d61d8327deb882cf99",
		"da39a3ee5e6b4b0d3255bfef95601890afd80709", "$notascheme$abc$def", "jean.dupont@example.org",
		"$2b$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW and more",
	} {
		if IsPasswordHash(v) {
			t.Errorf("IsPasswordHash(%q) = true, want false", v)
		}
	}
}

// A column of password hashes is found by its values, whatever its name.
func TestClassifyValues_PasswordHashes(t *testing.T) {
	got, ok := ClassifyValues([]string{
		"$2b$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
		"$2b$12$Q9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
		"$2b$12$S9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
	}, "varchar")
	if !ok {
		t.Fatal("ClassifyValues finds nothing")
	}
	if got.Category != "password_hash" || !got.Sensitive ||
		got.Confidence != mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED ||
		got.Suggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED {
		t.Errorf("ClassifyValues = %+v", got)
	}
}
