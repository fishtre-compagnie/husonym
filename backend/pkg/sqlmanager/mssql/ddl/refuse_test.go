package ddl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// intColumn is a plain int column.
func intColumn(id int, name string) *Column {
	return &Column{
		ColumnID: id, Name: name,
		TypeSchema: "sys", TypeName: "int", BaseTypeName: "int",
		MaxLength: 4, Precision: 10, IsNullable: true,
	}
}

// plainTable is a table of one int column.
func plainTable(id int64, schema, name string) *Table {
	return &Table{
		ObjectID: id, Schema: schema, Name: name,
		Columns: []*Column{intColumn(1, "id")},
	}
}

func Test_Build_Refusals(t *testing.T) {
	t.Parallel()

	withColumn := func(change func(c *Column)) *Snapshot {
		table := plainTable(1, "dbo", "t")
		change(table.Columns[0])
		return &Snapshot{Tables: []*Table{table}}
	}
	withTable := func(change func(table *Table)) *Snapshot {
		table := plainTable(1, "dbo", "t")
		change(table)
		return &Snapshot{Tables: []*Table{table}}
	}
	withIndex := func(index *Index) *Snapshot {
		return withTable(func(table *Table) {
			index.Columns = []*IndexColumn{{Name: "id", KeyOrdinal: 1}}
			table.Indexes = []*Index{index}
		})
	}
	// calledFunction is a table whose check calls a function.
	calledFunction := func(function *Module, more ...*Dependency) *Snapshot {
		table := plainTable(1, "dbo", "t")
		table.Checks = []*CheckConstraint{{Name: "CK", Definition: "([fn].[f]([id])>(0))"}}
		function.ObjectID, function.Schema, function.Name = 10, "fn", "f"
		return &Snapshot{
			Tables:  []*Table{table},
			Modules: []*Module{function},
			Dependencies: append([]*Dependency{{
				ReferencingID: 5, ReferencingType: TypeCheck, ReferencingParentID: 1,
				ReferencedClass: ClassObject, ReferencedID: 10, ReferencedType: function.Type,
				ReferencedSchema: "fn", ReferencedName: "f",
			}}, more...),
		}
	}

	cases := []struct {
		name     string
		snapshot *Snapshot
		expected Refusal
	}{
		{
			name:     "memory-optimized table",
			snapshot: withTable(func(table *Table) { table.IsMemoryOptimized = true }),
			expected: Refusal{Object: "[dbo].[t]", Reason: "memory-optimized table"},
		},
		{
			name:     "graph node table",
			snapshot: withTable(func(table *Table) { table.IsNode = true }),
			expected: Refusal{Object: "[dbo].[t]", Reason: "graph node table"},
		},
		{
			name:     "graph edge table",
			snapshot: withTable(func(table *Table) { table.IsEdge = true }),
			expected: Refusal{Object: "[dbo].[t]", Reason: "graph edge table"},
		},
		{
			name:     "external table",
			snapshot: withTable(func(table *Table) { table.IsExternal = true }),
			expected: Refusal{Object: "[dbo].[t]", Reason: "external table"},
		},
		{
			name:     "FileTable",
			snapshot: withTable(func(table *Table) { table.IsFileTable = true }),
			expected: Refusal{Object: "[dbo].[t]", Reason: "FileTable"},
		},
		{
			name:     "ledger table",
			snapshot: withColumn(func(c *Column) { c.GeneratedAlways = 5 }),
			expected: Refusal{Object: "[dbo].[t]", Reason: "ledger table"},
		},
		{
			name: "typed xml",
			snapshot: withColumn(func(c *Column) {
				c.TypeName, c.BaseTypeName, c.MaxLength, c.HasXMLCollection = "xml", "xml", -1, true
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "xml column bound to an XML schema collection"},
		},
		{
			name: "alias type with a bound rule",
			snapshot: withColumn(func(c *Column) {
				c.TypeSchema, c.TypeName, c.IsUserDefinedType, c.TypeHasRule = "dbo", "age", true, true
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "alias type [dbo].[age] has a bound rule"},
		},
		{
			name: "alias type with a bound default",
			snapshot: withColumn(func(c *Column) {
				c.TypeSchema, c.TypeName, c.IsUserDefinedType, c.TypeHasDefault = "dbo", "age", true, true
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "alias type [dbo].[age] has a bound default"},
		},
		{
			name:     "rule bound to a column",
			snapshot: withColumn(func(c *Column) { c.HasRule = true }),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "a rule is bound to the column"},
		},
		{
			name:     "stand-alone default bound to a column",
			snapshot: withColumn(func(c *Column) { c.HasDefault = true }),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "a stand-alone default is bound to the column"},
		},
		{
			name: "CLR user-defined type",
			snapshot: withColumn(func(c *Column) {
				c.TypeSchema, c.TypeName, c.BaseTypeName = "dbo", "Point", ""
				c.IsUserDefinedType, c.IsAssemblyType = true, true
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "CLR user-defined type [dbo].[Point]"},
		},
		{
			name:     "unknown system type",
			snapshot: withColumn(func(c *Column) { c.TypeName, c.BaseTypeName = "vector", "vector" }),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "type vector is not supported"},
		},
		{
			name: "alias of an unknown system type",
			snapshot: withColumn(func(c *Column) {
				c.TypeSchema, c.TypeName, c.BaseTypeName, c.IsUserDefinedType = "dbo", "doc", "json", true
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "type json is not supported"},
		},
		{
			name: "ANSI_PADDING OFF",
			snapshot: withColumn(func(c *Column) {
				c.TypeName, c.BaseTypeName, c.MaxLength, c.IsAnsiPadded = "varchar", "varchar", 10, false
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "created under ANSI_PADDING OFF"},
		},
		{
			name:     "Always Encrypted",
			snapshot: withColumn(func(c *Column) { c.IsEncrypted = true }),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "Always Encrypted column"},
		},
		{
			name: "FILESTREAM",
			snapshot: withColumn(func(c *Column) {
				c.TypeName, c.BaseTypeName, c.MaxLength, c.IsAnsiPadded = "varbinary", "varbinary", -1, true
				c.IsFilestream = true
			}),
			expected: Refusal{Object: "[dbo].[t].[id]", Reason: "FILESTREAM column"},
		},
		{
			name:     "disabled clustered index",
			snapshot: withIndex(&Index{IndexID: 1, Name: "CIX", Type: IndexClustered, IsDisabled: true}),
			expected: Refusal{Object: "[dbo].[t].[CIX]", Reason: "disabled clustered index"},
		},
		{
			name: "disabled index backing a primary key",
			snapshot: withIndex(&Index{
				IndexID: 2, Name: "PK", Type: IndexNonClustered, IsUnique: true, IsPrimaryKey: true, IsDisabled: true,
			}),
			expected: Refusal{Object: "[dbo].[t].[PK]", Reason: "disabled index backing a key"},
		},
		{
			name: "disabled index backing a unique constraint",
			snapshot: withIndex(&Index{
				IndexID: 2, Name: "UQ", Type: IndexNonClustered, IsUnique: true, IsUniqueConstraint: true, IsDisabled: true,
			}),
			expected: Refusal{Object: "[dbo].[t].[UQ]", Reason: "disabled index backing a key"},
		},
		{
			name:     "hash index",
			snapshot: withIndex(&Index{IndexID: 2, Name: "HX", Type: IndexHash}),
			expected: Refusal{Object: "[dbo].[t].[HX]", Reason: "hash index"},
		},
		{
			name:     "encrypted function called by a table",
			snapshot: calledFunction(&Module{Type: TypeScalarFunction}),
			expected: Refusal{
				Object: "[fn].[f]",
				Reason: "needed by table [dbo].[t]: encrypted, its definition cannot be read",
			},
		},
		{
			name:     "CLR function called by a table",
			snapshot: calledFunction(&Module{Type: TypeCLRScalar}),
			expected: Refusal{Object: "[fn].[f]", Reason: "needed by table [dbo].[t]: CLR function"},
		},
		{
			name: "function called by a table and schema-bound to a table",
			snapshot: calledFunction(
				&Module{Type: TypeScalarFunction, HasDefinition: true, IsSchemaBound: true, Definition: "x"},
				&Dependency{
					ReferencingID: 10, ReferencingType: TypeScalarFunction,
					ReferencedClass: ClassObject, ReferencedID: 77, ReferencedType: TypeTable,
					ReferencedSchema: "ref", ReferencedName: "rates",
				},
			),
			expected: Refusal{
				Object: "[fn].[f]",
				Reason: "needed by table [dbo].[t]: schema-bound to table [ref].[rates]",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := Build(tc.snapshot)
			require.Nil(t, plan)
			var refusal *RefusalError
			require.ErrorAs(t, err, &refusal)
			require.Equal(t, []Refusal{tc.expected}, refusal.Refusals)
		})
	}
}

func Test_Build_RefusesEveryObjectAtOnce(t *testing.T) {
	t.Parallel()
	first := plainTable(1, "dbo", "a")
	first.IsMemoryOptimized = true
	first.Indexes = []*Index{{IndexID: 2, Name: "HX", Type: IndexHash, Columns: []*IndexColumn{{Name: "id", KeyOrdinal: 1}}}}
	second := plainTable(2, "dbo", "b")
	second.Columns[0].IsEncrypted = true
	second.Columns = append(second.Columns, intColumn(2, "other"))
	second.Columns[1].HasRule = true

	_, err := Build(&Snapshot{Tables: []*Table{first, second}})

	var refusal *RefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, []Refusal{
		{Object: "[dbo].[a]", Reason: "memory-optimized table"},
		{Object: "[dbo].[b].[id]", Reason: "Always Encrypted column"},
		{Object: "[dbo].[b].[other]", Reason: "a rule is bound to the column"},
	}, refusal.Refusals)
	require.Equal(
		t,
		"[dbo].[a]: memory-optimized table\n"+
			"[dbo].[b].[id]: Always Encrypted column\n"+
			"[dbo].[b].[other]: a rule is bound to the column",
		refusal.Error(),
	)
}

func Test_Build_AcceptsWhatLooksLikeARefusal(t *testing.T) {
	t.Parallel()

	t.Run("a default constraint of the column", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		table.Columns[0].HasDefault = true
		table.Columns[0].DefaultName = "DF_t_id"
		table.Columns[0].DefaultDefinition = "((1))"
		_, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
	})

	t.Run("a type that is never padded", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		table.Columns[0].IsAnsiPadded = false
		_, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
	})

	t.Run("a system CLR type", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		c := table.Columns[0]
		c.TypeName, c.BaseTypeName, c.IsAssemblyType, c.MaxLength = "geography", "geography", true, -1
		_, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
	})

	t.Run("a disabled non-clustered index", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		table.Indexes = []*Index{{
			IndexID: 2, Name: "IX", Type: IndexNonClustered, IsDisabled: true,
			AllowRowLocks: true, AllowPageLocks: true,
			Columns: []*IndexColumn{{Name: "id", KeyOrdinal: 1}},
		}}
		_, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
	})

	t.Run("a function schema-bound to nothing but itself", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		_, err := Build(&Snapshot{
			Tables: []*Table{table},
			Modules: []*Module{{
				ObjectID: 10, Schema: "fn", Name: "f", Type: TypeScalarFunction,
				HasDefinition: true, IsSchemaBound: true, Definition: "CREATE FUNCTION fn.f() ...",
			}},
			Dependencies: []*Dependency{{
				ReferencingID: 1, ReferencingType: TypeTable,
				ReferencedClass: ClassObject, ReferencedID: 10, ReferencedType: TypeScalarFunction,
				ReferencedSchema: "fn", ReferencedName: "f",
			}},
		})
		require.NoError(t, err)
	})
}
