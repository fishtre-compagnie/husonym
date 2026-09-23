package transformers

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/worker/pkg/phoneformat"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
	"github.com/stretchr/testify/require"
)

func parsePreserveFormat(t *testing.T, key [32]byte) *bloblang.Executor {
	t.Helper()
	env := bloblang.NewEnvironment()
	require.NoError(t, RegisterTransformPhoneNumberPreserveFormat(env, phoneformat.New(key)))
	ex, err := env.Parse("root = " + BuildPhoneNumberPreserveFormatBloblang("phone"))
	require.NoError(t, err)
	return ex
}

// The mapping reads the column, as a run does: bloblang rebuilds the function for every
// value, and the pseudonym of a number must still not change from one row to the next.
func Test_TransformPhoneNumberPreserveFormat_ConsistentAcrossRows(t *testing.T) {
	ex := parsePreserveFormat(t, [32]byte{1})

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

// Two streams of the same run share the key of its consistency scope, and two tables of a job
// may run on two workers: the same number has to come out the same on both.
func Test_TransformPhoneNumberPreserveFormat_SameKeySameOutput(t *testing.T) {
	key := [32]byte{7}
	one, err := parsePreserveFormat(t, key).Query(map[string]any{"phone": "06 12 34 56 78"})
	require.NoError(t, err)
	other, err := parsePreserveFormat(t, key).Query(map[string]any{"phone": "06 12 34 56 78"})
	require.NoError(t, err)
	require.Equal(t, one, other)

	elsewhere, err := parsePreserveFormat(t, [32]byte{8}).
		Query(map[string]any{"phone": "06 12 34 56 78"})
	require.NoError(t, err)
	require.NotEqual(t, one, elsewhere)
}

// A deployment with no key to derive from fails the jobs that ask for the permutation, and says
// what to set — and no other job.
func Test_TransformPhoneNumberPreserveFormat_NoKey(t *testing.T) {
	env := bloblang.NewEnvironment()
	require.NoError(t, RegisterTransformPhoneNumberPreserveFormat(env, nil))

	ex, err := env.Parse("root = " + BuildPhoneNumberPreserveFormatBloblang("phone"))
	require.NoError(t, err)
	_, err = ex.Query(map[string]any{"phone": "0612345678"})
	require.ErrorContains(t, err, "ANONYMIZATION_CONSISTENCY_KEY")

	// The column of another mapping is not held back by it.
	other, err := env.Parse("root = this.phone")
	require.NoError(t, err)
	res, err := other.Query(map[string]any{"phone": "0612345678"})
	require.NoError(t, err)
	require.Equal(t, "0612345678", res)
}
