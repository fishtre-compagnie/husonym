package javascript_functions

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToScriptAndBack(t *testing.T) {
	const big53 = int64(1) << 53
	row := map[string]any{
		"exact":    big53,
		"above":    big53 + 1,
		"below":    -big53 - 1,
		"uint":     uint64(math.MaxUint64),
		"nested":   map[string]any{"key": int64(math.MaxInt64)},
		"list":     []any{int64(1), big53 + 3},
		"text":     "Durand",
		"smallInt": 42,
	}
	script := ToScript(row).(map[string]any)
	require.Equal(t, big53, script["exact"], "2^53 is exact in JavaScript")
	require.Equal(t, big.NewInt(big53+1), script["above"])
	require.Equal(t, big.NewInt(-big53-1), script["below"])
	require.Equal(t, new(big.Int).SetUint64(math.MaxUint64), script["uint"])
	require.Equal(t, big.NewInt(math.MaxInt64), script["nested"].(map[string]any)["key"])
	require.Equal(t, big.NewInt(big53+3), script["list"].([]any)[1])
	require.Equal(t, int64(big53+1), row["above"], "the row itself is not changed")

	require.Equal(t, row, FromScript(script))

	small := map[string]any{"id": int64(7), "nom": "Durand"}
	require.Equal(t, small, ToScript(small))
}
