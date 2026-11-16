package piidetect

import (
	"slices"
	"strings"
)

const (
	// FreeTextMinWords is the mean number of words per value from which a column is free text.
	FreeTextMinWords = 3.0
	// FreeTextMinPersons is how many values of a free-text column must name a person for the
	// column to be told personal, whatever the size of the sample.
	FreeTextMinPersons = 2
	// FreeTextCategory is the data category of a free-text column found to name persons.
	FreeTextCategory = "free_text_pii"
	// PersonEntity is the entity an analyzer gives to the name of a person.
	PersonEntity = "PERSON"
)

// IsFreeText tells whether the values are sentences rather than names or codes: the mean number
// of words of the filled values reaches FreeTextMinWords. Empty values are ignored.
func IsFreeText(values []string) bool {
	words, filled := 0, 0
	for _, value := range values {
		count := len(strings.Fields(value))
		if count == 0 {
			continue
		}
		words += count
		filled++
	}
	return filled > 0 && float64(words)/float64(filled) >= FreeTextMinWords
}

// freeTextLanguages are the languages in which the rule of free text was measured against
// the engine of the analyzer: at most one column of six without a person reported, and the
// six columns with persons found. A language joins the list with its own measure.
var freeTextLanguages = []string{"fr"}

// FreeTextRuleApplies tells whether the rule of free text is applied to texts analyzed in the
// language, given as "fr" or as a locale such as "fr-FR". Elsewhere the engine of the
// analyzer takes places, companies and references for persons too often for the rule.
func FreeTextRuleApplies(language string) bool {
	base, _, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(language)), "_", "-"), "-")
	return slices.Contains(freeTextLanguages, base)
}
