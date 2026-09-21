package phoneformat

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestPseudonymizer(t *testing.T, keyByte byte) *Pseudonymizer {
	t.Helper()
	var key [32]byte
	for i := range key {
		key[i] = keyByte
	}
	return New(key)
}

// shape replaces every digit by '#', so that two values of the same format compare equal.
func shape(s string) string {
	return regexp.MustCompile(`[0-9]`).ReplaceAllString(s, "#")
}

func Test_Pseudonymize_KeepsPrefixSeparatorsAndLength(t *testing.T) {
	p := newTestPseudonymizer(t, 1)
	cases := []struct {
		in     string
		prefix string
	}{
		{"0612345678", "06"},
		{"06 12 34 56 78", "06"},
		{"06.12.34.56.78", "06"},
		{"01-23-45-67-89", "01"},
		{"+33 6 12 34 56 78", "+33 6"},
		{"+33612345678", "+336"},
		{"0033 7 12 34 56 78", "0033 7"},
		{"+1 (555) 123-4567", "+1 (5"},
		{"+44 20 7946 0958", "+44 2"},
		{"+352 621 123 456", "+352 6"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := p.Pseudonymize(c.in)
			require.NoError(t, err)
			require.Equal(t, shape(c.in), shape(got), "format")
			require.True(t, strings.HasPrefix(got, c.prefix), "%q should start with %q", got, c.prefix)
			require.NotEqual(t, c.in, got)
		})
	}
}

func Test_Pseudonymize_IsDeterministicUnderOneKey(t *testing.T) {
	a := newTestPseudonymizer(t, 1)
	b := newTestPseudonymizer(t, 1)
	other := newTestPseudonymizer(t, 2)

	first, err := a.Pseudonymize("06 12 34 56 78")
	require.NoError(t, err)
	again, err := b.Pseudonymize("06 12 34 56 78")
	require.NoError(t, err)
	require.Equal(t, first, again)

	elsewhere, err := other.Pseudonymize("06 12 34 56 78")
	require.NoError(t, err)
	require.NotEqual(t, first, elsewhere, "another key, another pseudonym")
}

// The same number written nationally and internationally keeps the same subscriber digits.
func Test_Pseudonymize_SameNumberAcrossFormats(t *testing.T) {
	p := newTestPseudonymizer(t, 1)
	subscriber := func(s string) string {
		digits := regexp.MustCompile(`[^0-9]`).ReplaceAllString(s, "")
		return digits[len(digits)-8:]
	}

	national, err := p.Pseudonymize("06 12 34 56 78")
	require.NoError(t, err)
	for _, in := range []string{"0612345678", "+33 6 12 34 56 78", "+33612345678", "0033 6 12 34 56 78"} {
		got, err := p.Pseudonymize(in)
		require.NoError(t, err)
		require.Equal(t, subscriber(national), subscriber(got), in)
	}
}

// Two distinct numbers never give the same pseudonym: a unique column stays unique.
func Test_Pseudonymize_NoCollision(t *testing.T) {
	p := newTestPseudonymizer(t, 3)
	seen := map[string]string{}
	for i := range 20000 {
		in := fmt.Sprintf("06 %02d %02d %02d %02d", i/1000000%100, i/10000%100, i/100%100, i%100)
		got, err := p.Pseudonymize(in)
		require.NoError(t, err)
		if prev, ok := seen[got]; ok {
			t.Fatalf("%s and %s both give %s", prev, in, got)
		}
		seen[got] = in
	}
}

func Test_Pseudonymize_ShortOrDigitlessValues(t *testing.T) {
	p := newTestPseudonymizer(t, 1)

	for _, in := range []string{"", "N/A", "5", "+"} {
		got, err := p.Pseudonymize(in)
		require.NoError(t, err)
		require.Equal(t, in, got, "nothing to hide in %q", in)
	}

	// Too short to keep a prefix and still hide something: every digit is replaced.
	got, err := p.Pseudonymize("06 12")
	require.NoError(t, err)
	require.Equal(t, shape("06 12"), shape(got))
}
