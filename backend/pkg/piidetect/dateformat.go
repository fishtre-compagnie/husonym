// Inférence du format des dates stockées en TEXTE.
//
// A column typed DATE/TIMESTAMP has no format: the driver returns a normalized
// value, and "dd/mm/yyyy" is only a rendering. The question therefore only arises
// for dates stored in VARCHAR — a legacy case, but a common one.
//
// Deux usages, le second étant le plus important :
//  1. DÉTECTER : savoir qu'une colonne texte contient des dates.
//  2. RESTITUER : réécrire la date anonymisée DANS LE MÊME FORMAT. Si la source
//     contient "25/12/1980" et qu'on écrit "1985-03-14", l'application qui relit
//     la base cible ne parse plus rien. Le format inféré doit donc redescendre
//     jusqu'au transformer.
//
// Le raisonnement se fait au niveau COLONNE, pas valeur par valeur : une colonne
// est homogène. Un format n'est retenu que s'il explique TOUTES les valeurs.
package piidetect

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// DateFormatInfo décrit le format inféré pour une colonne de dates en texte.
type DateFormatInfo struct {
	// Layout est le format Go retenu (ex: "02/01/2006"). Vide si ambigu.
	Layout string
	// Ambiguous vaut true quand plusieurs formats expliquent toutes les valeurs
	// sans qu'aucune ne permette de trancher (ex: "03/04/1985" seul).
	Ambiguous bool
	// Candidates liste les formats encore possibles quand Ambiguous est true.
	Candidates []string
	// Evidence explique la décision en une phrase, pour l'infobulle.
	Evidence string
}

// dateCandidate associe un layout Go à son libellé lisible.
type dateCandidate struct {
	layout string
	label  string
}

// Les formats ambigus entre eux (jj/mm et mm/jj) sont volontairement voisins dans
// la liste : c'est leur cohabitation qui déclenche la levée de doute.
var dateCandidates = []dateCandidate{
	{"2006-01-02", "aaaa-mm-jj"},
	{"2006/01/02", "aaaa/mm/jj"},
	{"02/01/2006", "jj/mm/aaaa"},
	{"01/02/2006", "mm/jj/aaaa"},
	{"02-01-2006", "jj-mm-aaaa"},
	{"01-02-2006", "mm-jj-aaaa"},
	{"02.01.2006", "jj.mm.aaaa"},
	{"20060102", "aaaammjj"},
	{"2006-01-02 15:04:05", "aaaa-mm-jj hh:mm:ss"},
	{"02/01/2006 15:04", "jj/mm/aaaa hh:mm"},
	// Colonnes de type DATE/TIMESTAMP natif : le driver les décode en time.Time,
	// que la sérialisation rend sous ces deux formes. Sans elles, une colonne de
	// dates natives n'était pas reconnue par le scan de contenu.
	{"2006-01-02 15:04:05 -0700 MST", "date native (time.Time)"},
	{time.RFC3339, "aaaa-mm-jjThh:mm:ssZ"},
}

// labelFor retourne le libellé lisible d'un layout.
func labelFor(layout string) string {
	for _, c := range dateCandidates {
		if c.layout == layout {
			return c.label
		}
	}
	return layout
}

// frenchMonths maps French month names to their number. time.Parse only knows
// English months, so "25 décembre 1980" escapes it — we translate before parsing.
// Accented and unaccented spellings both appear in real databases.
var frenchMonths = map[string]string{
	"janvier": "01",
	"fevrier": "02", "février": "02",
	"mars":    "03",
	"avril":   "04",
	"mai":     "05",
	"juin":    "06",
	"juillet": "07",
	"aout":    "08", "août": "08",
	"septembre": "09",
	"octobre":   "10",
	"novembre":  "11",
	"decembre":  "12", "décembre": "12", //nolint:misspell // mois français, pas un mot anglais
}

var frenchTextDateRe = regexp.MustCompile(
	`^(\d{1,2})\s+([A-Za-zÀ-ÿ]+)\s+(\d{4})$`)

// normalizeFrenchTextDate turns "25 décembre 1980" into "25/12/1980". Values that
// do not have this shape are returned untouched, so the function can be applied
// unconditionally to every value.
func normalizeFrenchTextDate(v string) string {
	m := frenchTextDateRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return v
	}
	month, ok := frenchMonths[strings.ToLower(m[2])]
	if !ok {
		return v
	}
	return fmt.Sprintf("%02s/%s/%s", m[1], month, m[3])
}

// DetectDateFormat infère le format des valeurs texte d'une colonne.
//
// ok is false when the values do not look like dates. When several formats remain
// possible we do NOT pick one: Ambiguous is raised and the caller must ask the user
// (or apply the connection locale).
func DetectDateFormat(values []string) (DateFormatInfo, bool) {
	clean := make([]string, 0, len(values))
	frenchText := 0
	for _, v := range values {
		t := strings.TrimSpace(v)
		if t == "" {
			continue
		}
		if n := normalizeFrenchTextDate(t); n != t {
			frenchText++
			t = n
		}
		clean = append(clean, t)
	}
	if len(clean) < minSamples {
		return DateFormatInfo{}, false
	}

	// Mois écrit en lettres : format non ambigu par construction (le mois est
	// nommé, il n'y a rien à trancher entre jj/mm et mm/jj).
	if frenchText == len(clean) {
		return DateFormatInfo{
			Layout: "2 January 2006",
			Evidence: fmt.Sprintf(
				"format « jj mois aaaa » (mois en lettres) prouvé sur %d valeurs", len(clean)),
		}, true
	}

	// Un format est retenu seulement s'il explique TOUTES les valeurs. Le parsing
	// fait lui-même une partie de la désambiguïsation : "25/12/1980" est rejeté
	// par le layout mm/jj (mois 25 invalide).
	var survivors []string
	for _, c := range dateCandidates {
		allOk := true
		for _, v := range clean {
			if _, err := time.Parse(c.layout, v); err != nil {
				allOk = false
				break
			}
		}
		if allOk {
			survivors = append(survivors, c.layout)
		}
	}

	switch len(survivors) {
	case 0:
		return DateFormatInfo{}, false
	case 1:
		return DateFormatInfo{
			Layout: survivors[0],
			Evidence: fmt.Sprintf("format %s prouvé sur %d valeurs",
				labelFor(survivors[0]), len(clean)),
		}, true
	}

	// Plusieurs formats survivent. Cas typique : toutes les valeurs ont jour ET
	// mois <= 12, donc jj/mm et mm/jj expliquent tout. Indécidable par les
	// données : on remonte l'ambiguïté au lieu de deviner.
	labels := make([]string, 0, len(survivors))
	for _, s := range survivors {
		labels = append(labels, labelFor(s))
	}
	return DateFormatInfo{
		Ambiguous:  true,
		Candidates: survivors,
		Evidence: fmt.Sprintf("format ambigu sur %d valeurs : %s — aucune valeur ne permet de trancher",
			len(clean), strings.Join(labels, " ou ")),
	}, true
}

// birthDateHints : un nom de colonne qui, combiné à des valeurs de type date,
// caractérise une date de naissance — donnée personnelle au sens du RGPD. Une
// date seule ne l'est pas (created_at, updated_at...), d'où ce filtre par le nom.
var birthDateHints = []string{
	"birthdate", "birthday", "dateofbirth", "datenaissance", "datedenaissance",
	"naissance",
}

// birthDateTokens : indices trop courts pour une recherche en sous-chaîne
// ("nee" est dans "annee" et "donnee") ; ils ne comptent que comme token entier.
var birthDateTokens = []string{"dob", "ddn", "nele", "nee"}

// IsBirthDateName indique si le nom de colonne désigne une date de naissance.
func IsBirthDateName(columnName string) bool {
	norm := normalize(columnName)
	if norm == "" {
		return false
	}
	for _, h := range birthDateHints {
		if strings.Contains(norm, h) {
			return true
		}
	}
	tokens := tokenize(columnName)
	for _, h := range birthDateTokens {
		if slices.Contains(tokens, h) {
			return true
		}
	}
	return false
}
