package piidetect

import "strings"

const (
	// FreeTextMinWords is the mean number of words per value from which a column is free text.
	FreeTextMinWords = 3.0
	// FreeTextMinMatches is how many values of a free-text column must hold a sensitive entity
	// for the column to be told personal, whatever the size of the sample.
	FreeTextMinMatches = 2
	// FreeTextCategory is the data category of a free-text column found to hold personal data.
	FreeTextCategory = "free_text_pii"
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
