package piidetect

import "strings"

// A column name is read as words. Separators and case boundaries cut it into tokens
// (tokenize); a token written without separators is cut again when it is made of known
// words only (customeremail, telefonnummer, firstname), or around a long keyword that
// opens or closes it (addressline, lieunaissance). A keyword of a rule is compared with a
// word, never searched inside one: "mobil" is not found in "automobile", nor "city" in
// "capacity".

// pattern is one word of a keyword.
//
//	email     the word itself, or its plural in "s" when it has five letters or more
//	telefon*  a word that starts with it (telefono, telefonnummer)
//	*ssn      a word that ends with it (empssn)
type pattern struct {
	text           string
	prefix, suffix bool
}

// Words shorter than this have no plural of their own in the rules: "names" is not
// "name", "caps" is not "cap".
const pluralMinLength = 5

func parsePattern(word string) pattern {
	switch {
	case strings.HasSuffix(word, "*"):
		return pattern{text: strings.TrimSuffix(word, "*"), prefix: true}
	case strings.HasPrefix(word, "*"):
		return pattern{text: strings.TrimPrefix(word, "*"), suffix: true}
	default:
		return pattern{text: word}
	}
}

func (p pattern) matches(word string) bool {
	switch {
	case p.prefix:
		return strings.HasPrefix(word, p.text)
	case p.suffix:
		// What comes before is a word, and one letter is not one: "assn" is not an ssn.
		return word == p.text || (strings.HasSuffix(word, p.text) && len(word) >= len(p.text)+2)
	default:
		return word == p.text || (len(p.text) >= pluralMinLength && word == p.text+"s")
	}
}

// phrase is a keyword: one word or several that follow each other in the name.
type phrase []pattern

func parsePhrase(keyword string) phrase {
	fields := strings.Fields(keyword)
	out := make(phrase, len(fields))
	for i, field := range fields {
		out[i] = parsePattern(field)
	}
	return out
}

// in tells whether the words of the phrase follow each other somewhere in words.
func (p phrase) in(words []string) bool {
	for start := 0; start+len(p) <= len(words); start++ {
		found := true
		for i, pat := range p {
			if !pat.matches(words[start+i]) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

// lexicon is what the rules know: the words a glued token may be made of.
type lexicon struct {
	words map[string]bool
	stems []pattern
	// anchors are the keywords of one word and six letters or more: long enough to be
	// read at an end of a glued token whose other end the rules do not know.
	anchors map[string]bool
}

// The shortest keyword read at an end of a token the rules cannot cut entirely.
const anchorMinLength = 6

// Keywords that also end ordinary words, as a suffix of their language does: automobile,
// capacidade, destinazione, werkplaats. They are not read at the end of a token.
var ordinaryEndings = wordSet("mobile", "cidade", "nazione", "plaats")

// Keywords that also open ordinary words: secretary, secretaria. They are not read at
// the start of a token.
var ordinaryOpenings = wordSet("secret")

// What derives an ordinary word from a keyword: adressage, addressable, gendered. A
// token that ends with one of them after a keyword is not cut.
var derivations = wordSet(
	"age", "ed", "er", "ee", "es", "ing", "able", "less", "ment", "ary", "ive", "al", "ly", "ness", "ship",
)

// addAnchors records the keywords of one word among keywords.
func (l *lexicon) addAnchors(keywords ...string) {
	for _, keyword := range keywords {
		p := parsePhrase(keyword)
		if len(p) == 1 && !p[0].prefix && !p[0].suffix && len(p[0].text) >= anchorMinLength {
			l.anchors[p[0].text] = true
		}
	}
}

func (l *lexicon) add(keywords ...string) {
	for _, keyword := range keywords {
		for _, pat := range parsePhrase(keyword) {
			if pat.prefix || pat.suffix {
				l.stems = append(l.stems, pat)
				continue
			}
			l.words[pat.text] = true
		}
	}
}

// knows tells whether a token is a word of the rules as it stands.
func (l *lexicon) knows(token string) bool {
	if l.words[token] {
		return true
	}
	if strings.HasSuffix(token, "s") && len(token) > pluralMinLength && l.words[token[:len(token)-1]] {
		return true
	}
	for _, stem := range l.stems {
		if stem.matches(token) {
			return true
		}
	}
	return false
}

// Words of two letters a glued token may hold after its first word: the links of a
// phrase (dateofbirth, nombredeusuario), what a number or a reference ends with (telno,
// userid) and the address of a machine (clientip).
var shortWords = wordSet("of", "de", "di", "du", "da", "do", "nr", "no", "id", "ip")

// The one word of two letters that may open a glued token: idcard, idfiscal. The others
// open ordinary words (noemail, deville).
const shortOpening = "id"

// The shortest word a glued token is cut into, the short words above aside. Shorter ones
// are found by accident in ordinary words.
const gluedMinLength = 3

// split cuts a glued token into words. A token known as it stands stays whole. One made
// of known words only is cut into them. Otherwise a long keyword at one of its ends is
// cut from the rest (see around). Anything else stays whole.
func (l *lexicon) split(token string) []string {
	if parts, ok := l.whole(token); ok {
		return parts
	}
	if parts, ok := l.around(token); ok {
		return parts
	}
	return []string{token}
}

// whole reads a token made of known words only.
func (l *lexicon) whole(token string) ([]string, bool) {
	if l.knows(token) {
		return []string{token}, true
	}
	return l.cut(token, true)
}

// around cuts a token the rules do not know entirely into the known words that open or
// close it and the rest, taken as one word, when one of those known words is a long
// keyword: "addressline" gives address and line, "lieunaissance" lieu and naissance. The
// longest known side wins, and the rest is two letters or more.
func (l *lexicon) around(token string) ([]string, bool) {
	for size := len(token) - 2; size >= anchorMinLength; size-- {
		if parts, ok := l.whole(token[:size]); ok && l.anchored(parts) &&
			!ordinaryOpenings[parts[len(parts)-1]] && !derivations[token[size:]] {
			return append(parts, token[size:]), true
		}
		at := len(token) - size
		if parts, ok := l.whole(token[at:]); ok && l.anchored(parts) && !ordinaryEndings[parts[0]] {
			return append([]string{token[:at]}, parts...), true
		}
	}
	return nil, false
}

// anchored tells whether one of the words is a long keyword.
func (l *lexicon) anchored(words []string) bool {
	for _, word := range words {
		if l.anchors[word] {
			return true
		}
	}
	return false
}

// shortWordAt tells whether a word of two letters may stand at the start of a glued
// token, or after its first word.
func shortWordAt(word string, first bool) bool {
	if first {
		return word == shortOpening
	}
	return shortWords[word]
}

func (l *lexicon) cut(rest string, first bool) ([]string, bool) {
	for end := len(rest) - 1; end >= 2; end-- {
		head := rest[:end]
		if !l.words[head] {
			continue
		}
		if len(head) < gluedMinLength && !shortWordAt(head, first) {
			continue
		}
		tail := rest[end:]
		if l.endsWith(tail) {
			return []string{head, tail}, true
		}
		if parts, ok := l.cut(tail, false); ok {
			return append([]string{head}, parts...), true
		}
	}
	return nil, false
}

// endsWith tells whether tail may be the last word of a glued token.
func (l *lexicon) endsWith(tail string) bool {
	if len(tail) < gluedMinLength {
		return shortWords[tail]
	}
	return l.knows(tail)
}

// wordsOf reads a column name as words: its tokens, the glued ones cut, the numbers
// dropped (email2, address_1 and ssn4 are an email, an address and an ssn).
func wordsOf(name string) []string {
	var out []string
	for _, token := range tokenize(name) {
		if token[0] >= '0' && token[0] <= '9' {
			continue
		}
		out = append(out, vocabulary.split(token)...)
	}
	return out
}
