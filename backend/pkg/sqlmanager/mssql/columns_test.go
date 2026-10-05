package sqlmanager_mssql

import (
	"testing"

	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func Test_toDatabaseSchemaRow(t *testing.T) {
	t.Parallel()
	value := func(n int) *int { return &n }
	text := func(s string) *string { return &s }

	column := func(change func(*mssql_queries.GetColumnsRow)) *mssql_queries.GetColumnsRow {
		row := &mssql_queries.GetColumnsRow{
			ObjectID: 1, TableSchema: "dbo", TableName: "users", ColumnID: 3, Name: "c",
			TypeSchema: "sys", TypeName: "int", BaseTypeName: "int", MaxLength: 4, Precision: 10,
		}
		change(row)
		return row
	}
	expected := func(change func(*sqlmanager_shared.DatabaseSchemaRow)) *sqlmanager_shared.DatabaseSchemaRow {
		row := &sqlmanager_shared.DatabaseSchemaRow{
			TableSchema: "dbo", TableName: "users", ColumnName: "c", DataType: "int",
			CharacterMaximumLength: -1, NumericPrecision: 10, OrdinalPosition: 3, UpdateAllowed: true,
		}
		change(row)
		return row
	}

	cases := []struct {
		name     string
		row      *mssql_queries.GetColumnsRow
		expected *sqlmanager_shared.DatabaseSchemaRow
	}{
		{
			name:     "a plain column",
			row:      column(func(*mssql_queries.GetColumnsRow) {}),
			expected: expected(func(*sqlmanager_shared.DatabaseSchemaRow) {}),
		},
		{
			name: "an identity tells its seed and its increment",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.IsIdentity, r.IdentitySeed, r.IdentityIncrement = true, "-5", "10"
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.IdentityGeneration = text("IDENTITY(-5,10)")
				r.IdentitySeed, r.IdentityIncrement = value(-5), value(10)
				r.UpdateAllowed = false
			}),
		},
		{
			name: "an identity an int does not hold keeps its text",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.TypeName, r.BaseTypeName, r.Precision = "decimal", "decimal", 38
				r.IsIdentity, r.IdentitySeed, r.IdentityIncrement = true, "99999999999999999999999999999999999999", "1"
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.DataType, r.NumericPrecision = "decimal", 38
				r.IdentityGeneration = text("IDENTITY(99999999999999999999999999999999999999,1)")
				r.IdentityIncrement = value(1)
				r.UpdateAllowed = false
			}),
		},
		{
			name: "nvarchar counts characters",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.TypeName, r.BaseTypeName, r.MaxLength, r.Precision, r.IsNullable = "nvarchar", "nvarchar", 100, 0, true
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.DataType, r.CharacterMaximumLength, r.NumericPrecision, r.IsNullable = "nvarchar", 50, 0, true
			}),
		},
		{
			name: "max has no length",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.TypeName, r.BaseTypeName, r.MaxLength, r.Precision = "varchar", "varchar", -1, 0
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.DataType, r.NumericPrecision = "varchar", 0
			}),
		},
		{
			name: "an alias type is named as declared and measured as stored",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.TypeSchema, r.TypeName, r.BaseTypeName, r.IsUserDefinedType = "sales", "Email", "nvarchar", true
				r.MaxLength, r.Precision = 640, 0
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.DataType, r.CharacterMaximumLength, r.NumericPrecision = "Email", 320, 0
			}),
		},
		{
			name: "a default",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.DefaultID, r.DefaultName, r.DefaultDefinition = 9, "DF", "(NEXT VALUE FOR [dbo].[seq])"
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.ColumnDefault = "(NEXT VALUE FOR [dbo].[seq])"
			}),
		},
		{
			name: "a computed column",
			row: column(func(r *mssql_queries.GetColumnsRow) {
				r.IsComputed, r.ComputedDefinition = true, "([a]+[b])"
			}),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.GeneratedType, r.UpdateAllowed = text("([a]+[b])"), false
			}),
		},
		{
			name: "a period column",
			row:  column(func(r *mssql_queries.GetColumnsRow) { r.GeneratedAlways = 2 }),
			expected: expected(func(r *sqlmanager_shared.DatabaseSchemaRow) {
				r.GeneratedType, r.UpdateAllowed = text("GENERATED ALWAYS AS ROW END"), false
			}),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, toDatabaseSchemaRow(tc.row))
		})
	}
}

func Test_Manager_GetDatabaseTableSchemasBySchemasAndTables(t *testing.T) {
	t.Parallel()
	requested := []*sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "users"}, {Schema: "dbo", Table: "gone"}}

	t.Run("reads the columns of the tables the server finds", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, []mssql_queries.SchemaTable{
			{Schema: "dbo", Table: "users"}, {Schema: "dbo", Table: "gone"},
		}).Return([]*mssql_queries.ResolveTablesRow{{Position: 0, ObjectID: 7, Schema: "dbo", Name: "Users"}}, nil).Once()
		querier.EXPECT().GetColumns(mock.Anything, mock.Anything, []int64{7}).
			Return([]*mssql_queries.GetColumnsRow{{
				ObjectID: 7, TableSchema: "dbo", TableName: "Users", ColumnID: 1, Name: "id",
				TypeName: "int", BaseTypeName: "int", Precision: 10,
			}}, nil).Once()

		rows, err := manager.GetDatabaseTableSchemasBySchemasAndTables(t.Context(), requested)

		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "Users", rows[0].TableName)
	})

	t.Run("reads no column when no table is found", func(t *testing.T) {
		t.Parallel()
		manager, querier := newTestManager(t)
		querier.EXPECT().ResolveTables(mock.Anything, mock.Anything, mock.Anything).
			Return([]*mssql_queries.ResolveTablesRow{}, nil).Once()

		rows, err := manager.GetDatabaseTableSchemasBySchemasAndTables(t.Context(), requested)

		require.NoError(t, err)
		require.NotNil(t, rows)
		require.Empty(t, rows)
	})
}
