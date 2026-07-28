// Package piidetect fournit une détection heuristique de la nature d'une colonne
// (email, téléphone, nom, adresse, etc.) à partir de son nom et de son type SQL,
// afin de signaler les données à caractère personnel (RGPD) et de proposer un
// transformer d'anonymisation adapté.
//
// Il s'agit d'une détection SANS lecture de données : uniquement le nom de la
// colonne et son type. Elle couvre la majorité des cas réels ; un second niveau
// basé sur l'échantillonnage de contenu (Presidio) pourra la compléter plus tard.
//
// Le vocabulaire de catégories est aligné sur les domaines sémantiques du moteur
// Athanor (voir worker/pkg/athanor/runner/deterministic.go) de sorte qu'une
// suggestion hérite naturellement de la cohérence déterministe.
package piidetect

import (
	"regexp"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Classification est le résultat de la détection pour une colonne.
type Classification struct {
	// Category est la nature sémantique détectée (ex: "email", "phone_number").
	Category string
	// Sensitive indique une donnée à caractère personnel (RGPD).
	Sensitive bool
	// Suggested est le transformer recommandé pour anonymiser la colonne.
	Suggested mgmtv1alpha1.TransformerSource
}

// rule décrit une règle de détection par mots-clés sur le nom de la colonne.
type rule struct {
	category  string
	sensitive bool
	suggested mgmtv1alpha1.TransformerSource
	// keywords recherchés en sous-chaîne sur le nom normalisé.
	keywords []string
	// tokenOnly : mots-clés recherchés UNIQUEMENT comme token entier (évite les
	// faux positifs des mots courts, ex. "nom" dans "prenom", "tel" dans "hotel").
	tokenOnly []string
	// suggestIfNumeric : si non nul, transformer alternatif quand le type SQL est
	// numérique (ex. téléphone stocké en entier).
	suggestIfNumeric mgmtv1alpha1.TransformerSource
}

// L'ordre est significatif : première règle qui matche = gagnante. Les règles les
// plus spécifiques (username, prénom) précèdent les plus génériques (nom, name).
var rules = []rule{
	{
		category:  "email",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL,
		keywords:  []string{"email", "mail", "courriel"},
	},
	{
		category:         "phone_number",
		sensitive:        true,
		suggested:        mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER,
		suggestIfNumeric: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER,
		keywords:         []string{"phone", "telephone", "mobile", "cellphone"},
		tokenOnly:        []string{"tel", "gsm", "fax"},
	},
	{
		category:  "username",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_USERNAME,
		keywords:  []string{"username", "login"},
		tokenOnly: []string{"user", "pseudo"},
	},
	{
		category:  "person_first_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FIRST_NAME,
		keywords:  []string{"firstname", "givenname", "forename", "prenom"},
		tokenOnly: []string{"fname"},
	},
	{
		category:  "person_last_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_LAST_NAME,
		keywords:  []string{"lastname", "surname", "familyname", "patronyme"},
		tokenOnly: []string{"lname", "nom"},
	},
	{
		category:  "person_full_name",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME,
		keywords:  []string{"fullname", "nomcomplet"},
		tokenOnly: []string{"name"},
	},
	{
		// Avant street_address : "ip_address" contient la sous-chaîne "address".
		category:  "ip_address",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS,
		keywords:  []string{"ipaddress", "ipaddr"},
		tokenOnly: []string{"ip"},
	},
	{
		category:  "street_address",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_ADDRESS,
		keywords:  []string{"address", "adresse", "street"},
		tokenOnly: []string{"rue"},
	},
	{
		// Champs géo rapportés à une personne = donnée personnelle (RGPD).
		category:  "city",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY,
		keywords:  []string{"city", "ville"},
	},
	{
		category:  "state",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STATE,
		keywords:  []string{"province"},
		tokenOnly: []string{"state", "region"},
	},
	{
		category:  "postal_code",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE,
		keywords:  []string{"zipcode", "zip", "postal", "postcode"},
		tokenOnly: []string{"cp"},
	},
	{
		category:  "country",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_COUNTRY,
		keywords:  []string{"country", "pays"},
	},
	{
		category:  "ssn",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN,
		keywords:  []string{"ssn", "socialsecurity", "securitesociale"},
		tokenOnly: []string{"nir"},
	},
	{
		category:  "credit_card",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER,
		keywords:  []string{"cardnumber", "creditcard", "ccnumber", "cardno"},
	},
	{
		category:  "gender",
		sensitive: true,
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_GENDER,
		keywords:  []string{"gender", "sexe", "genre"},
	},
}

var (
	nonAlnum      = regexp.MustCompile(`[^a-z0-9]+`)
	numericTypeRe = regexp.MustCompile(`int|serial|numeric|decimal|number|float|double|real`)
)

// normalize met le nom en minuscules et retire les séparateurs (garde a-z0-9).
func normalize(name string) string {
	return nonAlnum.ReplaceAllString(strings.ToLower(name), "")
}

// tokenize découpe le nom en tokens sur les séparateurs et les frontières de casse
// (camelCase). Ex: "customerEmail_2" -> ["customer","email","2"].
func tokenize(name string) []string {
	var out []string
	var cur strings.Builder
	var prevLower bool
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower && cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			cur.WriteRune(r - 'A' + 'a')
			prevLower = false
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			cur.WriteRune(r)
			prevLower = r >= 'a' && r <= 'z'
		default:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			prevLower = false
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func isNumericType(dataType string) bool {
	return numericTypeRe.MatchString(strings.ToLower(dataType))
}

// Classify retourne la classification d'une colonne à partir de son nom et de son
// type SQL. ok vaut false si aucune règle ne matche.
func Classify(columnName, dataType string) (Classification, bool) {
	norm := normalize(columnName)
	if norm == "" {
		return Classification{}, false
	}
	tokens := tokenize(columnName)
	tokenSet := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		tokenSet[t] = struct{}{}
	}

	for _, ru := range rules {
		matched := false
		for _, kw := range ru.keywords {
			if strings.Contains(norm, kw) {
				matched = true
				break
			}
		}
		if !matched {
			for _, kw := range ru.tokenOnly {
				if _, ok := tokenSet[kw]; ok {
					matched = true
					break
				}
			}
		}
		if !matched {
			continue
		}

		suggested := ru.suggested
		if ru.suggestIfNumeric != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED &&
			isNumericType(dataType) {
			suggested = ru.suggestIfNumeric
		}
		return Classification{
			Category:  ru.category,
			Sensitive: ru.sensitive,
			Suggested: suggested,
		}, true
	}
	return Classification{}, false
}

// Enrich annote chaque colonne avec sa catégorie détectée, son flag RGPD et le
// transformer suggéré. Les colonnes non reconnues restent inchangées.
func Enrich(columns []*mgmtv1alpha1.DatabaseColumn) {
	for _, col := range columns {
		if col == nil {
			continue
		}
		c, ok := Classify(col.GetColumn(), col.GetDataType())
		if !ok {
			continue
		}
		col.DataCategory = c.Category
		col.IsSensitive = c.Sensitive
		col.SuggestedTransformerSource = c.Suggested
	}
}
