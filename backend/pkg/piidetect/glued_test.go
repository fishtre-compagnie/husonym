package piidetect

import "testing"

// A name written without separators is a finding when a keyword of six letters or more
// opens or closes it, whatever the rest is.
func TestClassify_GluedNamesWithAWordTheRulesDoNotKnow(t *testing.T) {
	expectCategories(t, map[string]string{
		"addressline1":       "street_address",
		"AddressLine2":       "street_address",
		"adresselivraison":   "street_address",
		"strassehausnr":      "street_address",
		"adresseip":          "ip_address",
		"lastloginip":        "ip_address",
		"codigopostalpt":     "postal_code",
		"postcodenl":         "postal_code",
		"zipplus4":           "postal_code",
		"villeresidence":     "city",
		"maskedemail":        "email",
		"correoelectronico":  "email",
		"phoneext":           "phone_number",
		"phonemasked":        "phone_number",
		"phoneprefix":        "phone_number",
		"oldpassword":        "secret",
		"passwordresettoken": "secret",
		"passworddigest":     "secret",
		"passwordhint":       "secret",
		"passwordmd5":        "secret",
		"passwordresetcode":  "secret",
		"webhooksecret":      "secret",
		"idcardnumber":       "national_id",
		"idpassport":         "national_id",
		"passportexpiry":     "national_id",
		"passportissuedate":  "national_id",
		"lieunaissance":      "birth_date",
		"fechadenacimiento":  "birth_date",
		"fechanacimiento":    "birth_date",
		"datadinascita":      "birth_date",
		"datanascita":        "birth_date",
		"luogonascita":       "birth_date",
		"dataurodzenia":      "birth_date",
		"datanascimento":     "birth_date",
		"nombrepadres":       "person_first_name",
		"nomfamille":         "person_last_name",
		"annualsalary":       "salary",
		"salairebrut":        "salary",
	})
}

// A keyword found at the end or at the start of an ordinary word is not a finding.
func TestClassify_OrdinaryWordsThatHoldAKeyword(t *testing.T) {
	expectNone(t,
		"immobilier", "capacidade", "classname", "voicemail", "automobile", "immobile",
		"velocidade", "destinazione", "prenotazione", "werkplaats", "parkeerplaats",
		"secretary", "secretaria", "secretariat", "reconnaissance", "connaissance",
		"capacity", "zipped", "island", "homepage", "storage", "percentage", "message",
		"adressage", "addressable", "addressing", "gendered", "passwordless", "countryside",
	)
}

// A word of two letters does not open a glued name, "id" aside: "no" and "de" open
// ordinary words.
func TestWordsOf_AShortWordDoesNotOpenAGluedName(t *testing.T) {
	for name, want := range map[string]string{
		"noemail": "noemail",
		"nophone": "nophone",
		"deville": "deville",
		"idcard":  "id card",
		"iddni":   "id dni",
		"userid":  "user id",
		"telno":   "tel no",
	} {
		got := ""
		for i, word := range wordsOf(name) {
			if i > 0 {
				got += " "
			}
			got += word
		}
		if got != want {
			t.Errorf("wordsOf(%q) = %q, want %q", name, got, want)
		}
	}
	expectNone(t, "noemail", "nophone", "deville")
}

// "user" is a login as the whole name; beside other words it says whose datum it is.
func TestClassify_TheUserBesideOtherWords(t *testing.T) {
	expectCategories(t, map[string]string{
		"user": "username", "usuario": "username", "utilizador": "username",
		"user_age": "age", "user_email": "email", "user_ip": "ip_address",
	})
	expectNone(t, "user_agent", "user_role", "user_type", "user_status", "user_group")
}
