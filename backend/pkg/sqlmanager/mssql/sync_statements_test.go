package sqlmanager_mssql

import (
	"math"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

func Test_BuildMssqlDeleteStatement(t *testing.T) {
	t.Parallel()
	require.Equal(t, "DELETE FROM [public].[users];", BuildMssqlDeleteStatement("public", "users"))
	require.Equal(
		t,
		"DELETE FROM [sales].[Order ]] Lines];",
		BuildMssqlDeleteStatement("sales", "Order ] Lines"),
	)
	require.Equal(t, "DELETE FROM [a.b].[it's];", BuildMssqlDeleteStatement("a.b", "it's"))
}

func Test_BuildMssqlSetIdentityInsertStatement(t *testing.T) {
	t.Parallel()
	require.Equal(
		t,
		"SET IDENTITY_INSERT [sales].[Order ]] Lines] ON;",
		BuildMssqlSetIdentityInsertStatement("sales", "Order ] Lines", true),
	)
	require.Equal(
		t,
		"SET IDENTITY_INSERT [dbo].[users] OFF;",
		BuildMssqlSetIdentityInsertStatement("dbo", "users", false),
	)
}

func Test_BuildMssqlIdentityColumnResetCurrent(t *testing.T) {
	t.Parallel()
	require.Equal(
		t,
		"DBCC CHECKIDENT (N'[sales].[Order ]] Lines]', RESEED)",
		BuildMssqlIdentityColumnResetCurrent("sales", "Order ] Lines"),
	)
	require.Equal(
		t,
		"DBCC CHECKIDENT (N'[it''s].[a.b]', RESEED)",
		BuildMssqlIdentityColumnResetCurrent("it's", "a.b"),
	)
}

func Test_BuildMssqlIdentityColumnResetStatement(t *testing.T) {
	t.Parallel()
	value := func(n int) *int { return &n }

	t.Run("reseeds one increment below the seed, so that the next value is the seed", func(t *testing.T) {
		t.Parallel()
		require.Equal(
			t,
			"IF EXISTS (SELECT 1 FROM sys.identity_columns WHERE object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U') AND last_value IS NOT NULL)\n"+
				"DBCC CHECKIDENT (N'[sales].[Order ]] Lines]', RESEED, 0)",
			BuildMssqlIdentityColumnResetStatement("sales", "Order ] Lines", value(1), value(1)),
		)
	})

	t.Run("with another seed and increment", func(t *testing.T) {
		t.Parallel()
		require.Contains(
			t,
			BuildMssqlIdentityColumnResetStatement("dbo", "t", value(-5), value(10)),
			"DBCC CHECKIDENT (N'[dbo].[t]', RESEED, -15)",
		)
		require.Contains(
			t,
			BuildMssqlIdentityColumnResetStatement("dbo", "t", value(100), value(-1)),
			"DBCC CHECKIDENT (N'[dbo].[t]', RESEED, 101)",
		)
	})

	t.Run("a seed at the least value an integer holds is reseeded below it, never wrapped", func(t *testing.T) {
		t.Parallel()
		// The server refuses a value its type does not hold: the statement fails, whatever the
		// type of the column.
		require.Contains(
			t,
			BuildMssqlIdentityColumnResetStatement("dbo", "t", value(math.MinInt64), value(1)),
			"DBCC CHECKIDENT (N'[dbo].[t]', RESEED, -9223372036854775809)",
		)
		require.Contains(
			t,
			BuildMssqlIdentityColumnResetStatement("dbo", "t", value(math.MaxInt64), value(-1)),
			"DBCC CHECKIDENT (N'[dbo].[t]', RESEED, 9223372036854775808)",
		)
		require.Contains(
			t,
			BuildMssqlIdentityColumnResetStatement("dbo", "t", value(math.MinInt32), value(1)),
			"DBCC CHECKIDENT (N'[dbo].[t]', RESEED, -2147483649)",
		)
	})

	t.Run("raises the value to the greatest of the column when the seed is not known", func(t *testing.T) {
		t.Parallel()
		expected := BuildMssqlIdentityColumnResetCurrent("dbo", "t")
		require.Equal(t, expected, BuildMssqlIdentityColumnResetStatement("dbo", "t", nil, value(1)))
		require.Equal(t, expected, BuildMssqlIdentityColumnResetStatement("dbo", "t", value(1), nil))
	})
}

func Test_GetMssqlColumnOverrideAndResetProperties(t *testing.T) {
	t.Parallel()
	identity := "IDENTITY(1,1)"
	cases := []struct {
		name          string
		column        *sqlmanager_shared.DatabaseSchemaRow
		needsOverride bool
		needsReset    bool
	}{
		{
			name:          "an identity column needs both",
			column:        &sqlmanager_shared.DatabaseSchemaRow{IdentityGeneration: &identity},
			needsOverride: true, needsReset: true,
		},
		{
			name:       "a default that draws from a sequence needs a reset",
			column:     &sqlmanager_shared.DatabaseSchemaRow{ColumnDefault: "(NEXT VALUE FOR [dbo].[seq])"},
			needsReset: true,
		},
		{
			name:       "whatever the case the default is written in",
			column:     &sqlmanager_shared.DatabaseSchemaRow{ColumnDefault: "(next value for [dbo].[seq])"},
			needsReset: true,
		},
		{
			name:   "another default needs neither",
			column: &sqlmanager_shared.DatabaseSchemaRow{ColumnDefault: "((1))"},
		},
		{
			name:   "a plain column needs neither",
			column: &sqlmanager_shared.DatabaseSchemaRow{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			needsOverride, needsReset := GetMssqlColumnOverrideAndResetProperties(tc.column)
			require.Equal(t, tc.needsOverride, needsOverride)
			require.Equal(t, tc.needsReset, needsReset)
		})
	}
}
