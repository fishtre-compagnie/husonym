package ddl

import (
	"strings"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// readable is a module whose text was read.
func readable(id int64, schema, name, moduleType string) *Module {
	return &Module{
		ObjectID: id, Schema: schema, Name: name, Type: moduleType,
		UsesAnsiNulls: true, UsesQuotedIdentifier: true,
		HasDefinition: true, Definition: "CREATE " + name,
	}
}

// reference is a resolved reference of one object to another.
func reference(from int64, fromType string, to int64, toType, schema, name string) *Dependency {
	return &Dependency{
		ReferencingID: from, ReferencingType: fromType,
		ReferencedClass: ClassObject, ReferencedID: to, ReferencedType: toType,
		ReferencedSchema: schema, ReferencedName: name,
	}
}

func dataTypeNames(types []*sqlmanager_shared.DataType) []string {
	out := make([]string, len(types))
	for i, dt := range types {
		out[i] = dt.Schema + "." + dt.Name
	}
	return out
}

func Test_Build_ModulesOfTheSelection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		modules         []*Module
		dependencies    []*Dependency
		expectedModules []string
		expectedSkipped []*Skipped
	}{
		{
			name:            "a view of a selected schema on a selected table",
			modules:         []*Module{readable(10, "sales", "v", TypeView)},
			dependencies:    []*Dependency{reference(10, TypeView, 1, TypeTable, "sales", "orders")},
			expectedModules: []string{"sales.v"},
		},
		{
			name:            "a view of another schema is outside the selection",
			modules:         []*Module{readable(10, "hr", "v", TypeView)},
			expectedModules: []string{},
		},
		{
			name:            "an encrypted view",
			modules:         []*Module{{ObjectID: 10, Schema: "sales", Name: "v", Type: TypeView}},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[v]",
				Reason: "encrypted: its definition cannot be read",
			}},
		},
		{
			name:            "a CLR procedure",
			modules:         []*Module{{ObjectID: 10, Schema: "sales", Name: "p", Type: TypeCLRProcedure}},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[p]",
				Reason: "CLR module: it has no definition to read",
			}},
		},
		{
			name:            "a view on a table outside the selection",
			modules:         []*Module{readable(10, "sales", "v", TypeView)},
			dependencies:    []*Dependency{reference(10, TypeView, 99, TypeTable, "hr", "staff")},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[v]",
				Reason: "depends on table [hr].[staff], which is outside the selection",
			}},
		},
		{
			name: "a view on a view that is not reproduced, and a view on that one",
			modules: []*Module{
				readable(10, "sales", "a_top", TypeView),
				readable(11, "sales", "b_middle", TypeView),
				readable(12, "sales", "c_bottom", TypeView),
			},
			dependencies: []*Dependency{
				reference(10, TypeView, 11, TypeView, "sales", "b_middle"),
				reference(11, TypeView, 12, TypeView, "sales", "c_bottom"),
				reference(12, TypeView, 99, TypeTable, "hr", "staff"),
			},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{
				{
					Label: ViewsFunctionsLabel, Object: "[sales].[a_top]",
					Reason: "depends on [sales].[b_middle], which is not reproduced",
				},
				{
					Label: ViewsFunctionsLabel, Object: "[sales].[b_middle]",
					Reason: "depends on [sales].[c_bottom], which is not reproduced",
				},
				{
					Label: ViewsFunctionsLabel, Object: "[sales].[c_bottom]",
					Reason: "depends on table [hr].[staff], which is outside the selection",
				},
			},
		},
		{
			name:            "a view on a module of a schema outside the selection",
			modules:         []*Module{readable(10, "sales", "v", TypeView), readable(11, "hr", "f", TypeInlineFunction)},
			dependencies:    []*Dependency{reference(10, TypeView, 11, TypeInlineFunction, "hr", "f")},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[v]",
				Reason: "depends on [hr].[f], which is not reproduced",
			}},
		},
		{
			name:            "a procedure that needs a synonym",
			modules:         []*Module{readable(10, "sales", "p", TypeProcedure)},
			dependencies:    []*Dependency{reference(10, TypeProcedure, 50, TypeSynonym, "sales", "syn")},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[p]", Reason: "depends on synonym [sales].[syn]",
			}},
		},
		{
			name:    "a procedure that needs a table type",
			modules: []*Module{readable(10, "sales", "p", TypeProcedure)},
			dependencies: []*Dependency{{
				ReferencingID: 10, ReferencingType: TypeProcedure,
				ReferencedClass: ClassType, ReferencedID: 300, ReferencedIsTableType: true,
				ReferencedSchema: "sales", ReferencedName: "lines",
			}},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[p]", Reason: "depends on table type [sales].[lines]",
			}},
		},
		{
			name:    "a procedure that needs a CLR type",
			modules: []*Module{readable(10, "sales", "p", TypeProcedure)},
			dependencies: []*Dependency{{
				ReferencingID: 10, ReferencingType: TypeProcedure,
				ReferencedClass: ClassType, ReferencedID: 300, ReferencedIsAssemblyType: true,
				ReferencedSchema: "sales", ReferencedName: "Point",
			}},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[p]", Reason: "depends on CLR type [sales].[Point]",
			}},
		},
		{
			name:            "a view that calls a CLR function",
			modules:         []*Module{readable(10, "sales", "v", TypeView)},
			dependencies:    []*Dependency{reference(10, TypeView, 60, TypeCLRScalar, "util", "clr")},
			expectedModules: []string{},
			expectedSkipped: []*Skipped{{
				Label: ViewsFunctionsLabel, Object: "[sales].[v]", Reason: "depends on CLR object [util].[clr]",
			}},
		},
		{
			name:    "a procedure with a reference the server did not resolve",
			modules: []*Module{readable(10, "sales", "p", TypeProcedure)},
			dependencies: []*Dependency{
				{
					ReferencingID: 10, ReferencingType: TypeProcedure,
					ReferencedClass: ClassObject, ReferencedSchema: "elsewhere", ReferencedName: "gone",
				},
			},
			expectedModules: []string{"sales.p"},
		},
		{
			name: "modules come in dependency order",
			modules: []*Module{
				readable(10, "sales", "a_view", TypeView),
				readable(11, "sales", "z_function", TypeInlineFunction),
			},
			dependencies:    []*Dependency{reference(10, TypeView, 11, TypeInlineFunction, "sales", "z_function")},
			expectedModules: []string{"sales.z_function", "sales.a_view"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := Build(&Snapshot{
				Tables:       []*Table{plainTable(1, "sales", "orders")},
				Modules:      tc.modules,
				Dependencies: tc.dependencies,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectedModules, dataTypeNames(plan.Modules))
			require.ElementsMatch(t, tc.expectedSkipped, plan.Skipped)
			require.Empty(t, plan.Functions)
		})
	}
}

func Test_Build_FunctionsCalledByATable(t *testing.T) {
	t.Parallel()
	table := plainTable(1, "sales", "orders")
	table.Columns = append(table.Columns, &Column{
		ColumnID: 2, Name: "total", IsComputed: true, ComputedDefinition: "([util].[double]([id]))",
	})
	snapshot := &Snapshot{
		Tables: []*Table{table},
		Modules: []*Module{
			readable(10, "util", "double", TypeScalarFunction),
			readable(11, "util", "a_helper", TypeScalarFunction),
			readable(12, "util", "unrelated", TypeScalarFunction),
			readable(13, "sales", "report", TypeView),
		},
		Dependencies: []*Dependency{
			reference(1, TypeTable, 10, TypeScalarFunction, "util", "double"),
			reference(10, TypeScalarFunction, 11, TypeScalarFunction, "util", "a_helper"),
			// A scalar function created before the tables may read one of them: the server
			// resolves the name when the function runs.
			reference(11, TypeScalarFunction, 1, TypeTable, "sales", "orders"),
			reference(13, TypeView, 10, TypeScalarFunction, "util", "double"),
		},
	}

	plan, err := Build(snapshot)

	require.NoError(t, err)
	require.Equal(t, []string{"util.a_helper", "util.double"}, dataTypeNames(plan.Functions))
	require.Equal(t, []string{"sales.report"}, dataTypeNames(plan.Modules))
	require.Empty(t, plan.Skipped)
	require.Len(t, plan.Schemas, 2, "the schema of the functions is created with the one of the table")

	modules, sequences := snapshot.Wanted()
	require.Equal(t, []int64{10, 11, 13}, modules)
	require.Empty(t, sequences)
}

func Test_Build_Triggers(t *testing.T) {
	t.Parallel()
	trigger := func(id int64, name string, parent int64) *Module {
		m := readable(id, "sales", name, TypeTrigger)
		m.ParentID = parent
		return m
	}
	disabled := trigger(21, "b_disabled", 1)
	disabled.IsDisabled = true
	encrypted := trigger(22, "c_encrypted", 1)
	encrypted.HasDefinition, encrypted.Definition = false, ""
	clr := trigger(23, "d_clr", 1)
	clr.Type, clr.HasDefinition, clr.Definition = TypeCLRTrigger, false, ""

	snapshot := &Snapshot{
		Tables: []*Table{plainTable(1, "sales", "orders")},
		Modules: []*Module{
			readable(10, "sales", "open_orders", TypeView),
			readable(11, "sales", "skipped_view", TypeView),
			trigger(20, "a_after", 1),
			disabled, encrypted, clr,
			trigger(24, "on_view", 10),
			trigger(25, "on_skipped_view", 11),
			trigger(26, "on_another_table", 99),
		},
		Dependencies: []*Dependency{reference(11, TypeView, 99, TypeTable, "hr", "staff")},
	}

	plan, err := Build(snapshot)

	require.NoError(t, err)
	type created struct{ table, name, state string }
	actual := []created{}
	for _, tr := range plan.Triggers {
		require.Equal(t, "sales", tr.Schema)
		require.Equal(t, "sales", *tr.TriggerSchema)
		require.NotEmpty(t, tr.Definition)
		actual = append(actual, created{tr.Table, tr.TriggerName, tr.EnabledState})
	}
	require.Equal(t, []created{
		{"open_orders", "on_view", ""},
		{"orders", "a_after", ""},
		{"orders", "b_disabled", "D"},
	}, actual)
	require.ElementsMatch(t, []*Skipped{
		{
			Label: ViewsFunctionsLabel, Object: "[sales].[skipped_view]",
			Reason: "depends on table [hr].[staff], which is outside the selection",
		},
		{
			Label: TableTriggersLabel, Object: "[sales].[c_encrypted]",
			Reason: "encrypted: its definition cannot be read",
		},
		{
			Label: TableTriggersLabel, Object: "[sales].[d_clr]",
			Reason: "CLR module: it has no definition to read",
		},
	}, plan.Skipped)

	modules, _ := snapshot.Wanted()
	require.Equal(t, []int64{10, 20, 21, 24}, modules)
}

func Test_Build_TriggerIsGuardedByItsNameAndItsParent(t *testing.T) {
	t.Parallel()
	trigger := readable(20, "sales", "it's audit", TypeTrigger)
	trigger.ParentID, trigger.IsDisabled = 1, true
	plan, err := Build(&Snapshot{
		Tables:  []*Table{plainTable(1, "sales", "Order ] Lines")},
		Modules: []*Module{trigger},
	})
	require.NoError(t, err)

	// A trigger of that name on another table or view is not this one: the statement runs,
	// and the server refuses the name.
	guard := "SELECT 1 FROM sys.triggers WHERE name = N'it''s audit' " +
		"AND parent_id = OBJECT_ID(N'[sales].[Order ]] Lines]')"
	statements := plan.Blocks()[7].Statements
	require.Len(t, statements, 2)
	require.True(t, strings.HasPrefix(statements[0], "IF NOT EXISTS ("+guard+")\nBEGIN\n"), statements[0])
	require.Contains(t, statements[0], "    IF NOT EXISTS ("+guard+")\n        THROW 50000, ")
	require.True(t, strings.HasPrefix(statements[1], "IF EXISTS ("+guard+" AND is_disabled = 0)\n"), statements[1])
}

func Test_Build_Sequences(t *testing.T) {
	t.Parallel()
	sequence := func(id int64, schema, name string) *Sequence {
		return &Sequence{
			ObjectID: id, Schema: schema, Name: name,
			TypeSchema: "sys", TypeName: "bigint", BaseTypeName: "bigint", Precision: 19,
			StartValue: "1000", Increment: "1", MinimumValue: "1000", MaximumValue: "9223372036854775807",
			CurrentValue: "1000", IsCached: true, CacheSize: 50,
		}
	}
	used := sequence(31, "core", "used")
	used.CurrentValue, used.IsUsed = "1042", true
	table := plainTable(1, "sales", "orders")
	snapshot := &Snapshot{
		Tables: []*Table{table},
		// The snapshot holds two more sequences than the defaults draw from: one that a kept
		// procedure draws from, which is created, and one that nothing of the selection draws
		// from, which is not.
		Sequences: []*Sequence{
			sequence(30, "sales", "fresh"), used, sequence(33, "sales", "of_a_procedure"), sequence(34, "sales", "idle"),
		},
		Modules: []*Module{readable(40, "sales", "p_next", TypeProcedure)},
		Dependencies: []*Dependency{
			reference(40, TypeProcedure, 33, TypeSequence, "sales", "of_a_procedure"),
			{
				ReferencingID: 5, ReferencingType: TypeDefault, ReferencingParentID: 1,
				ReferencedClass: ClassObject, ReferencedID: 30, ReferencedType: TypeSequence,
				ReferencedSchema: "sales", ReferencedName: "fresh",
			},
			{
				ReferencingID: 6, ReferencingType: TypeDefault, ReferencingParentID: 1,
				ReferencedClass: ClassObject, ReferencedID: 31, ReferencedType: TypeSequence,
				ReferencedSchema: "core", ReferencedName: "used",
			},
			// The default of a table that was not requested draws from another sequence.
			{
				ReferencingID: 7, ReferencingType: TypeDefault, ReferencingParentID: 99,
				ReferencedClass: ClassObject, ReferencedID: 32, ReferencedType: TypeSequence,
				ReferencedSchema: "sales", ReferencedName: "other",
			},
		},
	}

	plan, err := Build(snapshot)

	require.NoError(t, err)
	require.Equal(t, []string{"core.used", "sales.fresh", "sales.of_a_procedure"}, dataTypeNames(plan.Sequences))
	require.Equal(t, []*Skipped{{
		Label: DataTypesLabel, Object: "[core].[used]",
		Reason: "created at its declared start 1000; the source is at 1042",
	}}, plan.Skipped)

	_, sequences := snapshot.Wanted()
	require.Equal(t, []int64{30, 31, 33}, sequences)
}

func Test_Build_AliasTypesOfModules(t *testing.T) {
	t.Parallel()
	aliasOf := func(module int64, moduleType, name string) *Dependency {
		return &Dependency{
			ReferencingID: module, ReferencingType: moduleType,
			ReferencedClass: ClassType, ReferencedID: 300, ReferencedSchema: "sales", ReferencedName: name,
		}
	}
	table := plainTable(1, "sales", "orders")
	table.Columns = append(table.Columns, &Column{
		ColumnID: 2, Name: "amount", TypeSchema: "sales", TypeName: "of_a_column", BaseTypeName: "decimal",
		IsUserDefinedType: true, MaxLength: 9, Precision: 19, Scale: 4, IsNullable: true,
	})
	snapshot := &Snapshot{
		Tables: []*Table{table},
		Modules: []*Module{
			readable(10, "sales", "p_column_type", TypeProcedure),
			readable(11, "sales", "p_other_type", TypeProcedure),
		},
		Dependencies: []*Dependency{
			aliasOf(10, TypeProcedure, "of_a_column"),
			aliasOf(11, TypeProcedure, "only_in_module"),
		},
	}

	plan, err := Build(snapshot)

	require.NoError(t, err)
	require.Equal(t, []string{"sales.p_column_type"}, dataTypeNames(plan.Modules))
	require.Equal(t, []*Skipped{{
		Label: ViewsFunctionsLabel, Object: "[sales].[p_other_type]",
		Reason: "depends on alias type [sales].[only_in_module], which no column of the selection uses",
	}}, plan.Skipped)

	// Before the columns are read, nothing tells which alias types they use: the definitions of
	// both procedures are asked for.
	bare := &Snapshot{
		Tables: []*Table{plainTable(1, "sales", "orders")}, Modules: snapshot.Modules, Dependencies: snapshot.Dependencies,
	}
	modules, _ := bare.Wanted()
	require.Equal(t, []int64{10, 11}, modules)
}

func Test_Build_Skips(t *testing.T) {
	t.Parallel()

	t.Run("a requested table the database does not have", func(t *testing.T) {
		t.Parallel()
		plan, err := Build(&Snapshot{
			Tables:  []*Table{plainTable(1, "dbo", "t")},
			Missing: []sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "Gone"}},
		})
		require.NoError(t, err)
		require.Len(t, plan.Tables, 1)
		require.Equal(t, []*Skipped{{
			Label: sqlmanager_shared.CreateTablesLabel, Object: "[dbo].[Gone]",
			Reason: "not found in the source database",
		}}, plan.Skipped)
	})

	t.Run("a foreign key to a table that was not requested", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		table.ForeignKeys = []*ForeignKey{{
			Name: "FK_out", ReferencedID: 99, ReferencedSchema: "ref", ReferencedTable: "parent",
			Columns: []*ForeignKeyColumn{{Name: "id", ReferencedName: "id"}},
		}}
		plan, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
		require.Empty(t, plan.Tables[0].AlterTableStatements)
		require.Equal(t, []*Skipped{{
			Label: FkAlterTableLabel, Object: "[dbo].[t].[FK_out]",
			Reason: "foreign key to [ref].[parent], which is not among the requested tables",
		}}, plan.Skipped)
	})

	t.Run("indexes that are access paths only", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		table.Indexes = []*Index{
			{IndexID: 2, Name: "XI", Type: IndexXML},
			{IndexID: 3, Name: "SI", Type: IndexSpatial},
			{IndexID: 4, Name: "JI", Type: IndexJSON},
		}
		plan, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
		require.Empty(t, plan.Tables[0].IndexStatements)
		require.Equal(t, []*Skipped{
			{Label: TableIndexLabel, Object: "[dbo].[t].[XI]", Reason: "XML indexes are not reproduced"},
			{Label: TableIndexLabel, Object: "[dbo].[t].[SI]", Reason: "spatial indexes are not reproduced"},
			{Label: TableIndexLabel, Object: "[dbo].[t].[JI]", Reason: "JSON indexes are not reproduced"},
		}, plan.Skipped)
	})

	t.Run("alias-typed character columns", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		for i, name := range []string{"email", "other"} {
			table.Columns = append(table.Columns, &Column{
				ColumnID: i + 2, Name: name, TypeSchema: "dbo", TypeName: "Email", BaseTypeName: "nvarchar",
				IsUserDefinedType: true, MaxLength: 640, Collation: "Latin1_General_100_CS_AS", IsAnsiPadded: true,
			})
		}
		plan, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
		require.Equal(t, []*Skipped{{
			Label: sqlmanager_shared.CreateTablesLabel, Object: "[dbo].[t]",
			Reason: "collation of 2 alias-typed column(s) follows the default of the destination database",
		}}, plan.Skipped)
	})

	t.Run("a requested name that is a view", func(t *testing.T) {
		t.Parallel()
		plan, err := Build(&Snapshot{
			Tables: []*Table{plainTable(1, "dbo", "t")},
			Views:  []sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "v_report"}},
		})
		require.NoError(t, err)
		require.Equal(t, []*Skipped{{
			Label: sqlmanager_shared.CreateTablesLabel, Object: "[dbo].[v_report]",
			Reason: "a view, not a table: only tables are requested",
		}}, plan.Skipped)
	})

	t.Run("a table created under ANSI_NULLS OFF", func(t *testing.T) {
		t.Parallel()
		table := plainTable(1, "dbo", "t")
		table.AnsiNullsOff = true
		plan, err := Build(&Snapshot{Tables: []*Table{table}})
		require.NoError(t, err)
		require.Equal(t, []*Skipped{{
			Label: sqlmanager_shared.CreateTablesLabel, Object: "[dbo].[t]",
			Reason: "created under ANSI_NULLS OFF at the source: it is created under ANSI_NULLS ON",
		}}, plan.Skipped)
	})

	t.Run("the indexes of an indexed view", func(t *testing.T) {
		t.Parallel()
		indexed := readable(10, "dbo", "v_indexed", TypeView)
		indexed.HasIndex = true
		// A view that is not created tells why, and nothing of its indexes.
		outside := readable(11, "dbo", "v_outside", TypeView)
		outside.HasIndex = true
		plan, err := Build(&Snapshot{
			Tables:       []*Table{plainTable(1, "dbo", "t")},
			Modules:      []*Module{indexed, outside},
			Dependencies: []*Dependency{reference(11, TypeView, 99, TypeTable, "hr", "staff")},
		})
		require.NoError(t, err)
		require.Equal(t, []string{"dbo.v_indexed"}, dataTypeNames(plan.Modules))
		require.ElementsMatch(t, []*Skipped{
			{
				Label: TableIndexLabel, Object: "[dbo].[v_indexed]",
				Reason: "the indexes of a view are not reproduced",
			},
			{
				Label: ViewsFunctionsLabel, Object: "[dbo].[v_outside]",
				Reason: "depends on table [hr].[staff], which is outside the selection",
			},
		}, plan.Skipped)
	})

	t.Run("system versioning comes after the indexes of the table", func(t *testing.T) {
		t.Parallel()
		current, history := staff()
		current.Indexes = append(current.Indexes, rowstore(2, "IX_staff_name", IndexNonClustered, key("name", 1)))
		plan, err := Build(&Snapshot{Tables: []*Table{current, history}})
		require.NoError(t, err)
		statements := plan.Tables[1].IndexStatements
		require.Len(t, statements, 2)
		require.Contains(t, statements[0], "[IX_staff_name]")
		require.Contains(t, statements[1], "SYSTEM_VERSIONING = ON")
	})

	t.Run("attributes of a table that are left out", func(t *testing.T) {
		t.Parallel()
		plan, err := Build(&Snapshot{
			Tables: []*Table{plainTable(1, "dbo", "t")},
			Notices: []*Notice{
				{ObjectID: 1, Kind: NoticeFilegroup, Detail: "FG_ARCHIVE"},
				{ObjectID: 1, Kind: NoticePartitioning, Detail: "ps_year"},
				{ObjectID: 1, Kind: NoticeCompression, Detail: "PAGE"},
				{ObjectID: 1, Kind: NoticeStatistics, Count: 2},
				{ObjectID: 1, Kind: NoticeExtendedProperties, Count: 3},
				{ObjectID: 1, Kind: NoticePermissions, Count: 1},
				{ObjectID: 1, Kind: NoticeFullTextIndex},
				{ObjectID: 1, Kind: NoticeRowLevelSecurity, Count: 1},
				{ObjectID: 1, Kind: NoticeChangeTracking},
				{ObjectID: 1, Kind: NoticeChangeDataCapture},
				{ObjectID: 1, Kind: NoticeTriggerOrder, Detail: "trg_first"},
				{ObjectID: 1, Kind: NoticeColumnstoreOrder, Detail: "CCI"},
				// A notice of a table that is not in the snapshot is not reported.
				{ObjectID: 99, Kind: NoticeCompression, Detail: "ROW"},
			},
		})
		require.NoError(t, err)
		create := sqlmanager_shared.CreateTablesLabel
		require.Equal(t, []*Skipped{
			{Label: create, Object: "[dbo].[t]", Reason: "filegroup not reproduced: FG_ARCHIVE"},
			{Label: create, Object: "[dbo].[t]", Reason: "partitioning not reproduced: ps_year"},
			{Label: create, Object: "[dbo].[t]", Reason: "compression not reproduced: PAGE"},
			{Label: create, Object: "[dbo].[t]", Reason: "statistics not reproduced: 2"},
			{Label: create, Object: "[dbo].[t]", Reason: "extended properties not reproduced: 3"},
			{Label: create, Object: "[dbo].[t]", Reason: "permissions not reproduced: 1"},
			{Label: TableIndexLabel, Object: "[dbo].[t]", Reason: "full-text index not reproduced"},
			{Label: create, Object: "[dbo].[t]", Reason: "row-level security not reproduced: 1"},
			{Label: create, Object: "[dbo].[t]", Reason: "change tracking not reproduced"},
			{Label: create, Object: "[dbo].[t]", Reason: "change data capture not reproduced"},
			{Label: TableTriggersLabel, Object: "[dbo].[t]", Reason: "trigger order not reproduced: trg_first"},
			{Label: TableIndexLabel, Object: "[dbo].[t]", Reason: "columnstore order not reproduced: CCI"},
		}, plan.Skipped)
	})
}

func Test_Plan_Blocks(t *testing.T) {
	t.Parallel()

	t.Run("an empty snapshot gives the eight blocks, empty", func(t *testing.T) {
		t.Parallel()
		plan, err := Build(&Snapshot{})
		require.NoError(t, err)
		blocks := plan.Blocks()
		labels := make([]string, len(blocks))
		for i, block := range blocks {
			labels[i] = block.Label
			require.NotNil(t, block.Statements)
			require.Empty(t, block.Statements)
			require.Empty(t, block.Skipped)
		}
		require.Equal(t, []string{
			"schemas", "data types", "create table", "view and functions",
			"non-fk alter table", "table index", "fk alter table", "table triggers",
		}, labels)
	})

	t.Run("what is skipped goes with the block it is missing from", func(t *testing.T) {
		t.Parallel()
		plan, err := Build(&Snapshot{
			Tables:  []*Table{plainTable(1, "dbo", "t")},
			Missing: []sqlmanager_shared.SchemaTable{{Schema: "dbo", Table: "Gone"}},
			Notices: []*Notice{{ObjectID: 1, Kind: NoticeTriggerOrder, Detail: "trg_first"}},
		})
		require.NoError(t, err)
		blocks := plan.Blocks()
		require.Equal(t, []*sqlmanager_shared.SkippedObject{
			{Object: "[dbo].[Gone]", Reason: "not found in the source database"},
		}, blocks[2].Skipped)
		require.Equal(t, []*sqlmanager_shared.SkippedObject{
			{Object: "[dbo].[t]", Reason: "trigger order not reproduced: trg_first"},
		}, blocks[7].Skipped)
	})
}
