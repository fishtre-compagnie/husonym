package piidetect

import (
	"slices"
	"strings"
	"testing"
)

func TestSpellings(t *testing.T) {
	for word, want := range map[string][]string{
		"email":               {"email"},
		"führerschein":        {"fuhrerschein", "fuehrerschein"},
		"staatsangehörigkeit": {"staatsangehorigkeit", "staatsangehoerigkeit"},
		"vollständiger name":  {"vollstandiger name", "vollstaendiger name"},
		"straße":              {"strasse"},
		"rue":                 {"rue"},
	} {
		if got := spellings(word); !slices.Equal(got, want) {
			t.Errorf("spellings(%q) = %q, want %q", word, got, want)
		}
	}
}

// Every keyword the dictionary writes with an umlaut is found as a name writes it: with
// the umlaut in either case, without it, and with the letters that stand for it.
func TestClassify_EveryKeywordWithAnUmlautInEverySpelling(t *testing.T) {
	var seen []string
	for i := range dictionary {
		for _, keyword := range dictionary[i].keywords {
			if isASCII(keyword) {
				continue
			}
			seen = append(seen, keyword)
			name := strings.ReplaceAll(keyword, " ", "_")
			for _, written := range []string{
				name, strings.ToUpper(name), fold(name), transliterated.Replace(name),
				strings.ToUpper(transliterated.Replace(name)),
			} {
				got, _ := Classify(written, "text")
				if got.Category != dictionary[i].category {
					t.Errorf("Classify(%q) = %q, want %q", written, got.Category, dictionary[i].category)
				}
			}
		}
	}
	for _, keyword := range []string{
		"staatsangehörigkeit", "nationalität", "ethnizität", "führerschein", "vollständiger name",
	} {
		if !slices.Contains(seen, keyword) {
			t.Errorf("the dictionary does not write %q with its umlaut", keyword)
		}
	}
}

// The rules compare words written in ASCII: no word of theirs keeps a mark.
func TestRules_HoldASCIIWordsOnly(t *testing.T) {
	for i := range rules {
		ru := &rules[i]
		words := slices.Concat(ru.keywords, ru.excludeTokens, ru.ownTokens)
		for _, g := range ru.guarded {
			words = append(words, g.word)
			words = slices.Concat(words, g.among, g.unless, g.despite)
		}
		for _, word := range words {
			if !isASCII(word) {
				t.Errorf("%s: %q is not written in ASCII", ru.category, word)
			}
		}
	}
	for word := range vocabulary.words {
		if !isASCII(word) {
			t.Errorf("the vocabulary holds %q", word)
		}
	}
}

// The words that qualify a datum are read in every spelling too.
func TestClassify_QualifiersWithAnUmlaut(t *testing.T) {
	expectNone(t,
		"email_bestätigt", "email_bestaetigt", "EMAIL_BESTÄTIGT", "email_gültig", "email_gueltig",
		"adresse_länge", "adresse_laenge",
	)
}
