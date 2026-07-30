package piidetect

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

func TestDetectDateFormat_FormatsTranchables(t *testing.T) {
	cases := []struct {
		name       string
		values     []string
		wantLayout string
	}{
		{
			// Des jours > 12 éliminent mm/jj : le format est prouvé par les données.
			name:       "jj/mm/aaaa prouve par un jour superieur a 12",
			values:     []string{"25/12/1980", "08/03/1975", "14/07/1992", "30/01/1988"},
			wantLayout: "02/01/2006",
		},
		{
			// Symétrique : des valeurs dont le 2e groupe dépasse 12 imposent mm/jj.
			name:       "mm/jj/aaaa prouve par un jour en 2e position",
			values:     []string{"12/25/1980", "03/08/1975", "07/14/1992", "01/30/1988"},
			wantLayout: "01/02/2006",
		},
		{
			name:       "iso non ambigu",
			values:     []string{"1980-12-25", "1975-03-08", "1992-07-14"},
			wantLayout: "2006-01-02",
		},
		{
			name:       "compact",
			values:     []string{"19801225", "19750308", "19920714"},
			wantLayout: "20060102",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DetectDateFormat(tc.values)
			if !ok {
				t.Fatal("DetectDateFormat = non détecté")
			}
			if got.Ambiguous {
				t.Errorf("Ambiguous = true, attendu false (%s)", got.Evidence)
			}
			if got.Layout != tc.wantLayout {
				t.Errorf("Layout = %q, attendu %q", got.Layout, tc.wantLayout)
			}
			if got.Evidence == "" {
				t.Error("Evidence vide")
			}
		})
	}
}

// Le cas qui justifie le badge orange : jour ET mois <= 12 partout, donc jj/mm et
// mm/jj expliquent aussi bien les données. On ne doit PAS trancher au hasard.
func TestDetectDateFormat_AmbiguiteRemonteeSansDeviner(t *testing.T) {
	values := []string{"03/04/1985", "05/06/1990", "07/09/1995", "01/08/1977"}
	got, ok := DetectDateFormat(values)
	if !ok {
		t.Fatal("DetectDateFormat = non détecté")
	}
	if !got.Ambiguous {
		t.Fatalf("Ambiguous = false, attendu true : %q a été choisi alors que rien ne tranche", got.Layout)
	}
	if got.Layout != "" {
		t.Errorf("Layout = %q, attendu vide quand c'est ambigu", got.Layout)
	}
	if len(got.Candidates) < 2 {
		t.Errorf("Candidates = %v, attendu au moins 2 formats possibles", got.Candidates)
	}
}

func TestDetectDateFormat_NonDates(t *testing.T) {
	cases := [][]string{
		{"alpha", "beta", "gamma"},
		{"jean@example.fr", "marie@example.fr", "luc@example.fr"},
		{"25 decembre 1980", "8 mars 1975", "14 juillet 1992"}, // mois en lettres : hors périmètre
		{"25/12/1980"}, // sous minSamples
		{},
	}
	for _, values := range cases {
		if got, ok := DetectDateFormat(values); ok {
			t.Errorf("DetectDateFormat(%v) = %+v, attendu aucune détection", values, got)
		}
	}
}

// Une colonne de dates n'est pas personnelle en soi : seul le nom la qualifie.
func TestIsBirthDateName(t *testing.T) {
	for _, n := range []string{
		"date_naissance", "dateNaissance", "birthdate", "birth_date",
		"date_of_birth", "dob", "ddn", "DATE_DE_NAISSANCE",
	} {
		if !IsBirthDateName(n) {
			t.Errorf("IsBirthDateName(%q) = false, attendu true", n)
		}
	}
	for _, n := range []string{"created_at", "updated_at", "date_commande", "invoice_date", ""} {
		if IsBirthDateName(n) {
			t.Errorf("IsBirthDateName(%q) = true, attendu false", n)
		}
	}
}

// Une date de naissance en type natif peut recevoir un générateur de timestamp :
// il n'y a pas de format textuel à préserver. En colonne texte, non.
func TestClassify_DateNaissanceSuggereSelonLeType(t *testing.T) {
	natifs := []string{"date", "timestamp", "timestamp with time zone", "datetime", "datetime2"}
	for _, dt := range natifs {
		got, ok := Classify("date_naissance", dt)
		if !ok {
			t.Errorf("Classify(date_naissance, %q) = non détecté", dt)
			continue
		}
		if got.Suggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_UTCTIMESTAMP {
			t.Errorf("type %q : transformer = %v, attendu GENERATE_UTCTIMESTAMP", dt, got.Suggested)
		}
	}
	textes := []string{"varchar(100)", "text", "char(10)"}
	for _, dt := range textes {
		got, ok := Classify("date_naissance_txt_fr", dt)
		if !ok {
			t.Errorf("Classify(..., %q) = non détecté", dt)
			continue
		}
		if got.Suggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED {
			t.Errorf("type %q : transformer = %v, attendu UNSPECIFIED (format à préserver)", dt, got.Suggested)
		}
	}
}
