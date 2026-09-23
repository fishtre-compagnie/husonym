package fpe

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func digitsOf(t *testing.T, s string) []byte {
	t.Helper()
	out := make([]byte, len(s))
	for i, r := range s {
		require.True(t, r >= '0' && r <= '9', "not a digit: %q", r)
		out[i] = byte(r - '0')
	}
	return out
}

func stringOf(digits []byte) string {
	out := make([]byte, len(digits))
	for i, d := range digits {
		out[i] = '0' + d
	}
	return string(out)
}

// The radix-10 samples of NIST's FF1 examples (SP 800-38G).
func Test_FF1_NISTSamples(t *testing.T) {
	const (
		key128 = "2B7E151628AED2A6ABF7158809CF4F3C"
		key192 = "2B7E151628AED2A6ABF7158809CF4F3CEF4359D8D580AA4F"
		key256 = "2B7E151628AED2A6ABF7158809CF4F3CEF4359D8D580AA4F7F036D6F04FC6A94"
		tweak  = "39383736353433323130"
	)
	samples := []struct {
		name, key, tweak, plain, cipher string
	}{
		{"sample 1", key128, "", "0123456789", "2433477484"},
		{"sample 2", key128, tweak, "0123456789", "6124200773"},
		{"sample 4", key192, "", "0123456789", "2830668132"},
		{"sample 7", key256, "", "0123456789", "6657667009"},
		{"sample 8", key256, tweak, "0123456789", "1001623463"},
	}
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			key, err := hex.DecodeString(s.key)
			require.NoError(t, err)
			tw, err := hex.DecodeString(s.tweak)
			require.NoError(t, err)

			ff1, err := NewFF1(key)
			require.NoError(t, err)
			got, err := ff1.Encrypt(digitsOf(t, s.plain), tw)
			require.NoError(t, err)
			require.Equal(t, s.cipher, stringOf(got))
		})
	}
}

// A permutation: over every input of a given length, no two outputs are equal.
func Test_FF1_IsAPermutation(t *testing.T) {
	ff1, err := NewFF1(make([]byte, 32))
	require.NoError(t, err)

	seen := map[string]string{}
	for x := range 1000 {
		in := []byte{byte(x / 100), byte(x / 10 % 10), byte(x % 10)}
		out, err := ff1.Encrypt(in, []byte("6"))
		require.NoError(t, err)
		require.Len(t, out, 3)
		if prev, ok := seen[stringOf(out)]; ok {
			t.Fatalf("%s and %s both give %s", prev, stringOf(in), stringOf(out))
		}
		seen[stringOf(out)] = stringOf(in)
	}
}

func Test_FF1_LeavesItsInputUntouched(t *testing.T) {
	ff1, err := NewFF1(make([]byte, 16))
	require.NoError(t, err)
	in := []byte{1, 2, 3, 4, 5, 6}
	_, err = ff1.Encrypt(in, nil)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3, 4, 5, 6}, in)
}

func Test_FF1_Refuses(t *testing.T) {
	ff1, err := NewFF1(make([]byte, 16))
	require.NoError(t, err)

	_, err = ff1.Encrypt([]byte{4}, nil)
	require.Error(t, err, "a single digit has no two halves")
	_, err = ff1.Encrypt([]byte{1, 12}, nil)
	require.Error(t, err, "12 is not a decimal digit")

	_, err = NewFF1(make([]byte, 10))
	require.Error(t, err, "not an AES key length")
}
