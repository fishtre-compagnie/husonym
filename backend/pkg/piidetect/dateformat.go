// Inférence du format des dates stockées en TEXTE.
//
// Une colonne typée DATE/TIMESTAMP n'a pas de format : le driver renvoie une
// valeur normalisée, "jj/mm/aaaa" n'est qu'un affichage. Le problème ne se pose
// donc que pour les dates stockées en VARCHAR — cas hérité mais fréquent.
//
// Deux usages, le second étant le plus important :
//   1. DÉTECTER : savoir qu'une colonne texte contient des dates.
//   2. RESTITUER : réécrire la date anonymisée DANS LE MÊME FORMAT. Si la source
//      contient "25/12/1980" et qu'on écrit "1985-03-14", l'application qui relit
//      la base cible ne parse plus rien. Le format inféré doit donc redescendre
//      jusqu'au transformer.
//
// Le raisonnement se fait au niveau COLONNE, pas valeur par valeur : une colonne
// est homogène. Un format n'est retenu que s'il explique TOUTES les valeurs.
package piidetect

import (
	"fmt"
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

// DetectDateFormat infère le format des valeurs texte d'une colonne.
//
// ok vaut false si les valeurs ne ressemblent pas à des dates. Quand plusieurs
// formats restent possibles, on ne tranche PAS : Ambiguous est levé et l'appelant
// doit demander à l'utilisateur (ou appliquer la locale de la connexion).
func DetectDateFormat(values []string) (DateFormatInfo, bool) {
	clean := make([]string, 0, len(values))
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) < minSamples {
		return DateFormatInfo{}, false
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
	"naissance", "dob", "ddn", "nele", "nee",
}

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
	return false
}
