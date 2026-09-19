package continuation_token

import (
	"encoding/base64"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_ContinuationToken_roundTrip(t *testing.T) {
	values := []any{
		nil,
		true,
		int64(math.MinInt64),
		int64(1)<<60 + 1, // beyond 2^53: a float64 would merge it with its neighbors
		uint64(math.MaxUint64),
		1.7976931348623157e308,
		"1234567890123.0001",
		"",
		[]byte{0xff, 0xfe, 0x00, 0x80},
		time.Date(2024, 2, 29, 23, 59, 59, 999999000, time.UTC),
	}
	encoded, err := NewFromContents(NewContents(values)).Encode()
	require.NoError(t, err)

	decoded, err := FromTokenString(encoded)
	require.NoError(t, err)
	require.Equal(t, values, decoded.Contents.LastReadOrderValues)
}

func Test_ContinuationToken_smallIntegersAreCanonical(t *testing.T) {
	encoded, err := NewFromContents(NewContents([]any{int32(7), uint16(9), float32(0.5)})).Encode()
	require.NoError(t, err)
	decoded, err := FromTokenString(encoded)
	require.NoError(t, err)
	require.Equal(t, []any{int64(7), uint64(9), 0.5}, decoded.Contents.LastReadOrderValues)
}

func Test_ContinuationToken_refusesWhatItCannotCarry(t *testing.T) {
	_, err := NewFromContents(NewContents([]any{map[string]any{"bytes": "AA=="}})).Encode()
	require.ErrorContains(t, err, "cannot be carried exactly")
}

func Test_ContinuationToken_readsTokensWrittenBeforeTyping(t *testing.T) {
	legacy := base64.StdEncoding.EncodeToString([]byte(`{"contents":{"lastReadOrderValues":[42,"abc",null]}}`))
	decoded, err := FromTokenString(legacy)
	require.NoError(t, err)
	require.Equal(t, []any{float64(42), "abc", nil}, decoded.Contents.LastReadOrderValues)
}

func Test_FromTokenString_empty(t *testing.T) {
	token, err := FromTokenString("")
	require.NoError(t, err)
	require.Nil(t, token)
}
