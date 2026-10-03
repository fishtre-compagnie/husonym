package piitext

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

// findingsFrom draws findings inside a text of the given number of characters from the bytes
// of a seed: four bytes each, for the start, the length, the entity type and the score.
func findingsFrom(seed []byte, characters int) []presidio.Finding {
	entities := []string{"PERSON", "LOCATION", "EMAIL_ADDRESS"}
	findings := []presidio.Finding{}
	for i := 0; i+3 < len(seed) && len(findings) < 8; i += 4 {
		start := int(seed[i]) % (characters + 1)
		end := min(start+int(seed[i+1])%6, characters)
		findings = append(findings, presidio.Finding{
			EntityType: entities[int(seed[i+2])%len(entities)],
			Start:      start,
			End:        end,
			Score:      float64(seed[i+3]%5) / 4,
		})
	}
	return findings
}

// covered tells, for each character of a text, whether a finding designates it.
func covered(characters int, findings []presidio.Finding) []bool {
	in := make([]bool, characters)
	for _, finding := range findings {
		for i := finding.Start; i < finding.End; i++ {
			in[i] = true
		}
	}
	return in
}

// privateUse stands for a rewritten finding in the properties below: a character no seed
// text carries.
const privateUse = ""

// Whatever the findings and however they overlap: no character a finding designates survives
// in the output unless the operator keeps it, every other character stays where it was, in
// order, and the output is valid text when the input is.
func Fuzz_Transform_RewritesExactlyWhatWasFound(f *testing.F) {
	for _, tc := range texts {
		f.Add(tc.text, []byte{0, 3, 0, 4, 2, 4, 1, 2})
		f.Add(tc.text, []byte{1, 5, 0, 3, 3, 5, 0, 3, 6, 2, 1, 4})
		f.Add(tc.text, []byte{0, 5, 0, 1, 0, 5, 1, 1, 4, 1, 0, 2, 5, 4, 0, 2})
	}
	f.Add("James Bond met Jörg Müller", []byte{0, 5, 0, 3, 6, 4, 0, 3, 15, 4, 0, 3, 20, 5, 0, 3})
	f.Add("", []byte{0, 0, 0, 0})
	f.Add("caf\xff Bob", []byte{4, 3, 0, 4})

	f.Fuzz(func(t *testing.T, text string, seed []byte) {
		if text == "" || strings.Contains(text, privateUse) {
			t.Skip()
		}
		characters := utf8.RuneCountInString(text)
		findings := findingsFrom(seed, characters)
		in := covered(characters, findings)

		var kept strings.Builder
		position := 0
		for _, character := range text {
			if !in[position] {
				kept.WriteRune(character)
			}
			position++
		}
		// An invalid byte is read as U+FFFD by the loop above and kept as it is by the
		// transformer: the comparison is made on valid input only.
		valid := utf8.ValidString(text)

		// The spaces between two findings of one type are rewritten with them: the
		// comparisons leave spaces out.
		spaceless := func(s string) string { return strings.ReplaceAll(s, " ", "") }

		redacted, err := rewrite(t, withDefault(redact()), Options{}, text, findings...)
		require.NoError(t, err)
		if valid {
			require.Equal(t, spaceless(kept.String()), spaceless(redacted),
				"redacting leaves exactly what no finding designates")
			require.True(t, utf8.ValidString(redacted))
		}

		replaced, err := rewrite(t, withDefault(replaceWith(privateUse)), Options{}, text, findings...)
		require.NoError(t, err)
		if valid {
			require.Equal(t, spaceless(kept.String()), spaceless(strings.ReplaceAll(replaced, privateUse, "")),
				"replacing leaves exactly what no finding designates, in order")
			require.True(t, utf8.ValidString(replaced))
		}

		masked, err := rewrite(t, withDefault(maskAll(privateUse)), Options{}, text, findings...)
		require.NoError(t, err)
		if valid {
			original := []rune(text)
			require.Equal(t, characters, utf8.RuneCountInString(masked), "a full mask keeps the number of characters")
			position = 0
			for _, character := range masked {
				switch {
				case in[position]:
					require.Equal(t, '', character, "character %d of a finding is masked", position)
				case character == '':
					require.Equal(t, ' ', original[position], "character %d is masked and in no finding", position)
				default:
					require.Equal(t, original[position], character, "character %d is in no finding", position)
				}
				position++
			}
		}
	})
}

// The overlap rule gives spans that do not overlap, in the order of the text, and that cover
// exactly what the findings cover — more only by the spaces between two findings it joins.
func Fuzz_Resolve_CoversWhatWasFound(f *testing.F) {
	f.Add("James Bond met Jörg Müller", []byte{0, 5, 0, 3, 6, 4, 0, 3, 15, 4, 0, 3, 20, 5, 0, 3})
	f.Add("ééééé John Mary 👩‍💻", []byte{0, 5, 0, 1, 3, 5, 1, 1, 4, 5, 2, 2, 5, 4, 0, 2})

	f.Fuzz(func(t *testing.T, text string, seed []byte) {
		if text == "" || !utf8.ValidString(text) {
			t.Skip()
		}
		findings := findingsFrom(seed, utf8.RuneCountInString(text))
		located, err := locate(text, findings)
		require.NoError(t, err)

		byteIn := make([]bool, len(text))
		for _, target := range located {
			for i := target.start; i < target.end; i++ {
				byteIn[i] = true
			}
		}

		resolved := resolve(text, located)
		end := 0
		for _, target := range resolved {
			require.GreaterOrEqual(t, target.start, end, "spans do not overlap and follow the text")
			require.Less(t, target.start, target.end, "no span is empty")
			require.True(t, utf8.ValidString(target.of(text)), "a span starts and ends between characters")
			for i := target.start; i < target.end; i++ {
				require.True(t, byteIn[i] || text[i] == ' ', "byte %d is covered by no finding", i)
				byteIn[i] = false
			}
			end = target.end
		}
		for i, in := range byteIn {
			require.False(t, in, "byte %d of a finding is in no span", i)
		}
	})
}
