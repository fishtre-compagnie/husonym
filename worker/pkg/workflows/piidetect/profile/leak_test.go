package profile

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// The letters and digits the values of these tests are made of. None of them is a letter
// of a member name of the serialized profile, of a kind or of a mask, and the digit runs
// are longer than any count a profile of 200 rows can hold: what is found of them in a
// profile can only come from a value.
const (
	leakLetters = "BCDFGHJKLMNPQRSTVWXZ"
	leakDigits  = "345678"
)

func randomValue(rng *rand.Rand) string {
	var b strings.Builder
	for range 2 + rng.IntN(4) {
		alphabet := leakLetters
		if rng.IntN(3) == 0 {
			alphabet = leakDigits
		}
		for range 4 + rng.IntN(8) {
			b.WriteByte(alphabet[rng.IntN(len(alphabet))])
		}
		b.WriteString([]string{" ", "-", "@", ".", "", "_"}[rng.IntN(6)])
	}
	return b.String()
}

// runs returns the runs of letters and of digits of a value.
func runs(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// A profile holds no value and no part of a value: no three characters in a row of a
// run of letters, no four of a run of digits.
func Test_Profile_HoldsNoPartOfAValue(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		table := NewTable([]Detector{{Name: "any", Match: func(string) bool { return true }}})
		values := make([]string, 0, 200)
		for range 1 + rng.IntN(200) {
			value := randomValue(rng)
			values = append(values, value)
			table.Add(map[string]any{"c": value, "n": int64(rng.IntN(900000) + 3333333)})
		}
		encoded, err := json.Marshal(table.Profile("c"))
		require.NoError(t, err)
		profile := string(encoded)

		for _, value := range values {
			require.NotContains(t, profile, `"`+value+`"`)
			for _, run := range runs(value) {
				window := 3
				if unicode.IsDigit(rune(run[0])) {
					window = 4
				}
				for i := 0; i+window <= len(run); i++ {
					require.NotContains(t, profile, run[i:i+window], "value %q, profile %s", value, profile)
				}
			}
		}
	}
}

// shadow replaces every letter and every digit by another one of the same class: the
// layout, the lengths and the distinct values of a sample are kept, its content is not.
func shadow(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return 'a' + (r-'a'+1)%26
		case r >= 'A' && r <= 'Z':
			return 'A' + (r-'A'+1)%26
		case r >= '0' && r <= '9':
			return '0' + (r-'0'+1)%10
		}
		return r
	}, value)
}

// Two samples that only differ by which letters and which digits they hold have the same
// profile: nothing in it depends on the content of a value.
func Fuzz_Profile_DependsOnNoLetterAndNoDigit(f *testing.F) {
	f.Add("jean.dupont@example.org", "FR76 3000 6000 0112 3456 7890 189", "1985-03-12")
	f.Add("", "   ", "rows")
	f.Add("a", "a", "b")
	f.Add("Zz99", "text", "200")
	f.Fuzz(func(t *testing.T, first, second, third string) {
		// Bytes that are not text have no letter to replace: shadow would merge them.
		if !utf8.ValidString(first) || !utf8.ValidString(second) || !utf8.ValidString(third) {
			t.Skip()
		}
		sample, shadowed := NewTable(nil), NewTable(nil)
		for range 2 {
			for _, value := range []string{first, second, third} {
				sample.Add(map[string]any{"c": value})
				shadowed.Add(map[string]any{"c": shadow(value)})
			}
		}
		want, err := json.Marshal(sample.Profile("c"))
		require.NoError(t, err)
		got, err := json.Marshal(shadowed.Profile("c"))
		require.NoError(t, err)
		require.Equal(t, string(want), string(got))
	})
}
