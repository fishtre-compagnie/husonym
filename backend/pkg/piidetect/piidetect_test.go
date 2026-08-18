package piidetect

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name          string
		column        string
		dataType      string
		wantOK        bool
		wantCategory  string
		wantSensitive bool
		wantSource    mgmtv1alpha1.TransformerSource
	}{
		{"email simple", "email", "text", true, "email", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL},
		{"email composé", "customer_email", "varchar", true, "email", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL},
		{"email camelCase", "customerEmail", "varchar", true, "email", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL},
		{"phone string", "phone_number", "varchar", true, "phone_number", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER},
		{"phone numérique", "mobile", "bigint", true, "phone_number", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER},
		{"tel token", "tel", "varchar", true, "phone_number", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER},
		{"prénom -> first_name (pas last_name via 'nom')", "prenom", "varchar", true, "person_first_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FIRST_NAME},
		{"first_name", "first_name", "varchar", true, "person_first_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FIRST_NAME},
		{"nom token -> last_name", "nom", "varchar", true, "person_last_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_LAST_NAME},
		{"lastname", "lastName", "varchar", true, "person_last_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_LAST_NAME},
		{"full name", "full_name", "varchar", true, "person_full_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME},
		{"username", "username", "varchar", true, "username", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_USERNAME},
		{"adresse", "street_address", "text", true, "street_address", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS},
		{"city", "city", "varchar", true, "city", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY},
		{"zipcode", "zip_code", "varchar", true, "postal_code", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE},
		{"ssn", "ssn", "varchar", true, "ssn", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN},
		{"credit card", "creditCardNumber", "varchar", true, "credit_card", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER},
		{"ip", "ip_address", "varchar", true, "ip_address", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS},
		{"gender", "gender", "varchar", true, "gender", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_GENDER},

		// Faux positifs à NE PAS déclencher :
		{"hotel ne matche pas tel", "hotel", "varchar", false, "", false, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED},
		{"detail ne matche pas tel", "detail", "varchar", false, "", false, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED},
		{"filename ne matche pas name", "filename", "varchar", false, "", false, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED},
		{"id générique", "id", "int", false, "", false, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED},
		{"created_at", "created_at", "timestamp", false, "", false, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Classify(tc.column, tc.dataType)
			if ok != tc.wantOK {
				t.Fatalf("Classify(%q) ok = %v, want %v", tc.column, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Category != tc.wantCategory {
				t.Errorf("category = %q, want %q", got.Category, tc.wantCategory)
			}
			if got.Sensitive != tc.wantSensitive {
				t.Errorf("sensitive = %v, want %v", got.Sensitive, tc.wantSensitive)
			}
			if got.Suggested != tc.wantSource {
				t.Errorf("suggested = %v, want %v", got.Suggested, tc.wantSource)
			}
		})
	}
}

// nom_complet contient le token "nom" (nom de famille) tout en désignant un nom
// complet : excludeTokens doit départager sans casser la détection de "nom" seul.
func TestClassify_NomCompletNestPasUnNomDeFamille(t *testing.T) {
	cases := []struct {
		column  string
		wantCat string
	}{
		{"nom_complet", "person_full_name"},
		{"nomComplet", "person_full_name"},
		{"full_name", "person_full_name"},
		{"nom", "person_last_name"},
		{"last_name", "person_last_name"},
		{"prenom", "person_first_name"},
	}
	for _, tc := range cases {
		got, ok := Classify(tc.column, "text")
		if !ok {
			t.Errorf("Classify(%q) = non détecté", tc.column)
			continue
		}
		if got.Category != tc.wantCat {
			t.Errorf("Classify(%q) = %q, attendu %q", tc.column, got.Category, tc.wantCat)
		}
	}
}

// La date de naissance est une donnée personnelle. Le transformer suggéré dépend
// du type : cf. TestClassify_DateNaissanceSuggereSelonLeType. Ici on vérifie la
// catégorie et la sensibilité, sur une colonne TEXTE (aucune suggestion possible,
// le format de la source devrait être préservé).
func TestClassify_DateNaissanceSensibleSansTransformer(t *testing.T) {
	for _, col := range []string{"date_naissance", "birthdate", "dob"} {
		got, ok := Classify(col, "varchar(100)")
		if !ok {
			t.Errorf("Classify(%q) = non détecté", col)
			continue
		}
		if got.Category != "birth_date" {
			t.Errorf("Classify(%q) = %q, attendu \"birth_date\"", col, got.Category)
		}
		if !got.Sensitive {
			t.Errorf("Classify(%q) : Sensitive = false, attendu true", col)
		}
		if got.Suggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED {
			t.Errorf("Classify(%q) : transformer = %v, attendu UNSPECIFIED", col, got.Suggested)
		}
	}
	// Une date non personnelle ne doit pas être marquée.
	if got, ok := Classify("created_at", "timestamp"); ok && got.Sensitive {
		t.Errorf("Classify(\"created_at\") marquée sensible : %+v", got)
	}
}
