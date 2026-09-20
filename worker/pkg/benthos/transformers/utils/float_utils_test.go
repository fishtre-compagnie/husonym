package transformer_utils

import (
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/gotypeutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/rng"
	"github.com/stretchr/testify/require"
)

func Test_GenerateRandomFloat64WithInclusiveBoundsMinEqualMax(t *testing.T) {
	v1 := float64(2.2)
	v2 := float64(2.2)

	val, err := GenerateRandomFloat64WithInclusiveBounds(rng.New(time.Now().UnixNano()), v1, v2)
	require.NoError(t, err, "Did not expect an error when min == max")
	require.Equal(t, v1, val, "actual value to be equal to min/max")
}

func Test_GenerateRandomFloat64WithInclusiveBoundsPositive(t *testing.T) {
	v1 := float64(2.2)
	v2 := float64(5.2)

	val, err := GenerateRandomFloat64WithInclusiveBounds(rng.New(time.Now().UnixNano()), v1, v2)
	require.NoError(t, err, "Did not expect an error for valid range")
	require.True(t, val >= v1 && val <= v2, "actual value to be within the range")
}

func Test_GenerateRandomFloat64WithInclusiveBoundsNegative(t *testing.T) {
	v1 := float64(-2.2)
	v2 := float64(-5.2)

	val, err := GenerateRandomFloat64WithInclusiveBounds(rng.New(time.Now().UnixNano()), v1, v2)

	require.NoError(t, err, "Did not expect an error for valid range")
	require.True(t, val <= v1 && val >= v2, "actual value to be within the range")
}

func Test_GenerateRandomFloat64WithBoundsNegativeToPositive(t *testing.T) {
	v1 := float64(-2.3)
	v2 := float64(9.32)

	val, err := GenerateRandomFloat64WithInclusiveBounds(rng.New(time.Now().UnixNano()), v1, v2)

	require.NoError(t, err, "Did not expect an error for valid range")
	require.True(t, val >= v1 && val <= v2, "actual value to be within the range")
}

func Test_AnyToFloat64(t *testing.T) {
	inputs := []any{
		"1.0",
		[]byte("1.0"),
		int(1),
		gotypeutil.ToPtr(int(1)),
		int8(1),
		gotypeutil.ToPtr(int8(1)),
		int16(1),
		gotypeutil.ToPtr(int16(1)),
		int32(1),
		gotypeutil.ToPtr(int32(1)),
		int64(1),
		gotypeutil.ToPtr(int64(1)),
		uint(1),
		gotypeutil.ToPtr(uint(1)),
		uint8(1),
		gotypeutil.ToPtr(uint8(1)),
		uint16(1),
		gotypeutil.ToPtr(uint16(1)),
		uint32(1),
		gotypeutil.ToPtr(uint32(1)),
		uint64(1),
		gotypeutil.ToPtr(uint64(1)),
		float32(1),
		gotypeutil.ToPtr(float32(1)),
		float64(1),
		gotypeutil.ToPtr(float64(1)),
		true,
		false,
		gotypeutil.ToPtr(true),
	}
	for _, input := range inputs {
		output, err := AnyToFloat64(input)
		require.NoError(t, err)
		require.NotNil(t, output)
	}
}
