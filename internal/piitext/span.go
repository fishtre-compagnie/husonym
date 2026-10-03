package piitext

import (
	"fmt"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
)

// span is a part of a value, from the byte offset start to the byte offset end, exclusive.
// Both fall between two characters: a span is made by locate, and by nothing else.
type span struct {
	start, end int
}

// of returns the part of value the span designates.
func (s span) of(value string) string {
	return value[s.start:s.end]
}

// target is a span to rewrite, with the entity type that tells how.
type target struct {
	span
	entity string
	score  float64
}

// locate turns the findings of an analyzer into targets of value, in the order of the findings.
// It is the one place where the positions of a finding are read: they count characters, and a
// target counts bytes.
//
// A finding that does not fit the value is an error, never a finding left out: what it
// designates would otherwise stay as it is in a value returned as anonymized. A finding of no
// character designates nothing, and gives no target.
func locate(value string, findings []presidio.Finding) ([]target, error) {
	if len(findings) == 0 {
		return nil, nil
	}

	// offsets[i] is the byte offset of the character i, and offsets[characters] the length of
	// the value. A byte that is no character counts as one, here as for the analyzer, which is
	// sent U+FFFD in its place.
	offsets := make([]int, 0, len(value)+1)
	for offset := range value {
		offsets = append(offsets, offset)
	}
	offsets = append(offsets, len(value))
	characters := len(offsets) - 1

	targets := make([]target, 0, len(findings))
	for i, finding := range findings {
		if finding.Start < 0 || finding.Start > finding.End || finding.End > characters {
			return nil, fmt.Errorf(
				"finding %d spans %d to %d in a text of %d characters: %w",
				i, finding.Start, finding.End, characters, presidio.ErrInvalidResponse,
			)
		}
		if finding.EntityType == "" {
			return nil, fmt.Errorf("finding %d has no entity type: %w", i, presidio.ErrInvalidResponse)
		}
		if finding.Start == finding.End {
			continue
		}
		targets = append(targets, target{
			span:   span{start: offsets[finding.Start], end: offsets[finding.End]},
			entity: finding.EntityType,
			score:  finding.Score,
		})
	}
	return targets, nil
}
