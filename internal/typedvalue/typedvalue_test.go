package typedvalue

import (
	"github.com/jackc/pgx/v5/pgtype"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_roundTrip(t *testing.T) {
	for _, value := range []any{
		nil, true, int64(math.MinInt64), int64(1)<<60 + 1, uint64(math.MaxUint64), 0.1,
		"1234567890123.0001", "", []byte{0xff, 0x00}, time.Date(2024, 2, 29, 23, 59, 59, 999999000, time.UTC),
	} {
		encoded, err := Marshal(value)
		require.NoError(t, err)
		decoded, err := Unmarshal(encoded)
		require.NoError(t, err)
		require.Equal(t, value, decoded)
	}
}

func Test_Marshal_refusesWhatItCannotCarry(t *testing.T) {
	_, err := Marshal(map[string]any{"a": 1})
	require.ErrorContains(t, err, "cannot be carried exactly")
}

func Test_Unmarshal_bareJSON(t *testing.T) {
	decoded, err := Unmarshal([]byte(`42`))
	require.NoError(t, err)
	require.Equal(t, float64(42), decoded)
}

// A driver that answers with a wrapper of its own — pgx gives *pgtype.Timestamp for a
// PostgreSQL timestamp — used to stop a run dead at the end of its first page: the key of
// the last row read could not enter the continuation token.
func Test_Marshal_driverWrapper(t *testing.T) {
	instant := time.Date(2024, 2, 29, 23, 59, 59, 999000000, time.UTC)
	raw, err := Marshal(&pgtype.Timestamp{Time: instant, Valid: true})
	require.NoError(t, err)

	got, err := Unmarshal(raw)
	require.NoError(t, err)
	require.IsType(t, time.Time{}, got)
	require.True(t, instant.Equal(got.(time.Time)), "the instant must come back as it went in")

	_, err = Marshal(&pgtype.Timestamp{Valid: false})
	require.NoError(t, err, "a wrapper holding nothing carries a NULL")
}
