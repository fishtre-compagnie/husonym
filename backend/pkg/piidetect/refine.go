// Affinage des catégories que Presidio ne sait pas distinguer.
//
// Presidio raisonne par ENTITÉ, avec un vocabulaire volontairement large :
// `PERSON` couvre indifféremment un prénom, un nom de famille et un nom complet ;
// `LOCATION` couvre une ville, un pays et une adresse postale. Notre produit a
// besoin d'être plus précis, parce que chaque cas appelle un transformer
// différent — Generate First Name ne remplace pas Generate Full Address.
//
// Ce fichier tranche à partir de la FORME des valeurs, ce qui reste déterministe
// et local. Mesuré au banc (scripts/testdata/bench-presidio.py), trois colonnes
// étaient mal classées faute de cet étage : `adresse` ressortait en « ville »,
// `prenom` et `nom` en « nom complet ».
package piidetect

import (
	"regexp"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

var (
	// Une adresse postale française commence typiquement par un numéro et
	// comporte un type de voie. « Bordeaux » n'a ni l'un ni l'autre.
	streetTypeRe = regexp.MustCompile(
		`(?i)\b(rue|avenue|av|boulevard|bd|place|cours|chemin|impasse|all[ée]e|route|quai|square|villa|passage|sentier|esplanade|faubourg|promenade)\b`)
	leadingNumberRe = regexp.MustCompile(`^\s*\d+\s*(bis|ter|quater)?\s*,?\s+\S`)
	whitespaceRe    = regexp.MustCompile(`\s+`)
)

// RefinePersonCategory précise une détection PERSON à partir de la forme des
// valeurs : un seul mot ne peut pas être un nom complet.
//
// Limite assumée : sur un mot unique, prénom et nom de famille sont
// indistinguables sans dictionnaire de prénoms (piste des listes INSEE). On
// retient person_first_name, dont le transformer produit un mot plausible dans
// les deux cas — l'anonymisation reste correcte même si l'étiquette est
// imparfaite. La détection par NOM de colonne, elle, tranche sans ambiguïté et
// reste prioritaire.
func RefinePersonCategory(values []string) (string, mgmtv1alpha1.TransformerSource) {
	multi, single := 0, 0
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if len(whitespaceRe.Split(v, -1)) >= 2 {
			multi++
		} else {
			single++
		}
	}
	// Majorité de valeurs en plusieurs mots : nom complet.
	if multi > single {
		return "person_full_name",
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME
	}
	if single == 0 {
		return "person_full_name",
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME
	}
	return "person_first_name",
		mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FIRST_NAME
}

// RefineLocationCategory distingue une adresse postale d'un nom de ville.
// Presidio émet LOCATION pour les deux, et tout mapper sur « ville » faisait
// suggérer Generate City sur une colonne d'adresses.
func RefineLocationCategory(values []string) (string, mgmtv1alpha1.TransformerSource) {
	addressLike, total := 0, 0
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		total++
		// Les deux marqueurs ensemble : « 12 rue Sainte-Catherine » est une
		// adresse, « Rue de la Paix » (sans numéro) reste ambigu et « Bordeaux »
		// est une ville. Exiger les deux évite de traiter une commune dont le nom
		// contient un mot de voie comme une adresse.
		if leadingNumberRe.MatchString(v) && streetTypeRe.MatchString(v) {
			addressLike++
		}
	}
	if total > 0 && addressLike*2 > total {
		return "street_address",
			mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS
	}
	return "city", mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY
}

// RefineByValues applique l'affinage adapté à la catégorie, s'il en existe un.
// Les autres catégories sont retournées inchangées.
func RefineByValues(
	category string,
	suggested mgmtv1alpha1.TransformerSource,
	values []string,
) (string, mgmtv1alpha1.TransformerSource) {
	switch category {
	case "person_full_name", "person_first_name", "person_last_name":
		return RefinePersonCategory(values)
	case "city":
		return RefineLocationCategory(values)
	default:
		return category, suggested
	}
}
