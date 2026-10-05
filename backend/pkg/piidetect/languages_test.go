package piidetect

import "testing"

// classified returns the category Classify gives a column name, "" when it gives none.
func classified(name string) string {
	c, ok := Classify(name, "text")
	if !ok {
		return ""
	}
	return c.Category
}

// The names of columns that hold personal data, as schemas in each language write them.
// The dictionaries were written from the languages, not from a data set.
func TestClassify_Languages(t *testing.T) {
	languages := map[string]map[string]string{
		"de": {
			"e_mail": "email", "email_adresse": "email",
			"telefon": "phone_number", "telefonnummer": "phone_number", "mobilnummer": "phone_number", "handy": "phone_number", "rufnummer": "phone_number",
			"benutzername": "username",
			"vorname":      "person_first_name",
			"nachname":     "person_last_name", "familienname": "person_last_name",
			"vollstaendiger_name": "person_full_name", "name": "person_full_name",
			"anschrift": "street_address", "strasse": "street_address", "straße": "street_address",
			"stadt": "city", "ort": "city", "wohnort": "city",
			"bundesland": "state",
			"plz":        "postal_code", "postleitzahl": "postal_code",
			"land":       "country",
			"geschlecht": "gender", "anrede": "gender",
			"geburtsdatum": "birth_date", "geburtstag": "birth_date",
			"sozialversicherungsnummer": "ssn",
			"steuer_id":                 "national_id", "steuernummer": "national_id", "personalausweisnummer": "national_id", "reisepassnummer": "national_id",
			"kreditkartennummer": "credit_card",
			"passwort":           "secret", "kennwort": "secret",
		},
		"es": {
			"correo": "email", "correo_electronico": "email",
			"telefono": "phone_number", "teléfono": "phone_number", "movil": "phone_number", "celular": "phone_number",
			"nombre_usuario": "username", "usuario": "username",
			"nombre": "person_first_name", "primer_nombre": "person_first_name",
			"apellido": "person_last_name", "apellidos": "person_last_name",
			"nombre_completo": "person_full_name",
			"direccion":       "street_address", "dirección": "street_address", "domicilio": "street_address", "calle": "street_address",
			"ciudad": "city", "localidad": "city",
			"provincia":     "state",
			"codigo_postal": "postal_code",
			"pais":          "country", "país": "country",
			"sexo": "gender", "genero": "gender",
			"fecha_nacimiento": "birth_date", "fecha_de_nacimiento": "birth_date",
			"numero_seguridad_social": "ssn",
			"dni":                     "national_id", "nie": "national_id", "nif": "national_id", "pasaporte": "national_id",
			"numero_tarjeta": "credit_card", "tarjeta_credito": "credit_card",
			"contrasena": "secret", "contraseña": "secret", "clave_acceso": "secret",
		},
		"it": {
			"posta_elettronica": "email", "email": "email",
			"telefono": "phone_number", "cellulare": "phone_number",
			"nome_utente": "username",
			"cognome":     "person_last_name",
			"nome":        "person_full_name", "nome_completo": "person_full_name",
			"indirizzo": "street_address",
			"citta":     "city", "città": "city",
			"provincia": "state", "regione": "state",
			"cap": "postal_code", "codice_postale": "postal_code",
			"paese": "country", "nazione": "country",
			"sesso":        "gender",
			"data_nascita": "birth_date", "data_di_nascita": "birth_date",
			"codice_fiscale": "national_id", "passaporto": "national_id",
			"carta_di_credito": "credit_card", "numero_carta": "credit_card",
			"password": "secret", "parola_chiave": "secret", "parola_d_ordine": "secret",
		},
		"nl": {
			"e_mailadres": "email", "emailadres": "email",
			"telefoon": "phone_number", "telefoonnummer": "phone_number", "mobiel": "phone_number",
			"gebruikersnaam": "username",
			"voornaam":       "person_first_name",
			"achternaam":     "person_last_name", "familienaam": "person_last_name",
			"volledige_naam": "person_full_name", "naam": "person_full_name",
			"adres": "street_address", "straat": "street_address",
			"woonplaats": "city", "stad": "city", "plaats": "city",
			"provincie": "state",
			"postcode":  "postal_code",
			"land":      "country",
			"geslacht":  "gender", "aanhef": "gender",
			"geboortedatum": "birth_date",
			"bsn":           "national_id", "burgerservicenummer": "national_id", "paspoortnummer": "national_id",
			"creditcardnummer": "credit_card",
			"wachtwoord":       "secret",
		},
		"pl": {
			"poczta_elektroniczna": "email", "adres_email": "email",
			"telefon": "phone_number", "numer_telefonu": "phone_number", "komorka": "phone_number",
			"nazwa_uzytkownika": "username",
			"imie":              "person_first_name", "imię": "person_first_name",
			"nazwisko":        "person_last_name",
			"imie_i_nazwisko": "person_full_name",
			"adres":           "street_address", "ulica": "street_address",
			"miasto": "city", "miejscowosc": "city",
			"wojewodztwo":  "state",
			"kod_pocztowy": "postal_code",
			"kraj":         "country",
			"plec":         "gender", "płeć": "gender",
			"data_urodzenia": "birth_date",
			"pesel":          "national_id", "numer_paszportu": "national_id",
			"numer_karty": "credit_card",
			"haslo":       "secret", "hasło": "secret",
		},
		"pt": {
			"correio_eletronico": "email", "email": "email",
			"telefone": "phone_number", "telemovel": "phone_number", "celular": "phone_number",
			"nome_utilizador": "username", "nome_usuario": "username",
			"primeiro_nome": "person_first_name",
			"apelido":       "person_last_name", "sobrenome": "person_last_name",
			"nome_completo": "person_full_name", "nome": "person_full_name",
			"morada": "street_address", "endereco": "street_address", "endereço": "street_address", "rua": "street_address",
			"cidade": "city", "localidade": "city",
			"distrito":      "state",
			"codigo_postal": "postal_code", "cep": "postal_code",
			"pais": "country",
			"sexo": "gender", "genero": "gender",
			"data_nascimento": "birth_date", "data_de_nascimento": "birth_date",
			"cpf": "national_id", "nif": "national_id", "passaporte": "national_id", "numero_contribuinte": "national_id",
			"numero_cartao": "credit_card", "cartao_credito": "credit_card",
			"senha": "secret", "palavra_passe": "secret",
		},
	}
	for language, names := range languages {
		for name, want := range names {
			if got := classified(name); got != want {
				t.Errorf("%s: Classify(%q) = %q, want %q", language, name, got, want)
			}
		}
	}
}

// A name of a thing is not the name of a person, in any language.
func TestClassify_NamesOfThings(t *testing.T) {
	for _, name := range []string{
		"produktname", "dateiname", "name_produkt", "firmenname",
		"nombre_producto", "nombre_archivo", "nombre_empresa",
		"nome_prodotto", "nome_file", "nome_azienda",
		"productnaam", "naam_bestand", "bedrijfsnaam",
		"nazwa_produktu", "nazwa_pliku",
		"nome_produto", "nome_arquivo", "nome_ficheiro",
	} {
		if got := classified(name); got != "" {
			t.Errorf("Classify(%q) = %q, want none", name, got)
		}
	}
}

// What lets someone act as a person: passwords, tokens, keys, secrets. A hash, a digest or
// a salt of one is one too.
func TestClassify_Secrets(t *testing.T) {
	for _, name := range []string{
		"password", "passwd", "pwd", "user_password", "password_hash", "hashed_password", "password_digest",
		"password_salt", "passphrase", "mot_de_passe", "motdepasse", "mdp", "mot_de_passe_hash",
		"token", "api_token", "api_key", "apiKey", "access_token", "refresh_token", "auth_token", "session_token",
		"bearer_token", "secret", "client_secret", "secret_key", "private_key", "jeton", "pin_code", "security_code",
		"credentials", "otp_secret",
	} {
		if got := classified(name); got != "secret" {
			t.Errorf("Classify(%q) = %q, want secret", name, got)
		}
	}
	c, _ := Classify("password_hash", "text")
	if !c.Sensitive {
		t.Error("a secret is sensitive")
	}
	// The identifier of a key or of a token is a reference, not the key.
	for _, name := range []string{"api_key_id", "token_id", "secret_ref"} {
		if got := classified(name); got != "" {
			t.Errorf("Classify(%q) = %q, want none", name, got)
		}
	}
}

// An identifier an authority issues ends with "id" or "number" and is the datum itself.
func TestClassify_NationalIds(t *testing.T) {
	for name, want := range map[string]string{
		"tax_id": "national_id", "national_id": "national_id", "passport_number": "national_id",
		"passport": "national_id", "numero_passeport": "national_id", "social_security_number": "ssn",
		"ssn": "ssn", "nir": "ssn",
	} {
		if got := classified(name); got != want {
			t.Errorf("Classify(%q) = %q, want %q", name, got, want)
		}
	}
}
