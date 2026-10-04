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
	ascii := true
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
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

// fold writes a name in lowercase letters without marks.
func fold(name string) string {
	return strings.ToLower(unmark(name))
}
