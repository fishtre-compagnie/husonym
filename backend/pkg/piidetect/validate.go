// Validation déterministe du CONTENU d'une colonne.
//
// Complément de la détection par nom (Classify) et alternative fiable à Presidio :
// ici on ne devine pas, on VÉRIFIE. Une valeur est acceptée seulement si sa clé de
// contrôle tombe juste (NIR mod 97, IBAN mod 97, SIRET/carte Luhn) ou si sa forme
// est strictement contrainte (email, IP, téléphone FR).
//
// Pourquoi cet étage existe : Presidio classe le NIR "180017511600146" en
// CREDIT_CARD avec un score de 1.00, parce que ce NIR passe Luhn par hasard. Un
// score maximal sur la mauvaise catégorie ne se corrige pas en ajustant un seuil ;
// il faut une preuve plus spécifique. C'est ce que fait ce fichier, et c'est
// pourquoi les validateurs sont ordonnés par SPÉCIFICITÉ décroissante et non par
// score : le NIR (structure sexe/année/mois/département + mod 97) est plus
// contraint qu'un simple Luhn, il est donc évalué avant la carte bancaire.
package piidetect

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

const (
	// Une colonne est homogène : on exige que la quasi-totalité des valeurs
	// non vides valide pour conclure. Quelques valeurs sales (saisie libre,
	// placeholders) ne doivent pas faire échouer la détection.
	confirmRatio = 0.9
	// En dessous de confirmRatio mais au-dessus de ce seuil, il y a un signal
	// mais pas une preuve : on alerte sans rien appliquer.
	reviewRatio = 0.5
	// Sous ce nombre de valeurs analysées, un ratio n'a pas de sens statistique.
	minSamples = 3
)

// ContentClassification est le résultat de l'analyse du contenu d'une colonne.
type ContentClassification struct {
	Category   string
	Sensitive  bool
	Suggested  mgmtv1alpha1.TransformerSource
	Confidence mgmtv1alpha1.PiiConfidence
	Method     mgmtv1alpha1.PiiDetectionMethod
	// Evidence explique la décision en une phrase, pour l'infobulle du badge.
	Evidence string
}

// validator décrit un contrôle déterministe applicable à une valeur.
type validator struct {
	category  string
	label     string // libellé lisible utilisé dans Evidence
	suggested mgmtv1alpha1.TransformerSource
	// numericSuggested : variante quand la colonne est de type numérique.
	numericSuggested mgmtv1alpha1.TransformerSource
	// weak : forme trop peu contrainte pour conclure seule (ex: code postal =
	// n'importe quel entier à 5 chiffres). Plafonné à NEEDS_REVIEW.
	weak bool
	fn   func(string) bool
}

// L'ORDRE EST SIGNIFIANT : du plus contraint au moins contraint. Le premier
// validateur qui atteint le seuil de confirmation gagne, ce qui évite qu'un
// contrôle générique (Luhn) ne rafle une valeur qu'un contrôle spécifique
// (NIR mod 97) revendique légitimement.
var validators = []validator{
	{
		category:  "email",
		label:     "adresse e-mail",
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_EMAIL,
		fn:        isEmail,
	},
	{
		category:  "iban",
		label:     "IBAN (clé mod 97)",
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
		fn:        IsIBAN,
	},
	{
		// Avant la carte bancaire : un NIR de 15 chiffres peut passer Luhn par
		// hasard, l'inverse (une carte respectant la structure NIR + mod 97)
		// est bien plus improbable.
		category:  "nir",
		label:     "NIR (clé mod 97)",
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_SSN,
		fn:        IsNIR,
	},
	{
		category:  "siret",
		label:     "SIRET/SIREN (Luhn)",
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED,
		fn:        IsSiretOrSiren,
	},
	{
		category:  "credit_card",
		label:     "carte bancaire (Luhn)",
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_CARD_NUMBER,
		fn:        IsCreditCard,
	},
	{
		category:  "ip_address",
		label:     "adresse IP",
		suggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_IP_ADDRESS,
		fn:        isIP,
	},
	{
		category:         "phone_number",
		label:            "téléphone français",
		suggested:        mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_STRING_PHONE_NUMBER,
		numericSuggested: mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_GENERATE_INT64_PHONE_NUMBER,
		fn:               IsFrenchPhone,
	},
	// Pas de validateur de CODE POSTAL ici, volontairement : « 5 chiffres dont un
	// département 01-98 » décrit aussi un salaire (28000), un identifiant ou une
	// quantité. Mesuré sur le jeu de test, ce contrôle classait « salaire_annuel »
	// en code postal. Un signal qui se déclenche sur des colonnes sans rapport
	// dégrade la confiance dans tous les autres. Les codes postaux restent très
	// bien détectés par le NOM de colonne (cf. rules dans piidetect.go), et
	// IsFrenchPostalCode reste exporté pour valider une valeur ponctuelle.
}

// ClassifyValues analyse les valeurs échantillonnées d'une colonne et retourne la
// classification la plus spécifique atteignant un seuil exploitable. ok vaut false
// si aucun validateur ne ressort.
func ClassifyValues(values []string, dataType string) (ContentClassification, bool) {
	clean := make([]string, 0, len(values))
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) < minSamples {
		return ContentClassification{}, false
	}

	var fallback ContentClassification
	var hasFallback bool

	for i := range validators {
		val := &validators[i]
		matched := 0
		for _, v := range clean {
			if val.fn(v) {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		ratio := float64(matched) / float64(len(clean))

		suggested := val.suggested
		if val.numericSuggested != mgmtv1alpha1.TransformerSource_TRANSFORMER_SOURCE_UNSPECIFIED &&
			isNumericType(dataType) {
			suggested = val.numericSuggested
		}

		switch {
		case ratio >= confirmRatio && !val.weak:
			return ContentClassification{
				Category:   val.category,
				Sensitive:  true,
				Suggested:  suggested,
				Confidence: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_CONFIRMED,
				Method:     mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CHECKSUM,
				Evidence: fmt.Sprintf("%s vérifié sur %d/%d valeurs",
					val.label, matched, len(clean)),
			}, true
		case ratio >= reviewRatio:
			// On mémorise le meilleur candidat douteux, mais on continue à
			// chercher une preuve franche parmi les validateurs suivants.
			if !hasFallback {
				reason := fmt.Sprintf("%s reconnu sur %d/%d valeurs seulement",
					val.label, matched, len(clean))
				if val.weak {
					reason = fmt.Sprintf("%s : forme trop courante pour conclure (%d/%d valeurs)",
						val.label, matched, len(clean))
				}
				fallback = ContentClassification{
					Category:   val.category,
					Sensitive:  true,
					Suggested:  suggested,
					Confidence: mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW,
					Method:     mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_CHECKSUM,
					Evidence:   reason,
				}
				hasFallback = true
			}
		}
	}

	if hasFallback {
		return fallback, true
	}
	return ContentClassification{}, false
}

// --- Contrôles unitaires ----------------------------------------------------

var (
	emailRe    = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	digitsOnly = regexp.MustCompile(`^[0-9]+$`)
	nonDigit   = regexp.MustCompile(`[^0-9A-Za-z]`)
)

func isEmail(v string) bool {
	return len(v) <= 254 && emailRe.MatchString(v)
}

func isIP(v string) bool {
	return net.ParseIP(v) != nil
}

// strip retire les séparateurs usuels (espaces, points, tirets) pour comparer la
// suite de caractères significatifs.
func strip(v string) string {
	return strings.ToUpper(nonDigit.ReplaceAllString(v, ""))
}

// luhn applique l'algorithme de Luhn à une suite de chiffres.
func luhn(digits string) bool {
	if !digitsOnly.MatchString(digits) || len(digits) < 2 {
		return false
	}
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// IsNIR valide un numéro de sécurité sociale français : 15 chiffres dont 2 de clé,
// clé = 97 - (les 13 premiers chiffres mod 97). La Corse utilise 2A/2B en
// département, remplacés respectivement par 19 et 18 dans le calcul.
func IsNIR(v string) bool {
	s := strip(v)
	if len(s) != 15 {
		return false
	}
	if s[0] != '1' && s[0] != '2' {
		return false
	}
	// Mois de naissance : 01-12, ou 20/30/40+ pour les cas de date incomplète.
	month, err := strconv.Atoi(s[3:5])
	if err != nil || month == 0 || (month > 12 && month < 20) || month > 42 {
		return false
	}

	body := s[:13]
	// Corse : le département est alphanumérique.
	if strings.Contains(body[5:7], "A") {
		body = body[:5] + "19" + body[7:]
	} else if strings.Contains(body[5:7], "B") {
		body = body[:5] + "18" + body[7:]
	}
	if !digitsOnly.MatchString(body) || !digitsOnly.MatchString(s[13:]) {
		return false
	}

	num, err := strconv.ParseInt(body, 10, 64)
	if err != nil {
		return false
	}
	key, err := strconv.Atoi(s[13:])
	if err != nil {
		return false
	}
	return int(97-(num%97)) == key
}

// IsIBAN valide un IBAN par sa clé mod 97 (norme ISO 13616) : on déplace les 4
// premiers caractères à la fin, on convertit les lettres (A=10...Z=35), et le
// nombre obtenu doit être congru à 1 modulo 97.
func IsIBAN(v string) bool {
	s := strip(v)
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	if s[0] < 'A' || s[0] > 'Z' || s[1] < 'A' || s[1] > 'Z' {
		return false
	}
	rearranged := s[4:] + s[:4]
	// Modulo progressif : l'entier complet dépasse la capacité d'un int64.
	rem := 0
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			rem = rem*10 + int(r-'0')
		case r >= 'A' && r <= 'Z':
			val := int(r-'A') + 10
			rem = rem*100 + val
		default:
			return false
		}
		rem %= 97
	}
	return rem == 1
}

// IsSiretOrSiren valide un SIRET (14 chiffres) ou un SIREN (9 chiffres) par Luhn.
func IsSiretOrSiren(v string) bool {
	s := strip(v)
	if len(s) != 14 && len(s) != 9 {
		return false
	}
	return luhn(s)
}

// IsCreditCard valide un numéro de carte : 13 à 19 chiffres, Luhn correct, et un
// préfixe de réseau plausible (Visa 4, Mastercard 5x/2x, Amex 3x, Discover 6x).
// Le contrôle du préfixe évite d'attraper n'importe quelle suite passant Luhn.
func IsCreditCard(v string) bool {
	s := strip(v)
	if len(s) < 13 || len(s) > 19 || !digitsOnly.MatchString(s) {
		return false
	}
	switch s[0] {
	case '2', '3', '4', '5', '6':
	default:
		return false
	}
	return luhn(s)
}

// IsFrenchPhone valide un numéro français : 0X suivi de 8 chiffres, ou une forme
// internationale +33 / 0033. Les séparateurs usuels sont tolérés.
func IsFrenchPhone(v string) bool {
	s := strings.TrimSpace(v)
	s = strings.NewReplacer(" ", "", ".", "", "-", "", "(", "", ")", "", "/", "").Replace(s)
	switch {
	case strings.HasPrefix(s, "+33"):
		s = "0" + s[3:]
	case strings.HasPrefix(s, "0033"):
		s = "0" + s[4:]
	}
	if len(s) != 10 || !digitsOnly.MatchString(s) {
		return false
	}
	// Indicatif national : 01-09, hors 00.
	return s[0] == '0' && s[1] >= '1' && s[1] <= '9'
}

// IsFrenchPostalCode valide un code postal français : 5 chiffres dont un
// département entre 01 et 98 (00 et 99 n'existent pas).
func IsFrenchPostalCode(v string) bool {
	s := strings.TrimSpace(strings.ReplaceAll(v, " ", ""))
	if len(s) != 5 || !digitsOnly.MatchString(s) {
		return false
	}
	dept, err := strconv.Atoi(s[:2])
	if err != nil {
		return false
	}
	return dept >= 1 && dept <= 98
}
