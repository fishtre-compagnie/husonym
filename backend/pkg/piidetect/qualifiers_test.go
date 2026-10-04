package piidetect

import "testing"

// A name that qualifies a datum without being one is not a finding: its format, its
// type, how many there are, whether it was verified. In every language of the rules.
func TestClassify_QualifierNames(t *testing.T) {
	languages := map[string][]string{
		"en": {
			"email_format", "phone_type", "address_count", "is_email_verified", "phone_model", "address_type",
			"name_format", "city_code", "country_code", "ip_version", "gender_ratio", "email_enabled",
			"email_status", "name_length", "has_phone", "email_verified", "phone_confirmed", "password_format",
			"password_length", "token_type", "is_password_set", "count_emails", "format_of_address", "type_of_phone",
			"email_template", "email_optin", "email_consent", "password_status", "username_valid",
		},
		"fr": {
			"format_email", "type_telephone", "nb_adresses", "email_verifie", "modele_telephone", "type_adresse",
			"code_pays", "version_ip", "statut_email", "longueur_nom", "telephone_valide", "email_confirmé",
			"format_mot_de_passe", "email_actif",
		},
		"de": {
			"email_format", "telefon_typ", "anzahl_adressen", "ist_email_bestaetigt", "telefon_modell",
			"adress_typ", "laendercode", "land_code", "email_status", "namen_laenge", "email_verifiziert",
			"passwort_format", "email_aktiv",
		},
		"es": {
			"formato_correo", "tipo_telefono", "cantidad_direcciones", "correo_verificado", "modelo_telefono",
			"tipo_direccion", "codigo_pais", "correo_estado", "longitud_nombre", "telefono_confirmado",
			"formato_contrasena", "correo_activo", "direccion_verificada",
		},
		"it": {
			"formato_email", "tipo_telefono", "conteggio_indirizzi", "email_verificata", "modello_telefono",
			"tipo_indirizzo", "codice_paese", "stato_email", "lunghezza_nome", "telefono_confermato",
			"formato_password", "email_attiva",
		},
		"nl": {
			"email_formaat", "telefoon_type", "aantal_adressen", "email_geverifieerd", "telefoon_model",
			"adres_type", "land_code", "email_status", "naam_lengte", "telefoon_bevestigd",
			"wachtwoord_formaat", "email_actief",
		},
		"pl": {
			"format_email", "typ_telefonu", "liczba_adresow", "email_zweryfikowany", "model_telefonu",
			"typ_adresu", "kod_kraju", "status_email", "dlugosc_imienia", "telefon_potwierdzony",
			"format_hasla", "email_aktywny", "czy_email_potwierdzony",
		},
		"pt": {
			"formato_email", "tipo_telefone", "quantidade_enderecos", "email_verificado", "modelo_telefone",
			"tipo_endereco", "codigo_pais", "estado_email", "comprimento_nome", "telefone_confirmado",
			"formato_senha", "email_ativo", "morada_verificada",
		},
	}
	for language, names := range languages {
		for _, name := range names {
			if got := classified(name); got != "" {
				t.Errorf("%s: Classify(%q) = %q, want none", language, name, got)
			}
		}
	}
}

// A word that qualifies elsewhere is part of the datum in these names: a postal code, a
// security code, an identifier issued by an authority. And an adjective before the noun
// says which datum, not something about it. When in doubt a name is a finding.
func TestClassify_NamesThatLookLikeQualifiers(t *testing.T) {
	for name, want := range map[string]string{
		"postal_code": "postal_code", "zip_code": "postal_code", "code_postal": "postal_code",
		"codigo_postal": "postal_code", "kod_pocztowy": "postal_code", "codice_postale": "postal_code",
		"pin_code": "secret", "security_code": "secret", "code_secret": "secret",
		"codice_fiscale": "national_id",
		"verified_email": "email", "confirmed_phone": "phone_number", "active_email": "email",
		"password_hash": "secret", "password_salt": "secret", "password_digest": "secret",
		"email_address": "email", "phone_number": "phone_number", "email_domain": "email",
		"status": "", "type": "", "format": "",
	} {
		if got := classified(name); got != want {
			t.Errorf("Classify(%q) = %q, want %q", name, got, want)
		}
	}
}
