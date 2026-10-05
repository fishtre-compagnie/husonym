package sqlmanager_mssql

import (
	"testing"

	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_referencesItsOwnColumns(t *testing.T) {
	t.Parallel()

	t.Run("other columns of the table are a real reference", func(t *testing.T) {
		t.Parallel()
		require.False(t, referencesItsOwnColumns([]string{"ManagerId"}, []string{"Id"}))
	})

	t.Run("a column that references itself is not", func(t *testing.T) {
		t.Parallel()
		require.True(t, referencesItsOwnColumns([]string{"Id"}, []string{"Id"}))
	})

	t.Run("several columns that are all among those referenced are not", func(t *testing.T) {
		t.Parallel()
		require.True(t, referencesItsOwnColumns([]string{"Id", "SubId"}, []string{"SubId", "Id"}))
	})

	t.Run("one column outside those referenced makes a real reference", func(t *testing.T) {
		t.Parallel()
		require.False(t, referencesItsOwnColumns([]string{"Id", "SubId"}, []string{"Id", "DifferentId"}))
	})

	t.Run("no column at all is not a reference", func(t *testing.T) {
		t.Parallel()
		require.True(t, referencesItsOwnColumns([]string{}, []string{}))
	})
}

func Test_Manager_GetTableConstraintsBySchema(t *testing.T) {
	t.Parallel()
	manager, querier := newTestManager(t)
	schemas := []string{"dbo", "hr"}

	index := func(table int64, tableName string, id int, name string, column string, ordinal int, change func(*mssql_queries.GetIndexesRow)) *mssql_queries.GetIndexesRow {
		row := &mssql_queries.GetIndexesRow{
			ObjectID: table, TableSchema: "dbo", TableName: tableName,
			IndexID: id, Name: name, Type: 2, IsUnique: true,
			IndexColumnID: ordinal, ColumnName: column, KeyOrdinal: ordinal,
		}
		change(row)
		return row
	}
	primary := func(row *mssql_queries.GetIndexesRow) { row.IsPrimaryKey = true }
	constraint := func(row *mssql_queries.GetIndexesRow) { row.IsUniqueConstraint = true }
	plain := func(*mssql_queries.GetIndexesRow) {}

	querier.EXPECT().GetIndexesBySchemas(mock.Anything, mock.Anything, schemas).
		Return([]*mssql_queries.GetIndexesRow{
			// The key order of the primary key is not the order its columns are listed in.
			index(1, "Employee", 1, "PK_Employee", "b", 2, func(row *mssql_queries.GetIndexesRow) {
				primary(row)
				row.IndexColumnID = 1
			}),
			index(1, "Employee", 1, "PK_Employee", "a", 1, func(row *mssql_queries.GetIndexesRow) {
				primary(row)
				row.IndexColumnID = 2
			}),
			index(1, "Employee", 2, "UQ_z", "x, y", 1, constraint),
			index(1, "Employee", 3, "UQ_a", "email", 1, constraint),
			index(1, "Employee", 4, "UX_second", "badge", 1, plain),
			index(1, "Employee", 5, "UX_first", "phone", 1, plain),
			index(1, "Employee", 5, "UX_first", "note", 2, func(row *mssql_queries.GetIndexesRow) {
				row.KeyOrdinal, row.IsIncluded = 0, true
			}),
			index(1, "Employee", 6, "IX_not_unique", "name", 1, func(row *mssql_queries.GetIndexesRow) {
				row.IsUnique = false
			}),
		}, nil).Once()

	key := func(id int64, name string, referenced int64, referencedTable, column, referencedColumn string, nullable bool) *mssql_queries.GetForeignKeysRow {
		return &mssql_queries.GetForeignKeysRow{
			ObjectID: 1, TableSchema: "dbo", TableName: "Employee",
			ConstraintID: id, Name: name, ReferencedID: referenced, ReferencedSchema: "dbo", ReferencedTable: referencedTable,
			ColumnName: column, ColumnIsNullable: nullable, ReferencedColumn: referencedColumn,
		}
	}
	querier.EXPECT().GetForeignKeysBySchemas(mock.Anything, mock.Anything, schemas).
		Return([]*mssql_queries.GetForeignKeysRow{
			key(10, "FK_department", 2, "Department", "dept, id", "id", false),
			key(10, "FK_department", 2, "Department", "region", "region", true),
			key(11, "FK_manager", 1, "Employee", "ManagerId", "a", true),
			key(12, "FK_self", 1, "Employee", "a", "a", false),
		}, nil).Once()

	constraints, err := manager.GetTableConstraintsBySchema(t.Context(), schemas)

	require.NoError(t, err)
	require.Equal(t, &sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{"dbo.Employee": {"a", "b"}},
		UniqueConstraints:     map[string][][]string{"dbo.Employee": {{"email"}, {"x, y"}}},
		UniqueIndexes:         map[string][][]string{"dbo.Employee": {{"badge"}, {"phone"}}},
		ForeignKeyConstraints: map[string][]*sqlmanager_shared.ForeignConstraint{
			"dbo.Employee": {
				{
					Columns:     []string{"dept, id", "region"},
					NotNullable: []bool{true, false},
					ForeignKey:  &sqlmanager_shared.ForeignKey{Table: "dbo.Department", Columns: []string{"id", "region"}},
				},
				{
					Columns:     []string{"ManagerId"},
					NotNullable: []bool{false},
					ForeignKey:  &sqlmanager_shared.ForeignKey{Table: "dbo.Employee", Columns: []string{"a"}},
				},
			},
		},
	}, constraints)
}
