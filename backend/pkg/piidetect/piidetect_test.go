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
