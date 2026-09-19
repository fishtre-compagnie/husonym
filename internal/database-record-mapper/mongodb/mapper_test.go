package mongodb

import (
	"math/big"
	"testing"

	husonym_types "github.com/fishtre-compagnie/husonym/internal/types"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func Test_UnmarshalPrimitives(t *testing.T) {
	input := map[string]any{
		"string": "test",
		"int":    42,
		"bool":   true,
	}
	expectedOutput := map[string]any{
		"string": "test",
		"int":    42,
		"bool":   true,
	}
	expectedKTM := map[string]husonym_types.KeyType{}

	mapper := NewMongoBuilder()

	t.Run("Basic types", func(t *testing.T) {
		output, ktm, err := mapper.MapRecordWithKeyType(input)
		require.NoError(t, err)
		require.Equal(t, expectedOutput, output)
		require.Equal(t, expectedKTM, ktm)
	})

	dec128 := bson.NewDecimal128(3, 14159)
	objectId := bson.NewObjectID()
	input = map[string]any{
		"decimal":   dec128,
		"binary":    bson.Binary{Data: []byte("test")},
		"objectID":  objectId,
		"timestamp": bson.Timestamp{T: 1, I: 1},
	}
	expectedOutput = map[string]any{
		"decimal":   getBigFloat(dec128.String()),
		"binary":    bson.Binary{Data: []byte("test")},
		"objectID":  objectId,
		"timestamp": bson.Timestamp{T: 1, I: 1},
	}
	expectedKTM = map[string]husonym_types.KeyType{
		"decimal":   husonym_types.Decimal128,
		"binary":    husonym_types.Binary,
		"objectID":  husonym_types.ObjectID,
		"timestamp": husonym_types.Timestamp,
	}

	t.Run("BSON types", func(t *testing.T) {
		output, ktm, err := mapper.MapRecordWithKeyType(input)
		require.NoError(t, err)
		require.Equal(t, expectedOutput, output)
		require.Equal(t, expectedKTM, ktm)
	})
}

func getBigFloat(v string) *big.Float {
	f, _, _ := big.ParseFloat(v, 10, 128, big.ToNearestEven)
	return f
}

func Test_ParsePrimitives(t *testing.T) {
	objectId := bson.NewObjectID()
	dec128 := bson.NewDecimal128(3, 14159)
	testCases := []struct {
		name        string
		key         string
		value       any
		expectedKTM map[string]husonym_types.KeyType
		expected    any
	}{
		{
			name:        "Decimal128",
			key:         "decimal",
			value:       dec128,
			expectedKTM: map[string]husonym_types.KeyType{"decimal": husonym_types.Decimal128},
			expected:    getBigFloat(dec128.String()),
		},
		{
			name:        "Binary",
			key:         "binary",
			value:       bson.Binary{Data: []byte("test")},
			expectedKTM: map[string]husonym_types.KeyType{"binary": husonym_types.Binary},
			expected:    bson.Binary{Data: []byte("test")},
		},
		{
			name:        "ObjectID",
			key:         "objectID",
			value:       objectId,
			expectedKTM: map[string]husonym_types.KeyType{"objectID": husonym_types.ObjectID},
			expected:    objectId,
		},
		{
			name:        "Timestamp",
			key:         "timestamp",
			value:       bson.Timestamp{T: 1, I: 1},
			expectedKTM: map[string]husonym_types.KeyType{"timestamp": husonym_types.Timestamp},
			expected:    bson.Timestamp{T: 1, I: 1},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ktm := make(map[string]husonym_types.KeyType)
			result, err := parsePrimitives(tc.key, tc.value, ktm)
			require.NoError(t, err)
			require.Equal(t, tc.expectedKTM, ktm)
			require.Equal(t, tc.expected, result)
		})
	}
}
