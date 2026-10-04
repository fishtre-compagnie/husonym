package profile

import (
	"strings"
	"unicode"
)

const (
	// maskLimit is the length a mask is cut at.
	maskLimit = 32
	// The punctuation a mask shows as it is. Any other character that is neither a
	// letter, a digit nor a space is shown as "?".
	maskPunctuation = "@.,-_/:+()#"
)

// mask replaces every character of a value by its class: "A" for an uppercase letter,
// "a" for another letter, "9" for a digit, "?" for a character it does not show. A run of
// one class is written once, followed by "+"; a run of spaces is one space; a punctuation
// character of maskPunctuation stands for itself.
//
// A mask never spells a value: one that would hold no letter and no digit class, as the
// mask of ":-)" or of "-", is written "?+".
func mask(value string) string {
	layout, _ := layoutOf(value)
	return layout
}

// layoutOf returns the mask of a value, and whether it gathers characters: whether one
// of its runs stands for more than one character. A mask that gathers none gives the
// class of every character of the value, one by one.
func layoutOf(value string) (layout string, gathers bool) {
	var b strings.Builder
	var last rune
	abstracts := false
	for _, r := range value {
		if b.Len() >= maskLimit {
			break
		}
		class := maskClass(r)
		repeats := class == 'A' || class == 'a' || class == '9' || class == '?'
		if class == last && (repeats || class == ' ') {
			gathers = true
			continue
		}
		b.WriteRune(class)
		if repeats {
			b.WriteByte('+')
		}
		abstracts = abstracts || class == 'A' || class == 'a' || class == '9'
		last = class
	}
	if !abstracts {
		if b.Len() == 0 {
			return "", false
		}
		return "?+", gathers
	}
	if b.Len() > maskLimit {
		return b.String()[:maskLimit], gathers
	}
	return b.String(), gathers
}

func maskClass(r rune) rune {
	switch {
	case unicode.IsUpper(r):
		return 'A'
	case unicode.IsLetter(r):
		return 'a'
	case unicode.IsDigit(r):
		return '9'
	case unicode.IsSpace(r):
		return ' '
	case r < unicode.MaxASCII && strings.ContainsRune(maskPunctuation, r):
		return r
	}
	return '?'
}
