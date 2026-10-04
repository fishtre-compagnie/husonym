package rules

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/profile"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect/report"
	"github.com/stretchr/testify/require"
)

// The names that are recognized as a whole, whatever their case, with an underscore, a
// dash or nothing between their words.
func Test_Find_ByNameForm(t *testing.T) {
	forms := map[report.Category][]string{
		report.NationalID: {
			"ssn", "social_security_number", "social_security", "social_security_no", "tax_id",
			"passport", "passport_number", "driver_license", "driver_licence",
		},
		report.Contact: {"email", "email_address", "phone_number", "telephone_number", "mobile_number"},
		report.Personal: {
			"birth_date", "first_name", "last_name", "full_name", "dob", "date_of_birth", "age",
		},
		report.Financial: {
			"credit_card", "credit_card_num", "credit_card_number", "cc", "cc_num", "cc_number",
			"account_num", "account_number", "bank_account", "bank_account_num", "bank_account_number",
			"salary", "current_salary",
		},
		report.Authentication: {"password", "passwd", "auth_token", "secret"},
		report.Location:       {"address", "mailing_address", "zip_code", "postal_code", "ip_address"},
	}
	for category, names := range forms {
		for _, name := range names {
			for _, spelling := range spellings(name) {
				got, ok := byNameForm(spelling)
				require.True(t, ok, spelling)
				require.Equal(t, category, got, spelling)

				finding, ok := Find(spelling, "text", nil)
				require.True(t, ok, spelling)
				require.Equal(t, Finding{Category: category, Evidence: "name"}, finding, spelling)
			}
		}
	}
}

// spellings returns a name as it is, in uppercase, with dashes and without separator.
func spellings(name string) []string {
	upper, dashed, joined := []rune{}, []rune{}, []rune{}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			upper = append(upper, r-'a'+'A')
		} else {
			upper = append(upper, r)
		}
		if r == '_' {
			dashed = append(dashed, '-')
			continue
		}
		dashed = append(dashed, r)
		joined = append(joined, r)
	}
	return []string{name, string(upper), string(dashed), string(joined)}
}

// A form is the whole name, not a part of it.
func Test_ByNameForm_IsTheWholeName(t *testing.T) {
	for _, name := range []string{
		"phone", "telephone", "mobile", "customer_email", "email_format", "age_group", "cc_type",
		"address_count", "secret_santa", "passwords", "xssn", "", "account",
	} {
		_, ok := byNameForm(name)
		require.False(t, ok, name)
	}
}

// Names the token rules of the API's detection recognize, in English and in French, each
// under the category of the report.
func Test_Find_ByNameTokens(t *testing.T) {
	for name, category := range map[string]report.Category{
		"phone":            report.Contact,
		"customer_email":   report.Contact,
		"courriel":         report.Contact,
		"tel_portable":     report.Contact,
		"username":         report.Personal,
		"prenom":           report.Personal,
		"nom":              report.Personal,
		"customerFullName": report.Personal,
		"sexe":             report.Personal,
		"date_naissance":   report.Personal,
		"client_ip":        report.Location,
		"adresse":          report.Location,
		"ville":            report.Location,
		"region":           report.Location,
		"code_postal":      report.Location,
		"pays":             report.Location,
		"nir":              report.NationalID,
		"card_number":      report.Financial,
		"vorname":          report.Personal,
		"apellido":         report.Personal,
		"indirizzo":        report.Location,
		"woonplaats":       report.Location,
		"pesel":            report.NationalID,
		"codice_fiscale":   report.NationalID,
		"senha":            report.Authentication,
		"api_key":          report.Authentication,
		"refresh_token":    report.Authentication,
		"password_hash":    report.Authentication,
	} {
		finding, ok := Find(name, "text", nil)
		require.True(t, ok, name)
		require.Equal(t, Finding{Category: category, Evidence: "name"}, finding, name)
	}
}

// A name that designates a thing, or that refers to another row, is not a finding.
func Test_Find_NamesThatAreNotPersonalData(t *testing.T) {
	for _, name := range []string{
		"product_name", "file_name", "nom_fichier", "user_id", "email_uuid", "customer_ref",
		"created_by_user", "id", "quantity", "created_at", "c17", "status", "",
		// A name that qualifies a datum is not one.
		"email_format", "phone_type", "address_count", "is_email_verified", "country_code", "token_type",
	} {
		_, ok := Find(name, "text", nil)
		require.False(t, ok, name)
	}
}

func withHits(hits ...profile.Share) *profile.Profile {
	return &profile.Profile{Rows: 200, Kind: profile.KindText, Hits: hits}
}

// A format most values have is a finding whatever the name of the column: from a half of
// the values up.
func Test_Find_ByValueFormat(t *testing.T) {
	for detector, category := range map[string]report.Category{
		"email":         report.Contact,
		"phone_number":  report.Contact,
		"iban":          report.Financial,
		"credit_card":   report.Financial,
		"nir":           report.NationalID,
		"ip_address":    report.Location,
		"gender":        report.Personal,
		"password_hash": report.Authentication,
	} {
		_, ok := Find("c17", "text", withHits(profile.Share{Name: detector, Share: 0.49}))
		require.False(t, ok, detector)

		finding, ok := Find("c17", "text", withHits(profile.Share{Name: detector, Share: 0.5}))
		require.True(t, ok, detector)
		require.Equal(t, Finding{Category: category, Evidence: "values:" + detector + " 0.5"}, finding)

		finding, ok = Find("c17", "text", withHits(profile.Share{Name: detector, Share: 1}))
		require.True(t, ok, detector)
		require.Equal(t, Finding{Category: category, Evidence: "values:" + detector + " 1"}, finding)
	}

	finding, ok := Find("c17", "text", withHits(profile.Share{Name: "iban", Share: 0.97}))
	require.True(t, ok)
	require.Equal(t, "values:iban 0.97", finding.Evidence)
}

// The most frequent format decides, and a format that identifies a company decides that
// the rules find nothing: a less specific check does not claim its values.
func Test_Find_TheMostFrequentFormatDecides(t *testing.T) {
	finding, ok := Find("c17", "text", withHits(
		profile.Share{Name: "nir", Share: 0.9},
		profile.Share{Name: "credit_card", Share: 0.6},
	))
	require.True(t, ok)
	require.Equal(t, report.NationalID, finding.Category)

	_, ok = Find("c17", "text", withHits(profile.Share{Name: "siret", Share: 1}))
	require.False(t, ok)
	_, ok = Find("c17", "text", withHits(
		profile.Share{Name: "siret", Share: 1},
		profile.Share{Name: "credit_card", Share: 1},
	))
	require.False(t, ok)

	_, ok = Find("c17", "text", withHits(profile.Share{Name: "a check this package does not know", Share: 1}))
	require.False(t, ok)
}

// The name answers first, and what the values are not does not withdraw it.
func Test_Find_TheNameComesBeforeTheValues(t *testing.T) {
	finding, ok := Find("email", "text", withHits(profile.Share{Name: "iban", Share: 1}))
	require.True(t, ok)
	require.Equal(t, Finding{Category: report.Contact, Evidence: "name"}, finding)

	finding, ok = Find("email", "text", withHits())
	require.True(t, ok)
	require.Equal(t, Finding{Category: report.Contact, Evidence: "name"}, finding)
}

// The format checks are those of the API's content scan, in its order.
func Test_Detectors(t *testing.T) {
	detectors := Detectors()
	names := make([]string, 0, len(detectors))
	for _, detector := range detectors {
		names = append(names, detector.Name)
	}
	require.Equal(t, []string{"password_hash", "email", "iban", "nir", "siret", "credit_card", "ip_address", "phone_number", "gender"}, names)
	require.True(t, detectors[2].Match("FR76 3000 6000 0112 3456 7890 189"))
	require.False(t, detectors[2].Match("FR76 3000 6000 0112 3456 7890 180"))
}

// From sampled values to a finding: a column of IBANs under a neutral name.
func Test_Find_OnAProfiledColumn(t *testing.T) {
	table := profile.NewTable(Detectors())
	for _, iban := range []string{
		"FR7630006000011234567890189", "FR1420041010050500013M02606", "FR8810278073000002056360189", "unknown",
	} {
		table.Add(map[string]any{"c17": iban})
	}
	finding, ok := Find("c17", "text", table.Profile("c17"))
	require.True(t, ok)
	require.Equal(t, Finding{Category: report.Financial, Evidence: "values:iban 0.75"}, finding)
}
