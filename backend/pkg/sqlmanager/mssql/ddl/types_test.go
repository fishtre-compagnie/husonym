package ddl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_systemType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		typeName  string
		maxLength int
		precision int
		scale     int
		expected  string
	}{
		{name: "bigint", typeName: "bigint", maxLength: 8, precision: 19, expected: "bigint"},
		{name: "int", typeName: "int", maxLength: 4, precision: 10, expected: "int"},
		{name: "smallint", typeName: "smallint", maxLength: 2, precision: 5, expected: "smallint"},
		{name: "tinyint", typeName: "tinyint", maxLength: 1, precision: 3, expected: "tinyint"},
		{name: "bit", typeName: "bit", maxLength: 1, precision: 1, expected: "bit"},
		{name: "money", typeName: "money", maxLength: 8, precision: 19, scale: 4, expected: "money"},
		{name: "smallmoney", typeName: "smallmoney", maxLength: 4, precision: 10, scale: 4, expected: "smallmoney"},
		{name: "decimal", typeName: "decimal", maxLength: 17, precision: 38, scale: 10, expected: "decimal(38,10)"},
		{name: "numeric without scale", typeName: "numeric", maxLength: 5, precision: 5, scale: 0, expected: "numeric(5,0)"},
		{name: "float 24", typeName: "real", maxLength: 4, precision: 24, expected: "real"},
		{name: "float 53", typeName: "float", maxLength: 8, precision: 53, expected: "float(53)"},
		{name: "char", typeName: "char", maxLength: 10, expected: "char(10)"},
		{name: "varchar", typeName: "varchar", maxLength: 50, expected: "varchar(50)"},
		{name: "varchar max", typeName: "varchar", maxLength: -1, expected: "varchar(max)"},
		{name: "binary", typeName: "binary", maxLength: 10, expected: "binary(10)"},
		{name: "varbinary", typeName: "varbinary", maxLength: 50, expected: "varbinary(50)"},
		{name: "varbinary max", typeName: "varbinary", maxLength: -1, expected: "varbinary(max)"},
		{name: "nchar", typeName: "nchar", maxLength: 20, expected: "nchar(10)"},
		{name: "nvarchar", typeName: "nvarchar", maxLength: 100, expected: "nvarchar(50)"},
		{name: "nvarchar max", typeName: "nvarchar", maxLength: -1, expected: "nvarchar(max)"},
		{name: "sysname", typeName: "sysname", maxLength: 256, expected: "sysname"},
		{name: "date", typeName: "date", maxLength: 3, precision: 10, expected: "date"},
		{name: "datetime", typeName: "datetime", maxLength: 8, precision: 23, scale: 3, expected: "datetime"},
		{name: "smalldatetime", typeName: "smalldatetime", maxLength: 4, precision: 16, expected: "smalldatetime"},
		{name: "time 0", typeName: "time", maxLength: 3, precision: 8, scale: 0, expected: "time(0)"},
		{name: "time 7", typeName: "time", maxLength: 5, precision: 16, scale: 7, expected: "time(7)"},
		{name: "datetime2", typeName: "datetime2", maxLength: 7, precision: 23, scale: 3, expected: "datetime2(3)"},
		{name: "datetimeoffset", typeName: "datetimeoffset", maxLength: 9, precision: 32, scale: 5, expected: "datetimeoffset(5)"},
		{name: "uniqueidentifier", typeName: "uniqueidentifier", maxLength: 16, expected: "uniqueidentifier"},
		{name: "sql_variant", typeName: "sql_variant", maxLength: 8016, expected: "sql_variant"},
		{name: "text", typeName: "text", maxLength: 16, expected: "text"},
		{name: "ntext", typeName: "ntext", maxLength: 16, expected: "ntext"},
		{name: "image", typeName: "image", maxLength: 16, expected: "image"},
		{name: "timestamp", typeName: "timestamp", maxLength: 8, expected: "timestamp"},
		{name: "xml", typeName: "xml", maxLength: -1, expected: "xml"},
		{name: "hierarchyid", typeName: "hierarchyid", maxLength: 892, expected: "hierarchyid"},
		{name: "geometry", typeName: "geometry", maxLength: -1, expected: "geometry"},
		{name: "geography", typeName: "geography", maxLength: -1, expected: "geography"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.True(t, isSystemType(tc.typeName))
			require.Equal(t, tc.expected, systemType(tc.typeName, tc.maxLength, tc.precision, tc.scale))
		})
	}
}

func Test_systemType_UnknownName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"vector", "json", "", "INT", "varchar(max)"} {
		require.Falsef(t, isSystemType(name), "%q is not in the list of system types", name)
		require.Empty(t, systemType(name, 8, 0, 0))
	}
}

func Test_columnType(t *testing.T) {
	t.Parallel()

	t.Run("a system type is written with its numbers", func(t *testing.T) {
		t.Parallel()
		column := &Column{TypeSchema: "sys", TypeName: "nvarchar", BaseTypeName: "nvarchar", MaxLength: 80}
		require.Equal(t, "nvarchar(40)", columnType(column))
	})

	t.Run("an alias type is written by its name alone, with its schema", func(t *testing.T) {
		t.Parallel()
		column := &Column{
			TypeSchema:        "sales",
			TypeName:          "Email]Address",
			BaseTypeName:      "nvarchar",
			IsUserDefinedType: true,
			MaxLength:         640,
		}
		require.Equal(t, "[sales].[Email]]Address]", columnType(column))
	})
}

func Test_characterLength(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		baseType  string
		maxLength int
		expected  int
	}{
		{name: "nvarchar counts characters", baseType: "nvarchar", maxLength: 100, expected: 50},
		{name: "nchar counts characters", baseType: "nchar", maxLength: 20, expected: 10},
		{name: "varchar counts bytes", baseType: "varchar", maxLength: 50, expected: 50},
		{name: "char counts bytes", baseType: "char", maxLength: 10, expected: 10},
		{name: "binary counts bytes", baseType: "binary", maxLength: 10, expected: 10},
		{name: "varbinary counts bytes", baseType: "varbinary", maxLength: 50, expected: 50},
		{name: "max", baseType: "nvarchar", maxLength: -1, expected: -1},
		{name: "a type without length", baseType: "int", maxLength: 4, expected: -1},
		{name: "text holds a pointer", baseType: "text", maxLength: 16, expected: -1},
		{name: "xml", baseType: "xml", maxLength: -1, expected: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, CharacterLength(tc.baseType, tc.maxLength))
		})
	}
}
