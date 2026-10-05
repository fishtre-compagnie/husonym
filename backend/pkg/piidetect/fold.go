package piidetect

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// unmarked are the letters Unicode does not write as a letter and a mark, each beside the
// ASCII letters the rules know it by, in its case.
var unmarked = strings.NewReplacer(
	"ł", "l", "Ł", "L", "ø", "o", "Ø", "O", "đ", "d", "Đ", "D", "ð", "d", "Ð", "D",
	"ß", "ss", "ẞ", "SS", "æ", "ae", "Æ", "AE", "œ", "oe", "Œ", "OE", "þ", "th", "Þ", "TH",
	"ı", "i",
)

// unmark writes a name without the marks of its letters, in the case it was written in:
// "PRÉNOM" gives "PRENOM", "Straße" gives "Strasse".
func unmark(name string) string {
	if isASCII(name) {
		return name
	}
	var out strings.Builder
	out.Grow(len(name))
	for _, r := range norm.NFD.String(name) {
		if !unicode.Is(unicode.Mn, r) {
			out.WriteRune(r)
		}
	}
	return unmarked.Replace(out.String())
}

func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			return false
		}
	}
	return true
}

// fold writes a name in lowercase letters without marks.
func fold(name string) string {
	return strings.ToLower(unmark(name))
}

// transliterated are the umlauts beside the letters German writes in their place.
var transliterated = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "AE", "Ö", "OE", "Ü", "UE")

// spellings gives the ways the words of the dictionary are written in a name. The
// dictionary writes a word with its marks. A name is read without them (see unmark), so
// every word is known folded; a word with an umlaut is also known with the letters that
// stand for it: "staatsangehörigkeit" is read in "staatsangehorigkeit" and in
// "staatsangehoerigkeit".
func spellings(words ...string) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		folded := fold(word)
		out = append(out, folded)
		if other := fold(transliterated.Replace(word)); other != folded {
			out = append(out, other)
		}
	}
	return out
}
