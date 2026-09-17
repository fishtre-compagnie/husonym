package oracle

import (
	"strings"
	"testing"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/stretchr/testify/require"
)

func Test_CanonicalValue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  any
		binary bool
		want   string
	}{
		{"null", nil, false, `\N`},
		{"seed integer", int64(-7), false, "-7"},
		{"seed unsigned above int64", uint64(1<<63 + 1), false, "9223372036854775809"},
		{"seed text", "1.10", false, "1.10"},
		{"text read from the database", []byte("1.10"), false, "1.10"},
		{"seed bytes", []byte{0x00, 0xff}, true, "x'00ff'"},
		{"bytes read from the database", []byte{0x00, 0xff}, true, "x'00ff'"},
	} {
		got, err := CanonicalValue(tc.value, tc.binary)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.want, got, tc.name)
	}
	_, err := CanonicalValue(1.5, false)
	require.Error(t, err, "floats are seeded as text")
}

func Test_RowKey(t *testing.T) {
	require.Equal(t, "1\x1fa", RowKey([]string{"1", "a"}))
	require.NotEqual(t, RowKey([]string{"1a", ""}), RowKey([]string{"1", "a"}))

	long := RowKey([]string{strings.Repeat("x", 300)})
	require.True(t, strings.HasPrefix(long, "sha256:"))
	require.LessOrEqual(t, len(long), 255)
}

func Test_Writer_Add(t *testing.T) {
	w := NewWriter("case")
	require.NoError(t, w.Add("T", "k", cases.Kept()))
	require.NoError(t, w.Add("T", "k", cases.Kept()))
	require.Equal(t, 2, w.rows["T"]["k"].Occurrences)
	require.Error(t, w.Add("T", "k", cases.Dropped()), "identical rows cannot expect different outcomes")
}
