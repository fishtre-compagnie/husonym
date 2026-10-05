package connectiondata

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlconnect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// Init statements are given for MySQL and PostgreSQL connections. A SQL Server connection is
// told so before anything is read: the manager it is given has no expectation, so a single call
// to it fails the test.
func Test_GetInitStatements_SqlServerIsRefusedBeforeTheCatalogIsRead(t *testing.T) {
	connection := &mgmtv1alpha1.Connection{
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_MssqlConfig{MssqlConfig: &mgmtv1alpha1.MssqlConnectionConfig{}},
		},
	}
	service := NewSQLConnectionDataService(
		testutil.GetTestLogger(t),
		sqlconnect.NewMockSqlConnector(t),
		sqlmanager.NewMockSqlManagerClient(t),
		connection,
	)

	_, err := service.GetInitStatements(t.Context(), &mgmtv1alpha1.InitStatementOptions{InitSchema: true})

	require.EqualError(t, err, "unsupported connection config")
}

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
