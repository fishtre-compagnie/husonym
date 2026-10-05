package piidetect

import "testing"

func TestClassify_AgeOfAPerson(t *testing.T) {
	expectCategories(t, map[string]string{
		"age": "age", "user_age": "age", "customer_age": "age", "patient_age": "age", "age_years": "age",
		"edad": "age", "edad_cliente": "age",
	})
	expectNone(t, "age_group", "age_range", "customer_age_group", "min_age", "age_limit", "page", "eta_delivery")
	// "age" ends ordinary words: it is not read inside a name written without separators.
	expectNone(t, "paysage", "adressage", "postage", "userage")
}

func TestClassify_BirthOfAPerson(t *testing.T) {
	expectCategories(t, map[string]string{
		"date_birth": "birth_date", "birth_year": "birth_date", "birth_place": "birth_date",
		"place_of_birth": "birth_date", "year_of_birth": "birth_date", "birthplace": "birth_date",
		"birthyear": "birth_date", "yearofbirth": "birth_date", "born_on": "birth_date",
	})
	expectNone(t, "birth_certificate_url", "birth_date_format")
}

func TestClassify_CardsAndAccounts(t *testing.T) {
	expectCategories(t, map[string]string{
		"cvv": "secret", "card_cvv": "secret", "cvc": "secret", "cardcvv": "secret",
		"cardholder": "person_full_name", "card_holder": "person_full_name", "cardholder_name": "person_full_name",
		"account_holder": "person_full_name", "kontoinhaber": "person_full_name", "accountholder": "person_full_name",
		"card_expiry": "credit_card", "card_last4": "credit_card", "cardexpiry": "credit_card",
		"iban_code": "iban", "ibancode": "iban",
	})
}

func TestClassify_WhoSomeoneIs(t *testing.T) {
	expectCategories(t, map[string]string{
		"nationality": "country", "citizenship": "country", "nationalite": "country", "nacionalidad": "country",
		"staatsangehoerigkeit": "country",
		"marital_status":       "marital_status", "estado_civil": "marital_status", "maritalstatus": "marital_status",
		"etat_civil": "marital_status", "familienstand": "marital_status", "estadocivil": "marital_status",
		"income": "salary", "annual_income": "salary", "annualincome": "salary", "revenu": "salary",
		"emergency_contact": "person_full_name", "emergencycontact": "person_full_name",
		"tussenvoegsel": "person_last_name", "maiden_name": "person_last_name",
		"nom_jeune_fille": "person_last_name", "nomjeunefille": "person_last_name",
		"father_name": "person_full_name", "fathername": "person_full_name", "mothername": "person_full_name",
		"ethnic_group": "ethnicity", "ethnicgroup": "ethnicity",
		"house_number": "street_address", "housenumber": "street_address",
		"user_names": "username",
	})
}

func TestClassify_SecretsSomeoneTypes(t *testing.T) {
	expectCategories(t, map[string]string{
		"otp": "secret", "otp_code": "secret", "otpcode": "secret", "totp": "secret", "passcode": "secret",
		"security_question": "secret", "securityquestion": "secret", "resettoken": "secret",
	})
}

func TestClassify_IdentifiersOfAPerson(t *testing.T) {
	expectCategories(t, map[string]string{
		"numero_documento": "national_id", "documento": "national_id", "numerodocumento": "national_id",
		"nro_documento": "national_id", "documento_identidad": "national_id",
		"rut": "national_id", "rut_cliente": "national_id",
		"id_fiscal": "national_id", "idfiscal": "national_id", "id_number": "national_id", "idnumber": "national_id",
		"id_tax": "national_id", "idnif": "national_id", "id_national": "national_id",
		"insee": "ssn", "numero_insee": "ssn",
		"mac_address": "mac_address", "MACAddress": "mac_address", "macaddress": "mac_address", "mac_addr": "mac_address",
	})
	expectNone(t, "code_insee", "documento_tipo", "tipo_documento_id", "rut_file", "mac_os")
	c, ok := Classify("mac_address", "text")
	if !ok || !c.Sensitive || c.Suggested != scrambleText {
		t.Errorf("mac_address: %+v", c)
	}
}
