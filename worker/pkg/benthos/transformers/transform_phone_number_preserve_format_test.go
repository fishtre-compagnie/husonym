package transformers

import (
	"testing"

	"github.com/redpanda-data/benthos/v4/public/bloblang"
	"github.com/stretchr/testify/require"
)

// The mapping reads the column, as a run does: bloblang rebuilds the function for every
// value, and the pseudonym of a number must still not change from one row to the next.
func Test_TransformPhoneNumberPreserveFormat_ConsistentAcrossRows(t *testing.T) {
	ex, err := bloblang.Parse("root = " + BuildPhoneNumberPreserveFormatBloblang("phone"))
	require.NoError(t, err)

	query := func(phone any) any {
		t.Helper()
		res, err := ex.Query(map[string]any{"phone": phone})
		require.NoError(t, err)
		return res
	}

	first := query("06 12 34 56 78")
	require.Equal(t, first, query("06 12 34 56 78"))
	require.NotEqual(t, first, query("06 12 34 56 79"))

	s, ok := first.(string)
	require.True(t, ok, "got %T", first)
	require.Len(t, s, len("06 12 34 56 78"))
	require.Equal(t, "06 ", s[:3])

	require.Nil(t, query(nil))
}
