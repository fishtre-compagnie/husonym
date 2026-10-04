package piidetect

import "strings"

// A name may hold the word of a datum without holding the datum: email_format says how
// an email is written, address_count how many addresses there are, is_email_verified
// whether an email was checked. Such a name is not a finding.
//
// The lists below are closed, and short on purpose: missing personal data is worse than a
// false alarm, so a word is here only when it turns the name into something about the
// datum (its form, its kind, its number, its state, a yes or a no). A word that names a
// part or a derivative of the datum is not here: a hash, a digest, a salt, a domain, a
// prefix, a number.

// qualifierNouns qualify at either end of a name: email_format, format_email.
var qualifierNouns = wordSet(
	// its form
	"format", "formato", "formaat", "template", "mask", "pattern", "regex",
	// its kind
	"type", "typ", "tipo", "model", "modele", "modell", "modelo", "modello", "version", "versione", "versie",
	"wersja", "versao",
	// how many, how long, how much of it
	"count", "nb", "anzahl", "cantidad", "conteggio", "aantal", "liczba", "quantidade", "total",
	"length", "longueur", "laenge", "longitud", "lunghezza", "lengte", "dlugosc", "comprimento", "ratio",
	// its state
	"status", "statut", "estado", "stato", "stan",
	// a code that stands for it; a rule may own the word (see rule.ownTokens)
	"code", "codigo", "codice", "kod",
)

// qualifierAdjectives qualify at the end of a name only: email_verified is a yes or a no,
// verified_email is an email.
var qualifierAdjectives = wordSet(
	"verified", "verifie", "verifiee", "verifiziert", "verificado", "verificada",
	"verificato", "verificata", //nolint:misspell // Italian words
	"geverifieerd", "zweryfikowany", "zweryfikowana",
	"confirmed", "confirme", "confirmee", "bestaetigt", "confirmado", "confirmada", "confermato",
	"confermata", "bevestigd", "potwierdzony", "potwierdzona",
	"valid", "valide", "gueltig", "valido", "valida", "geldig",
	"enabled", "disabled", "active", "actif", "aktiv", "activo", "activa", "attivo", "attiva", "actief",
	"aktywny", "aktywna", "ativo", "ativa",
	"consent", "optin", "optout", "subscribed", "visible", "hidden", "required",
)

// qualifierFlags open a name that is a yes or a no: is_email_verified, has_phone.
var qualifierFlags = wordSet("is", "has", "est", "ist", "hat", "tiene", "heeft", "czy", "tem")

func wordSet(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// qualifies tells whether the tokens of a name make it a qualifier of the datum a rule
// matched. own are the tokens that are part of that datum.
func qualifies(tokens []string, own map[string]bool) bool {
	if len(tokens) < 2 {
		return false
	}
	first, last := tokens[0], tokens[len(tokens)-1]
	if qualifierFlags[first] {
		return true
	}
	if qualifierNouns[first] && !own[first] {
		return true
	}
	return (qualifierNouns[last] || qualifierAdjectives[last]) && !own[last]
}

// accents are the letters with a mark that column names hold, each beside the letter
// the rules know it by.
var accents = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "ą", "a",
	"ç", "c", "ć", "c",
	"è", "e", "é", "e", "ê", "e", "ë", "e", "ę", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i",
	"ł", "l", "ñ", "n", "ń", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o",
	"ś", "s", "ß", "ss",
	"ù", "u", "ú", "u", "û", "u", "ü", "u",
	"ź", "z", "ż", "z",
)

// fold writes a name in lowercase letters without marks.
func fold(name string) string {
	return accents.Replace(strings.ToLower(name))
}
