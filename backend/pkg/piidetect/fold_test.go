package piidetect

import "testing"

func TestClassify_UpperCaseNamesWithMarks(t *testing.T) {
	expectCategories(t, map[string]string{
		"CONTRASEÑA":       "secret",
		"DIRECCIÓN":        "street_address",
		"CIVILITÉ":         "gender",
		"SÉCURITÉ_SOCIALE": "ssn",
		"NUMÉRO_SÉCU":      "ssn",
		"HASŁO":            "secret",
		"CITTÀ":            "city",
		"ENDEREÇO":         "street_address",
		"PRÉNOM":           "person_first_name",
		"Prénom":           "person_first_name",
		"prénomClient":     "person_first_name",
		"STRASSE":          "street_address",
		"Straße":           "street_address",
		"TÉLÉPHONE":        "phone_number",
	})
}

func TestTokenize_FoldsMarksInEitherCase(t *testing.T) {
	for name, want := range map[string]string{
		"PRÉNOM":           "prenom",
		"ÉtatCivil":        "etat civil",
		"Łódź_ŻÓŁĆ":        "lodz zolc",
		"Ærø_ŒUVRE":        "aero oeuvre",
		"prénom":          "prenom",
		"GRÖSSE_größe":     "grosse grosse",
		"İstanbul_ıslak":   "istanbul islak",
		"Đorđe":            "dorde",
		"naïveCafé2Ñandú":  "naive cafe 2 nandu",
		"SÉCURITÉ_SOCIALE": "securite sociale",
	} {
		got := ""
		for i, token := range tokenize(name) {
			if i > 0 {
				got += " "
			}
			got += token
		}
		if got != want {
			t.Errorf("tokenize(%q) = %q, want %q", name, got, want)
		}
		if folded := normalize(name); folded != removeSpaces(want) {
			t.Errorf("normalize(%q) = %q, want %q", name, folded, removeSpaces(want))
		}
	}
}

func removeSpaces(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r != ' ' {
			out = append(out, r)
		}
	}
	return string(out)
}
