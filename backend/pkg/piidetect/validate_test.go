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
