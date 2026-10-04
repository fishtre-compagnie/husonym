package native

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/consistency"
	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/transform"
)

func scramblerUnder(key, scope string, regex *regexp.Regexp) *CharacterScrambler {
	return NewCharacterScrambler(consistency.New([]byte(key), scope).Domain("text.scramble"), regex)
}

func scrambled(t *testing.T, s *CharacterScrambler, in any) string {
	t.Helper()
	out, err := s.TransformValue(transform.Background(), in)
	require.NoError(t, err)
	text, ok := out.(string)
	require.Truef(t, ok, "a text comes out as a string, got %T", out)
	return text
}

func TestCharacterScrambler_EqualInputsGiveEqualOutputs(t *testing.T) {
	const value = "FR-1987-04-AB12345"
	first := scramblerUnder("key", "job:a", nil)
	second := scramblerUnder("key", "job:a", nil)

	out := scrambled(t, first, value)
	require.Equal(t, out, scrambled(t, first, value), "the same transformer, twice")
	require.Equal(t, out, scrambled(t, second, value), "another transformer of the same scope")
	require.Equal(t, out, scrambled(t, first, &[]string{value}[0]), "the value behind a pointer")
	require.Equal(t, out, scrambled(t, first, []byte(value)), "the value as bytes")

	require.NotEqual(t, out, scrambled(t, scramblerUnder("key", "job:b", nil), value), "another scope")
	require.NotEqual(t, out, scrambled(t, scramblerUnder("other key", "job:a", nil), value), "another key")
	require.NotEqual(t, out, scrambled(t, first, "FR-1987-04-AB12346"), "another value")
	require.NotEqual(t, scrambled(t, first, "abcdefgh"), scrambled(t, first, "ABCDEFGH"), "the case tells two values apart")
	require.NotEqual(t, scrambled(t, first, "abcdefgh"), scrambled(t, first, "abcdefgh "), "so does a trailing space")
}

func TestCharacterScrambler_KeepsTheClassOfEachCharacter(t *testing.T) {
	const value = "Jean Dupont 42, rue de l'Abbé #7"
	out := scrambled(t, scramblerUnder("key", "job:a", nil), value)

	in, got := []rune(value), []rune(out)
	require.Len(t, got, len(in), "as many characters as the value")
	for i, r := range in {
		switch {
		case unicode.IsSpace(r):
			require.Equalf(t, r, got[i], "position %d: a space stays", i)
		case unicode.IsUpper(r):
			require.Truef(t, got[i] >= 'A' && got[i] <= 'Z', "position %d: %q is no upper-case letter", i, got[i])
		case unicode.IsLetter(r):
			require.Truef(t, got[i] >= 'a' && got[i] <= 'z', "position %d: %q is no lower-case letter", i, got[i])
		case unicode.IsDigit(r):
			require.Truef(t, got[i] >= '0' && got[i] <= '9', "position %d: %q is no digit", i, got[i])
		case r == '\'':
			require.Equalf(t, r, got[i], "position %d: a sign outside the list stays", i)
		default:
			require.Truef(t, strings.ContainsRune("!@#$%^&*()-+=_ []{}|\\;\"<>,./?", got[i]), "position %d: %q is no listed sign", i, got[i])
		}
	}
}

func TestCharacterScrambler_NonASCII(t *testing.T) {
	s := scramblerUnder("key", "job:a", nil)

	out := scrambled(t, s, "Élodie Ünal ٣٤ 東京 €")
	got := []rune(out)
	require.Len(t, got, utf8.RuneCountInString("Élodie Ünal ٣٤ 東京 €"), "the length is counted in characters")
	require.Truef(t, got[0] >= 'A' && got[0] <= 'Z', "an accented capital becomes an ASCII capital, got %q", got[0])
	require.Truef(t, got[7] >= 'A' && got[7] <= 'Z', "got %q", got[7])
	require.Truef(t, got[12] >= '0' && got[12] <= '9', "a digit of another script becomes an ASCII digit, got %q", got[12])
	require.Truef(t, got[13] >= '0' && got[13] <= '9', "got %q", got[13])
	require.Truef(t, got[15] >= 'a' && got[15] <= 'z', "a letter without case becomes a lower-case ASCII letter, got %q", got[15])
	require.Equal(t, '€', got[18], "a sign outside the list stays")
}

func TestCharacterScrambler_KeepsNullAndEmpty(t *testing.T) {
	s := scramblerUnder("key", "job:a", nil)
	ctx := transform.Background()

	out, err := s.TransformValue(ctx, nil)
	require.NoError(t, err)
	require.Nil(t, out)

	var absent *string
	out, err = s.TransformValue(ctx, absent)
	require.NoError(t, err)
	require.Nil(t, out)

	require.Empty(t, scrambled(t, s, ""))
	require.Equal(t, "  \t", scrambled(t, s, "  \t"), "nothing to redraw")
	require.Equal(t, "'€'", scrambled(t, s, "'€'"), "nothing to redraw")
}

// The classes hold ten, twenty-six and twenty-nine members: over that many values of one
// character, a plain draw would give some of them back.
func TestCharacterScrambler_NeverGivesTheValueBack(t *testing.T) {
	s := scramblerUnder("key", "job:a", nil)
	for _, alphabet := range []string{
		"0123456789",
		"abcdefghijklmnopqrstuvwxyz",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"!@#$%^&*()-+=_[]{}|\\;\"<>,./?",
	} {
		for _, r := range alphabet {
			value := string(r)
			require.NotEqualf(t, value, scrambled(t, s, value), "one character %q", value)
			padded := " " + value + " "
			require.NotEqualf(t, padded, scrambled(t, s, padded), "one character among spaces %q", padded)
		}
	}
	for scope := range 200 {
		under := scramblerUnder("key", "job:"+string(rune('a'+scope%26))+string(rune('a'+scope/26)), nil)
		for _, value := range []string{"7", "42", "x", "No", "a1"} {
			require.NotEqualf(t, value, scrambled(t, under, value), "scope %d, value %q", scope, value)
		}
	}
}

func TestCharacterScrambler_UserRegex(t *testing.T) {
	const value = "josé.martin@example.com"
	s := scramblerUnder("key", "job:a", regexp.MustCompile(`@.*$`))

	out := scrambled(t, s, value)
	require.True(t, strings.HasPrefix(out, "josé.martin"), "what the expression does not match stays: %q", out)
	require.NotEqual(t, value, out)
	require.Equal(t, utf8.RuneCountInString(value), utf8.RuneCountInString(out))
	require.Equal(t, out, scrambled(t, s, value))

	several := scramblerUnder("key", "job:a", regexp.MustCompile(`é+|[0-9]+`))
	out = scrambled(t, several, "éé-12-ab-34-éé")
	got := []rune(out)
	require.Len(t, got, 14)
	require.Equal(t, "-ab-", string(got[5:9]))
	require.Equal(t, '-', got[2])
	require.Equal(t, '-', got[11])

	none := scramblerUnder("key", "job:a", regexp.MustCompile(`[0-9]+`))
	out = scrambled(t, none, "abcdef")
	require.NotEqual(t, "abcdef", out, "an expression that matches nothing scrambles the whole value")

	one := scramblerUnder("key", "job:a", regexp.MustCompile(`[0-9]`))
	for _, value := range []string{"a1", "b2", "c3", "d4", "e5", "f6", "g7", "h8", "i9", "j0"} {
		out = scrambled(t, one, value)
		require.Equal(t, value[:1], out[:1])
		require.NotEqual(t, value, out)
	}
}

func TestCharacterScrambler_OtherTypes(t *testing.T) {
	s := scramblerUnder("key", "job:a", nil)
	out := scrambled(t, s, 12345)
	require.Len(t, out, 5)
	require.NotEqual(t, "12345", out)
	require.Equal(t, scrambled(t, s, "12345"), out, "a number is scrambled as it is written")
}

func TestCharacterScrambler_BytesThatAreNoTextStay(t *testing.T) {
	out := scrambled(t, scramblerUnder("key", "job:a", nil), "ab\xffcd")
	require.Len(t, out, 5)
	require.Equal(t, byte(0xff), out[2])
}

func Test_shiftFirst(t *testing.T) {
	whole := func(value string) [][]int { return [][]int{{0, len(value)}} }
	require.Equal(t, "0", shiftFirst("9", whole("9")))
	require.Equal(t, " a9", shiftFirst(" z9", whole(" z9")))
	require.Equal(t, "A", shiftFirst("Z", whole("Z")))
	require.Equal(t, "@", shiftFirst("!", whole("!")))
	require.Equal(t, "a2", shiftFirst("a1", [][]int{{1, 2}}))
	require.Equal(t, " ' ", shiftFirst(" ' ", whole(" ' ")))
}
