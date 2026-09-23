package connectiondata

import (
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// The same conversion feeds the whole-database reader and the single-table one. They used to
// have one each and they disagreed, twice: the length went missing first, then the generated and
// identity markers. Each time the loss was silent, and each time it surfaced as a drafted rule
// that overflowed a column or rewrote one the database writes itself. So this pins every field.
func Test_toDatabaseColumn(t *testing.T) {
	generated := "s"
	identity := "a"

	t.Run("carries everything the row holds", func(t *testing.T) {
		column := toDatabaseColumn(&sqlmanager_shared.DatabaseSchemaRow{
			TableSchema:            "public",
			TableName:              "users",
			ColumnName:             "email",
			DataType:               "varchar",
			IsNullable:             true,
			ColumnDefault:          "''",
			CharacterMaximumLength: 255,
			GeneratedType:          &generated,
			IdentityGeneration:     &identity,
		})

		require.Equal(t, "public", column.GetSchema())
		require.Equal(t, "users", column.GetTable())
		require.Equal(t, "email", column.GetColumn())
		require.Equal(t, "varchar", column.GetDataType())
		require.Equal(t, "YES", column.GetIsNullable())
		require.Equal(t, "''", column.GetColumnDefault())
		require.Equal(t, int32(255), column.GetCharacterMaximumLength())
		require.Equal(t, "s", column.GetGeneratedType())
		require.Equal(t, "a", column.GetIdentityGeneration())
	})

	t.Run("absent rather than zero when the type bounds nothing", func(t *testing.T) {
		// The drivers write -1 here. Zero would read as "a bound of nothing", and a rule told to
		// produce at most zero characters is worse than one told nothing at all.
		column := toDatabaseColumn(&sqlmanager_shared.DatabaseSchemaRow{
			ColumnName:             "id",
			DataType:               "integer",
			CharacterMaximumLength: -1,
		})
		require.Nil(t, column.CharacterMaximumLength)
		require.Equal(t, "NO", column.GetIsNullable())
		require.Nil(t, column.ColumnDefault)
	})
}
