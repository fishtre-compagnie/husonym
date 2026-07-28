package mysql

import (
	"testing"

	husonymtypes "github.com/fishtre-compagnie/husonym/internal/husonym-types"
	"github.com/stretchr/testify/require"
)

func Test_parseMysqlRowValues(t *testing.T) {
	t.Run("Multiple Columns", func(t *testing.T) {
		binaryData := []byte{0x01, 0x02, 0x03}

		values := []any{
			"Hello",
			int64(42),
			true,
			nil,
			[]byte(`{"key": "value"}`),
			binaryData,
		}
		columnNames := []string{
			"text_col", "int_col", "bool_col", "nil_col", "json_col", "binary_col",
			"binary_col",
		}
		cTypes := []string{
			"text",
			"integer",
			"boolean",
			"text",
			"json",
			"binary",
		}
		result, err := parseMysqlRowValues(values, columnNames, cTypes)
		require.NoError(t, err)
		expected := map[string]any{
			"text_col": "Hello",
			"int_col":  int64(42),
			"bool_col": true,
			"nil_col":  nil,
			"json_col": map[string]any{"key": "value"},
			"binary_col": &husonymtypes.Binary{
				BaseType: husonymtypes.BaseType{
					Husonym: husonymtypes.Husonym{
						Version: 1,
						TypeId:  "HUSONYM_BINARY",
					},
				},
				Bytes: binaryData,
			},
		}
		require.Equal(t, expected, result)
	})

	t.Run("JSON Columns", func(t *testing.T) {
		values := []any{
			[]byte(`"Hello"`),
			[]byte(`true`),
			[]byte(`null`),
			[]byte(`42`),
			[]byte(`{"items": ["book", "pen"], "count": 2, "in_stock": true}`),
			[]byte(`[1,2,3]`),
			nil,
		}
		columnNames := []string{
			"text_col",
			"bool_col",
			"null_col",
			"int_col",
			"json_col",
			"array_col",
			"nil_col",
		}
		cTypes := []string{"json", "json", "json", "json", "json", "json", "json"}

		result, err := parseMysqlRowValues(values, columnNames, cTypes)
		require.NoError(t, err)

		expected := map[string]any{
			"text_col": "Hello",
			"bool_col": true,
			"null_col": "null",
			"int_col":  float64(42),
			"json_col": map[string]any{
				"items":    []any{"book", "pen"},
				"count":    float64(2),
				"in_stock": true,
			},
			"array_col": []any{float64(1), float64(2), float64(3)},
			"nil_col":   nil,
		}
		require.Equal(t, expected, result)
	})
}
