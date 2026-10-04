package piidetect

import "testing"

func expectCategories(t *testing.T, want map[string]string) {
	t.Helper()
	for name, category := range want {
		if got := classified(name); got != category {
			t.Errorf("Classify(%q) = %q, want %q", name, got, category)
		}
	}
}

func expectNone(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if got := classified(name); got != "" {
			t.Errorf("Classify(%q) = %q, want none", name, got)
		}
	}
}

// A name that holds two data is a finding when one rule sets it aside and another owns
// the word that did: the postal code of an address, the code of a gender.
func TestClassify_EveryMatchingRuleIsAsked(t *testing.T) {
	expectCategories(t, map[string]string{
		"address_zip_code":            "postal_code",
		"address_postal_code":         "postal_code",
		"billing_address_postal_code": "postal_code",
		"shipping_address_zip_code":   "postal_code",
		"user_zip_code":               "postal_code",
		"user_postal_code":            "postal_code",
		"mailing_postal_code":         "postal_code",
		"code_postal_ville":           "postal_code",
		"code_postal_adresse":         "postal_code",
		"codigo_postal_ciudad":        "postal_code",
		"codigo_postal_direccion":     "postal_code",
		"kod_pocztowy_miasto":         "postal_code",
		"gender_code":                 "gender",
		"gender_type":                 "gender",
		"code_sexe":                   "gender",
		"civilite_code":               "gender",
		"address_state_code":          "state",
		"direccion_estado":            "state",
		"endereco_estado":             "state",
		"user_tax_id":                 "national_id",
		"email_api_key":               "secret",
	})
	// Nothing owns the qualifier: the name stays set aside, whatever rules match it.
	expectNone(t, "user_email_format", "address_city_count", "user_address_type", "email_phone_status")
	// The code of a state is a finding beside an address, the key of a list elsewhere.
	expectNone(t, "state_code", "province_code", "country_code")
}

// Names that hold a keyword inside a longer word and are personal data all the same have
// a keyword of their own.
func TestClassify_WordsWithAKeywordOfTheirOwn(t *testing.T) {
	expectCategories(t, map[string]string{
		"ethnicity": "ethnicity", "gmail": "email", "mailbox": "email", "id_card_number": "national_id",
		"localidade": "city", "code_genre": "gender",
	})
}

// A code sent to check an address or a number is a secret while it is valid.
func TestClassify_ACodeThatProvesWhoSomeoneIs(t *testing.T) {
	for _, name := range []string{
		"email_verification_code", "verification_code", "phone_verification_code", "auth_code", "access_code",
		"code_acces", "recovery_codes", "backup_codes", "security_answer",
	} {
		if got := classified(name); got != "secret" {
			t.Errorf("Classify(%q) = %q, want secret", name, got)
		}
	}
}

func TestClassify_SocialSecurityNumbers(t *testing.T) {
	for _, name := range []string{
		"ssn", "SSN", "ssn4", "ssn1", "ssn_last4", "ssnum", "ssnumber", "ssno", "ssnhash", "ssn_hash",
		"empssn", "EMPSSN", "custssn", "patientssn", "memberssn", "employee_ssn", "num_secu", "numero_ss",
	} {
		if got := classified(name); got != "ssn" {
			t.Errorf("Classify(%q) = %q, want ssn", name, got)
		}
	}
	expectNone(t,
		"classname", "businessname", "processname", "witnessname", "class_name", "assn", "assn_id", "lessness",
	)
	expectCategories(t, map[string]string{
		"passnummer": "national_id", "pass_nummer": "national_id", "passnr": "national_id",
		"reisepassnummer": "national_id", "ausweisnummer": "national_id", "personalausweisnummer": "national_id",
	})
}

// A keyword is a word of the name, never letters inside a longer word.
func TestClassify_AKeywordIsAWord(t *testing.T) {
	expectNone(t,
		// mobil, movil, mobile
		"immobilier", "immobilisation", "montant_immobilisation", "mobilier", "mobilite", "mobility",
		"automovil", "automobile", "immobile", "mobili", "immobilien", "mobiliario",
		// cidade, city, stad, ort, ville
		"capacidade", "velocidade", "publicidade", "periodicidade", "capacity", "velocity", "publicity",
		"electricity_kwh", "elasticity", "specificity", "stadion", "export_path", "sort_order", "report",
		"transport", "sortie", "servilleta",
		// mail, login, zip, street, phone, name, ip, user
		"voicemail", "blogin", "zipped", "unzip_path", "streetview_url", "microphone", "headphones",
		"tip_amount", "zip_file", "superuser_flag",
		// address, adres
		"padres", "madres", "adressage_mode",
		// nom, name, nome, naam
		"nomenclature", "nominal", "economie", "binomial", "renamed", "namespace", "nomes",
		// pays, land, pais
		"paysage", "landing_page", "paisaje", "island", "inland",
		// tel, pin, cap, cp, dob
		"hotel", "motel", "telemetry", "template", "pinned", "spinner", "opinion", "capital", "escape", "cpu",
		"adobe",
		// sexe, genre, gender
		"sexennat", "genres_musicaux",
		// token, secret, password
		"tokenizer", "secretary", "secretariat",
	)
	// Glued names are read as their words.
	expectCategories(t, map[string]string{
		"customeremail": "email", "useremail": "email", "emailaddress": "email", "emailadresse": "email",
		"homephone": "phone_number", "phonenumber": "phone_number", "mobilephone": "phone_number",
		"telefonnummer": "phone_number", "handynummer": "phone_number", "mobilnummer": "phone_number",
		"telno": "phone_number", "tel1": "phone_number", "phone2": "phone_number",
		"firstname": "person_first_name", "lastname": "person_last_name", "fullname": "person_full_name",
		"custfirstname": "person_first_name", "dateofbirth": "birth_date", "birthdate": "birth_date",
		"streetaddress": "street_address", "address1": "street_address", "zipcode": "postal_code",
		"userpassword": "secret", "passwordhash": "secret", "apikey": "secret", "creditcardnumber": "credit_card",
		"emails": "email", "phones": "phone_number", "addresses": "street_address", "passwords": "secret",
		"nombredeusuario": "username", "kreditkartennummer": "credit_card",
	})
}

// A word that is personal in one language and ordinary in another is a finding as the
// whole name, or beside a word of its own language.
func TestClassify_WordsOfTwoLanguages(t *testing.T) {
	expectCategories(t, map[string]string{
		// Spanish "nombre" is a first name, French "nombre" a count.
		"nombre": "person_first_name", "nombre_cliente": "person_first_name", "nombre_y_apellido": "person_first_name",
		"nombre_empleado": "person_first_name", "nombres": "person_first_name",
		// Italian "cap" is a postal code, English "cap" a limit.
		"cap": "postal_code", "cap_residenza": "postal_code", "cap_spedizione": "postal_code",
		// German and Dutch "land" is a country, English "land" is ground.
		"land": "country", "kunden_land": "country", "geburtsland": "country", "herkunftsland": "country",
		// Spanish "dni" and "nie" are identity numbers, Polish "dni" are days and "nie" is no.
		"dni": "national_id", "numero_dni": "national_id", "dni_cliente": "national_id", "nie": "national_id",
		// Spanish "clave" is a password or the key of a thing.
		"clave": "secret", "clave_acceso": "secret", "clave_usuario": "secret",
		// Spanish and Portuguese "genero", French "genre": of a person or of a work.
		"genero": "gender", "genre": "gender", "genero_cliente": "gender", "genre_client": "gender",
		// Spanish and Portuguese "estado": the state of an address or a status.
		"estado_provincia": "state",
		// Italian "via" is a street.
		"via": "street_address", "via_residenza": "street_address",
	})
	expectNone(t,
		"nombre_articles", "nombre_pages", "nombre_de_lignes", "nombre_max", "nombre_jours", "nombre_heures",
		"nombre_places", "nombre_clients", "nombre_total", "nombre_essais",
		"cap_table", "market_cap", "rate_cap", "salary_cap", "price_cap", "cap_amount", "hard_cap",
		"land_use", "land_value", "land_area", "land_registry", "land_parcel",
		"dni_urlopu", "ilosc_dni", "dni_robocze", "nie_pokazuj",
		"clave_producto", "clave_unidad", "clave_cliente", "clave_sat", "clave_primaria", "clave_foranea",
		"genero_musical", "genero_pelicula", "genre_musical", "genre_film", "music_genre", "book_genre",
		"estado", "estado_pedido",
		"via_api", "sent_via",
	)
}

// A token or a pin is a secret, except the ones that turn a page or mark a map.
func TestClassify_TokensAndPins(t *testing.T) {
	for _, name := range []string{
		"api_token", "access_token", "refresh_token", "auth_token", "pin_code", "user_pin", "token", "pin",
		"reset_token", "csrf_token", "card_pin", "pin_hash", "device_token",
	} {
		if got := classified(name); got != "secret" {
			t.Errorf("Classify(%q) = %q, want secret", name, got)
		}
	}
	expectNone(t,
		"page_token", "next_page_token", "next_token", "continuation_token", "sync_token", "token_usage",
		"token_count", "map_pin", "pin_lat", "pin_lng", "pin_color", "pin_x", "pin_y", "pin_icon",
		"password_changed_at", "password_expires_at", "token_expires_at", "password_updated_at",
		"password_policy", "password_strength", "password_attempts", "password_set",
	)
}

// The name of a thing is not the name of a person.
func TestClassify_NamesOfOrdinaryThings(t *testing.T) {
	expectNone(t,
		"nome_corso", "nome_banca", "nome_piano", "nom_article", "nom_banque", "hotel_name", "bank_name",
		"plan_name", "item_name", "course_name", "nombre_banco", "nombre_curso", "naam_cursus", "name_artikel",
		"nome_artigo", "businessname", "process_name",
	)
}

// A name that opens with "id" is a reference, as one that ends with it.
func TestClassify_ALeadingIdIsAReference(t *testing.T) {
	expectNone(t,
		"id_usuario", "id_pais", "id_ciudad", "id_provincia", "id_direccion", "id_cidade", "id_endereco",
		"id_user", "id_adresse", "id_ville", "id_utente", "id_land", "id_kraj", "idUser", "id_email",
	)
	expectCategories(t, map[string]string{"id_passport": "national_id", "id_dni": "national_id"})
}

// What happened to a datum, and when, is not the datum.
func TestClassify_EventsAboutADatum(t *testing.T) {
	expectNone(t,
		"email_verified_at", "email_verified_date", "email_confirmed_at", "email_consent_date",
		"phone_number_verified_at", "phone_verified_at", "address_updated_at", "email_sent_at",
		"username_changed_at", "last_login_at", "email_bounced", "login_attempts", "email_opt_in",
	)
	expectCategories(t, map[string]string{"verified_email": "email", "confirmed_phone": "phone_number"})
}

// A rule for a datum written in text does not apply to a column whose type cannot hold
// it. A type that is not given refuses nothing.
func TestClassify_TheTypeOfTheColumn(t *testing.T) {
	cases := []struct {
		name, dataType, want string
	}{
		{"nombre", "integer", ""},
		{"nombre", "int4", ""},
		{"nombre", "varchar(80)", "person_first_name"},
		{"nombre", "", "person_first_name"},
		{"first_name", "bigint", ""},
		{"email", "boolean", ""},
		{"email", "tinyint(1)", ""},
		{"email", "timestamp with time zone", ""},
		{"email", "character varying(255)", "email"},
		{"email", "jsonb", "email"},
		{"email", "bytea", "email"},
		{"city", "smallint", ""},
		{"country", "integer", ""},
		{"address", "numeric(10,2)", ""},
		{"username", "uuid", ""},
		{"password", "timestamp", ""},
		{"password", "boolean", ""},
		{"password", "text", "secret"},
		{"pin", "integer", "secret"},
		{"phone", "bigint", "phone_number"},
		{"phone", "date", ""},
		{"postal_code", "integer", "postal_code"},
		{"ssn", "bigint", "ssn"},
		{"ssn", "bit", ""},
		{"card_number", "numeric(16,0)", "credit_card"},
		{"gender", "smallint", "gender"},
		{"gender", "boolean", "gender"},
		{"gender", "date", ""},
		{"birth_date", "date", "birth_date"},
		{"birth_date", "timestamp without time zone", "birth_date"},
		{"birth_date", "boolean", ""},
		{"ip", "bigint", "ip_address"},
		{"ip", "inet", "ip_address"},
	}
	for _, tc := range cases {
		got := ""
		if c, ok := Classify(tc.name, tc.dataType); ok {
			got = c.Category
		}
		if got != tc.want {
			t.Errorf("Classify(%q, %q) = %q, want %q", tc.name, tc.dataType, got, tc.want)
		}
	}
}

// Data the job's own name rules knew and the shared ones now hold.
func TestClassify_MoneyAndAge(t *testing.T) {
	expectCategories(t, map[string]string{
		"iban": "iban", "bank_account": "bank_account", "bank_account_number": "bank_account",
		"account_number": "bank_account", "account_num": "bank_account", "kontonummer": "bank_account",
		"salary": "salary", "current_salary": "salary", "salaire": "salary", "gehalt": "salary",
		"age": "age", "edad": "age",
		"drivers_license": "national_id", "driver_licence": "national_id", "cc_number": "credit_card",
		"cc": "credit_card", "cc_num": "credit_card",
	})
	expectNone(t, "age_group_label_count", "page", "image", "usage", "storage", "average")
	for _, name := range []string{"iban", "salary", "age", "bank_account"} {
		c, ok := Classify(name, "text")
		if !ok || !c.Sensitive {
			t.Errorf("Classify(%q) is not sensitive", name)
		}
	}
}

func TestWordsOf(t *testing.T) {
	for name, want := range map[string][]string{
		"customerEmail_2":  {"customer", "email"},
		"telefonnummer":    {"telefonnummer"},
		"homephone":        {"home", "phone"},
		"dateofbirth":      {"date", "of", "birth"},
		"immobilier":       {"immobilier"},
		"EMPSSN":           {"empssn"},
		"userid":           {"user", "id"},
		"island":           {"island"},
		"card_last4":       {"card", "last"},
		"IPAddress":        {"ipaddress"},
		"capacidade":       {"capacidade"},
		"code_postal":      {"code", "postal"},
	} {
		got := wordsOf(name)
		if len(got) != len(want) {
			t.Errorf("wordsOf(%q) = %v, want %v", name, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("wordsOf(%q) = %v, want %v", name, got, want)
				break
			}
		}
	}
}
