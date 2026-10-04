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
	"fmt"
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

var (
	nonAlnum      = regexp.MustCompile(`[^a-z0-9]+`)
	numericTypeRe = regexp.MustCompile(`int|serial|numeric|decimal|number|float|double|real`)
	// timestamptz, datetime2, smalldatetime... sont couverts par les racines.
	temporalTypeRe = regexp.MustCompile(`date|timestamp|datetime`)
)

// normalize met le nom en minuscules et retire les séparateurs (garde a-z0-9).
func normalize(name string) string {
	return nonAlnum.ReplaceAllString(fold(name), "")
}

// tokenize cuts a name into lowercase tokens at separators, at case boundaries
// (camelCase) and between letters and digits: "customerEmail2" gives customer, email, 2.
func tokenize(name string) []string {
	var out []string
	var cur strings.Builder
	var prevLower, prevDigit bool
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range accents.Replace(name) {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower || prevDigit {
				flush()
			}
			cur.WriteRune(r - 'A' + 'a')
			prevLower, prevDigit = false, false
		case r >= 'a' && r <= 'z':
			if prevDigit {
				flush()
			}
			cur.WriteRune(r)
			prevLower, prevDigit = true, false
		case r >= '0' && r <= '9':
			if !prevDigit {
				flush()
			}
			cur.WriteRune(r)
			prevLower, prevDigit = false, true
		default:
			flush()
			prevLower, prevDigit = false, false
		}
	}
	flush()
	return out
}

func isNumericType(dataType string) bool {
	return numericTypeRe.MatchString(strings.ToLower(dataType))
}

// isTemporalType reconnaît les types date/heure natifs, par opposition à une date
// stockée dans une colonne texte.
func isTemporalType(dataType string) bool {
	return temporalTypeRe.MatchString(strings.ToLower(dataType))
}

// SuggestionForEntity maps a Presidio entity (content analysis) to a
// Classification (category, sensitivity, suggested transformer). ok is false when
// the entity has no suitable transformer. dataType selects the numeric flavor of
// the phone transformer.
func SuggestionForEntity(entity, dataType string) (Classification, bool) {
	switch strings.ToUpper(entity) {
	case "EMAIL_ADDRESS":
		return Classification{"email", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL}, true
	case "PHONE_NUMBER":
		src := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER
		if isNumericType(dataType) {
			src = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER
		}
		return Classification{"phone_number", true, src}, true
	case "PERSON":
		return Classification{"person_full_name", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_FULL_NAME}, true
	case "LOCATION", "LOCATION_CITY", "GPE":
		return Classification{"city", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CITY}, true
	case "CREDIT_CARD":
		return Classification{"credit_card", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER}, true
	case "IP_ADDRESS":
		return Classification{"ip_address", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS}, true
	case "US_SSN", "FR_NIR":
		return Classification{"nir", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN}, true
	case "FR_PHONE_NUMBER":
		src := mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER
		if isNumericType(dataType) {
			src = mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER
		}
		return Classification{"phone_number", true, src}, true
	case "FR_POSTAL_CODE":
		return Classification{"postal_code", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_ZIPCODE}, true
	case "IBAN_CODE":
		// Pas de générateur d'IBAN : on signale la sensibilité sans suggérer de
		// transformer, plutôt que d'en imposer un qui produirait un IBAN invalide.
		return Classification{"iban", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED}, true
	case "FR_SIRET":
		return Classification{"siret", true, mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED}, true
	// DATE_TIME est volontairement ABSENT : Presidio l'émet sur presque tout texte
	// contenant une date, y compris du texte libre truffé de PII. Comme l'entité
	// dominante est celle qui couvre le plus de valeurs, DATE_TIME supplanterait
	// PERSON sur une colonne de commentaires et la ferait passer pour non
	// sensible. Les dates sont traitées en amont, de façon déterministe, par
	// DetectDateFormat.
	default:
		return Classification{}, false
	}
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
		// La détection par nom est déterministe : elle ne dépend d'aucun modèle
		// et donne le même résultat à chaque introspection.
		col.PiiConfidence = mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED
		col.PiiDetectionMethod = mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_COLUMN_NAME
		col.PiiEvidence = fmt.Sprintf("reconnu par le nom de colonne « %s »", col.GetColumn())
	}
}
