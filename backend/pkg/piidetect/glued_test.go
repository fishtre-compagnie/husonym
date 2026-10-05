package piidetect

import (
	"strings"
	"testing"
)

// A name written without separators is a finding when it is made of a keyword and of
// words the rules know: whose datum it is, which one, where and when, the part of it.
func TestClassify_GluedNamesOfKnownWords(t *testing.T) {
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

// A keyword glued to a word the rules do not know is part of another word: an address
// book is not an address, a tramway not a street, a capacity not a city.
func TestClassify_AKeywordGluedToAWordTheRulesDoNotKnow(t *testing.T) {
	expectNone(t,
		// the keyword opens the name
		"addressbook", "birthdayparty", "countryclub", "streetview", "mailboxquota",
		"revenuetotal", "revenuecenter", "revenuetype", "salaryman", "gehaltsklasse", "cellularnetwork",
		"pseudorandom", "pseudonymized", "pseudonymization", "sekretariat", "sekretarka", "geheimnis",
		"strassenbahn", "ausweisung", "plaatsvervanger", "plaatsingsdatum", "ciudadano", "gemeentehuis",
		"maritalarts", "ethnicfood",
		// the keyword closes the name
		"netincome", "grossincome", "operatingincome", "alkoholgehalt", "fettgehalt",
		"multicellular", "unicellular", "topsecret", "renaissance", "intercommune",
		"capacities", "velocities", "elasticities",
	)
}

// Two letters after a long keyword are read as a word of their own, the endings that
// derive a word from another aside.
func TestWordsOf_TwoLettersAfterALongKeyword(t *testing.T) {
	for name, want := range map[string]string{
		"postcodenl":     "postcode nl",
		"codigopostalpt": "codigo postal pt",
		"passwordmd":     "password md",
		// A keyword of less than six letters is not read beside letters the rules do not know.
		"phonexx": "phonexx",
		"cityfr":  "cityfr",
		// One letter is no word.
		"postcodex": "postcodex",
		// Nothing the rules do not know opens a name.
		"renaissance": "renaissance",
		"frpostcode":  "frpostcode",
		// Three letters the rules do not know are another word.
		"postcodexyz": "postcodexyz",
		// An ending that derives a word.
		"addressed":  "addressed",
		"passporten": "passporten",
		"secretly":   "secretly",
	} {
		if got := strings.Join(wordsOf(name), " "); got != want {
			t.Errorf("wordsOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// A mobile is a phone number; a mobile device, a mobile app and a mobile OS are not.
func TestClassify_AMobileAndWhatIsMobile(t *testing.T) {
	expectCategories(t, map[string]string{
		"mobile": "phone_number", "client_mobile": "phone_number", "mobilenumber": "phone_number",
		"MobilePhone": "phone_number", "mobile_work": "phone_number",
	})
	expectNone(t,
		"mobiledevice", "mobileos", "mobile_device", "mobile_os", "mobile_app", "mobileapp",
		"mobile_banking", "automobile", "immobile",
		"cellularnetwork", "cellular_network", "cellular_data", "multicellular",
	)
	expectCategories(t, map[string]string{"cellular": "phone_number", "cellular_number": "phone_number"})
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
		if got := strings.Join(wordsOf(name), " "); got != want {
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
