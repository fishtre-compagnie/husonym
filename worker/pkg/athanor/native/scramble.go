package native

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
	"github.com/fishtre-compagnie/husonym/worker/pkg/rng"
)

// scrambleDraws is how many times a value is drawn again when the draw gives it back, before
// one of its characters is moved to the next of its class.
const scrambleDraws = 4

// CharacterScrambler redraws each character of a text within its class — a letter among the
// ASCII letters of its case, a digit among the ASCII digits, a listed sign among the listed
// signs (transformers.ScrambleAlphabet) — and keeps spaces and every other sign. The draws
// come from the seed of the whole value, so equal values give equal outputs inside a
// consistency scope, and two values that differ by one character give unrelated outputs.
//
// The output has as many characters as the value. It is never the value itself, unless the
// value holds nothing to redraw. Two different values may give the same output: the fewer
// the characters, the likelier.
type CharacterScrambler struct {
	domain *consistency.Domain
	only   *regexp.Regexp
}

// NewCharacterScrambler builds the transformer on a consistency domain. With an expression,
// only what it matches in a value is redrawn; a value it matches nowhere is redrawn whole.
// The domain reads values as they are: case and surrounding spaces tell two values apart.
func NewCharacterScrambler(domain *consistency.Domain, only *regexp.Regexp) *CharacterScrambler {
	return &CharacterScrambler{domain: domain.WithCanonicalizer(consistency.Exact), only: only}
}

func (s *CharacterScrambler) TransformValue(_ transform.Ctx, in any) (any, error) {
	var value string
	switch v := in.(type) {
	case nil:
		return nil, nil
	case *string:
		if v == nil {
			return nil, nil
		}
		value = *v
	case string:
		value = v
	case []byte:
		value = string(v)
	default:
		value = fmt.Sprint(in)
	}

	spans := s.spans(value)
	seed := s.domain.Seed(value)
	draws := rng.NewSplit(binary.BigEndian.Uint64(seed[0:8]), binary.BigEndian.Uint64(seed[8:16]))
	for range scrambleDraws {
		if out := redraw(value, spans, draws); out != value {
			return out, nil
		}
	}
	return shiftFirst(value, spans), nil
}

// spans are the byte ranges of the value to redraw, in order and apart from each other.
func (s *CharacterScrambler) spans(value string) [][]int {
	if s.only != nil {
		if matches := s.only.FindAllStringIndex(value, -1); matches != nil {
			return matches
		}
	}
	return [][]int{{0, len(value)}}
}

// eachCharacter rebuilds the value, asking replace for each character inside the spans that
// has a class, with the class.
func eachCharacter(value string, spans [][]int, replace func(r rune, alphabet string) rune) string {
	var out strings.Builder
	out.Grow(len(value))
	next := 0
	for at := 0; at < len(value); {
		r, width := utf8.DecodeRuneInString(value[at:])
		for next < len(spans) && at >= spans[next][1] {
			next++
		}
		inside := next < len(spans) && at >= spans[next][0]
		if alphabet := transformers.ScrambleAlphabet(r); inside && alphabet != "" {
			out.WriteRune(replace(r, alphabet))
		} else {
			// The bytes as they are: one that is no valid UTF-8 stays what it is.
			out.WriteString(value[at : at+width])
		}
		at += width
	}
	return out.String()
}

func redraw(value string, spans [][]int, draws rng.Rand) string {
	return eachCharacter(value, spans, func(_ rune, alphabet string) rune {
		return rune(alphabet[draws.Intn(len(alphabet))])
	})
}

// shiftFirst moves the first character that has a class to the next member of the class.
func shiftFirst(value string, spans [][]int) string {
	done := false
	return eachCharacter(value, spans, func(r rune, alphabet string) rune {
		if done {
			return r
		}
		done = true
		return rune(alphabet[(strings.IndexRune(alphabet, r)+1)%len(alphabet)])
	})
}

var _ transform.ValueTransformer = (*CharacterScrambler)(nil)
