package piitext

import "golang.org/x/text/unicode/norm"

// allowedPhrases are the phrases a user wants left as they are. A finding is allowed when its
// whole text is one of them: the case counts, a space around it counts, and a phrase that is
// only part of the finding does not allow it.
//
// Both sides are compared in Unicode normalization form C: an accented letter written as one
// character and the same letter written as a letter followed by its accent are one phrase.
type allowedPhrases map[string]struct{}

func newAllowedPhrases(phrases []string) allowedPhrases {
	allowed := make(allowedPhrases, len(phrases))
	for _, phrase := range phrases {
		allowed[norm.NFC.String(phrase)] = struct{}{}
	}
	return allowed
}

// filter returns the targets whose text is not allowed, in their order.
func (a allowedPhrases) filter(value string, targets []target) []target {
	if len(a) == 0 {
		return targets
	}
	kept := make([]target, 0, len(targets))
	for _, target := range targets {
		if _, ok := a[norm.NFC.String(target.of(value))]; !ok {
			kept = append(kept, target)
		}
	}
	return kept
}
