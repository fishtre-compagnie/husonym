package ddl

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden files with what the generator writes")

const collation = "Latin1_General_100_CI_AS"

// batches shows statements the way a script would: one batch each, a GO line after each. GO is
// a line of the file only: no statement holds it.
func batches(statements []string) string {
	var b strings.Builder
	for _, statement := range statements {
		b.WriteString(statement)
		b.WriteString("\nGO\n")
	}
	return b.String()
}

func requireGolden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(actual), 0o600))
		return
	}
	expected, err := os.ReadFile(path)
	require.NoError(t, err, "run the tests with -update to write the golden files")
	require.Equal(t, string(expected), actual)
}

func character(id int, name, typeName string, maxLength int, nullable bool) *Column {
	return &Column{
		ColumnID: id, Name: name, TypeSchema: "sys", TypeName: typeName, BaseTypeName: typeName,
		MaxLength: maxLength, Collation: collation, IsNullable: nullable, IsAnsiPadded: true,
	}
}

func typed(id int, name, typeName string, maxLength, precision, scale int, nullable bool) *Column {
	return &Column{
		ColumnID: id, Name: name, TypeSchema: "sys", TypeName: typeName, BaseTypeName: typeName,
		MaxLength: maxLength, Precision: precision, Scale: scale, IsNullable: nullable,
	}
}

func rowstore(id int, name string, indexType int, columns ...*IndexColumn) *Index {
	return &Index{
		IndexID: id, Name: name, Type: indexType, AllowRowLocks: true, AllowPageLocks: true, Columns: columns,
	}
}

func key(name string, ordinal int) *IndexColumn {
	return &IndexColumn{Name: name, KeyOrdinal: ordinal}
}

func descending(name string, ordinal int) *IndexColumn {
	return &IndexColumn{Name: name, KeyOrdinal: ordinal, IsDescending: true}
}

func included(name string) *IndexColumn {
	return &IndexColumn{Name: name, IsIncluded: true}
}

// orderLines is a table whose names need every escape.
func orderLines() *Table {
	identity := typed(1, "id", "int", 4, 10, 0, false)
	identity.IsIdentity, identity.IdentitySeed, identity.IdentityIncrement = true, "1", "1"

	email := &Column{
		ColumnID: 4, Name: "email", TypeSchema: "sales", TypeName: "EmailAddress", BaseTypeName: "nvarchar",
		IsUserDefinedType: true, MaxLength: 640, Collation: collation, IsAnsiPadded: true,
	}
	quantity := typed(5, "qty", "decimal", 5, 9, 2, false)
	quantity.HasDefault, quantity.DefaultName, quantity.DefaultDefinition = true, "DF_lines_qty", "((1))"
	total := &Column{
		ColumnID: 6, Name: "total", IsComputed: true, ComputedDefinition: "([qty]*(2))", IsPersisted: true,
	}

	primary := rowstore(1, "PK_lines", IndexClustered, key("id", 1))
	primary.IsUnique, primary.IsPrimaryKey, primary.FillFactor = true, true, 90

	return &Table{
		ObjectID: 100, Schema: "sales", Name: "Order ] Lines",
		Columns: []*Column{
			identity,
			character(2, "it's", "nvarchar", 80, false),
			character(3, "note", "varchar", -1, true),
			email, quantity, total,
			typed(7, "order id", "int", 4, 10, 0, false),
			typed(8, "region", "int", 4, 10, 0, false),
		},
		Indexes: []*Index{primary},
	}
}

// orders is the table the lines reference.
func orders() *Table {
	primary := rowstore(1, "PK_Orders", IndexClustered, key("id", 1), key("region", 2))
	primary.IsUnique, primary.IsPrimaryKey = true, true
	return &Table{
		ObjectID: 101, Schema: "core", Name: "Orders",
		Columns: []*Column{
			typed(1, "id", "int", 4, 10, 0, false),
			typed(2, "region", "int", 4, 10, 0, false),
		},
		Indexes: []*Index{primary},
	}
}

func build(t *testing.T, snapshot *Snapshot) *Plan {
	t.Helper()
	plan, err := Build(snapshot)
	require.NoError(t, err)
	return plan
}

func Test_Golden_Schema(t *testing.T) {
	t.Parallel()
	plan := build(t, &Snapshot{Tables: []*Table{
		plainTable(1, "sales", "a"), plainTable(2, "we]ird", "b"), plainTable(3, "it's", "c"),
		plainTable(4, "a.b", "d"), plainTable(5, "Sales Ops", "e"), plainTable(6, "sales", "f"),
	}})
	requireGolden(t, "schema.sql", batches(plan.Schemas))
}

func Test_Golden_Table(t *testing.T) {
	t.Parallel()

	everyColumn := &Table{ObjectID: 1, Schema: "dbo", Name: "every column"}
	add := func(column *Column) *Column {
		column.ColumnID = len(everyColumn.Columns) + 1
		everyColumn.Columns = append(everyColumn.Columns, column)
		return column
	}
	identity := add(typed(0, "big id", "decimal", 13, 20, 0, false))
	identity.IsIdentity, identity.IdentitySeed, identity.IdentityIncrement = true, "-5", "10"
	identity.IdentityNotForReplication = true
	guid := add(typed(0, "guid", "uniqueidentifier", 16, 0, 0, false))
	guid.IsRowGuidCol = true
	guid.HasDefault, guid.DefaultName, guid.DefaultDefinition = true, "DF__every__guid__5EBF139D", "(newid())"
	add(typed(0, "sparse", "int", 4, 10, 0, true)).IsSparse = true
	add(typed(0, "set", "xml", -1, 0, 0, true)).IsColumnSet = true
	masked := add(character(0, "masked", "varchar", 20, true))
	// The catalog tells the function with its quotes already doubled.
	masked.IsMasked, masked.MaskingFunction = true, `partial(1, "x''x", 0)`
	quoted := add(character(0, "it's", "nchar", 20, false))
	quoted.HasDefault, quoted.DefaultName, quoted.DefaultDefinition = true, "DF it's", "(N'it''s')"
	add(&Column{Name: "plain", IsComputed: true, ComputedDefinition: "([sparse]+(1))", IsNullable: true})
	add(&Column{Name: "stored", IsComputed: true, ComputedDefinition: "(isnull([sparse],(0)))", IsPersisted: true})
	add(&Column{
		Name: "stored nullable", IsComputed: true, ComputedDefinition: "([sparse]*(2))",
		IsPersisted: true, IsNullable: true,
	})
	add(typed(0, "version", "timestamp", 8, 0, 0, false))
	add(typed(0, "when", "datetime2", 7, 23, 3, true))
	add(typed(0, "shape", "geography", -1, 0, 0, true)).IsAssemblyType = true
	add(character(0, "label", "sysname", 256, false)).BaseTypeName = "nvarchar"
	add(&Column{
		Name: "amount", TypeSchema: "dbo", TypeName: "Money]4", BaseTypeName: "decimal",
		IsUserDefinedType: true, MaxLength: 9, Precision: 19, Scale: 4, IsNullable: true,
	})

	periodOnly := &Table{
		ObjectID: 2, Schema: "hr", Name: "period only",
		PeriodStartColumn: "valid from", PeriodEndColumn: "valid to",
		Columns: []*Column{
			typed(1, "id", "int", 4, 10, 0, false),
			typed(2, "valid from", "datetime2", 8, 27, 7, false),
			typed(3, "valid to", "datetime2", 8, 27, 7, false),
		},
	}
	periodOnly.Columns[1].GeneratedAlways = GeneratedRowStart
	periodOnly.Columns[2].GeneratedAlways, periodOnly.Columns[2].IsHidden = GeneratedRowEnd, true

	plan := build(t, &Snapshot{Tables: []*Table{orderLines(), everyColumn, periodOnly}})
	statements := []string{}
	for _, table := range plan.Tables {
		statements = append(statements, table.CreateTableStatement)
	}
	requireGolden(t, "table.sql", batches(statements))
}

func Test_Golden_AliasType(t *testing.T) {
	t.Parallel()
	table := &Table{
		ObjectID: 1, Schema: "dbo", Name: "t",
		Columns: []*Column{
			{
				ColumnID: 1, Name: "email", TypeSchema: "sales", TypeName: "EmailAddress", BaseTypeName: "nvarchar",
				IsUserDefinedType: true, MaxLength: 640, IsAnsiPadded: true,
			},
			{
				ColumnID: 2, Name: "email again", TypeSchema: "sales", TypeName: "EmailAddress", BaseTypeName: "nvarchar",
				IsUserDefinedType: true, MaxLength: 640, IsAnsiPadded: true,
			},
			{
				ColumnID: 3, Name: "blob", TypeSchema: "core", TypeName: "EmailAddress", BaseTypeName: "varbinary",
				IsUserDefinedType: true, TypeIsNullable: true, MaxLength: 50, IsAnsiPadded: true, IsNullable: true,
			},
			{
				ColumnID: 4, Name: "amount", TypeSchema: "core", TypeName: "it's money", BaseTypeName: "decimal",
				IsUserDefinedType: true, MaxLength: 9, Precision: 19, Scale: 4,
			},
			{
				ColumnID: 5, Name: "text", TypeSchema: "core", TypeName: "Long]Text", BaseTypeName: "varchar",
				IsUserDefinedType: true, TypeIsNullable: true, MaxLength: -1, IsAnsiPadded: true, IsNullable: true,
			},
		},
	}
	requireGolden(t, "alias_type.sql", batches(dataTypeStatements(build(t, &Snapshot{Tables: []*Table{table}}).AliasTypes)))
}

func dataTypeStatements(types []*sqlmanager_shared.DataType) []string {
	statements := make([]string, len(types))
	for i, dt := range types {
		statements[i] = dt.Definition
	}
	return statements
}

// invoiceNumbers is a sequence, and the default that draws from it.
func invoiceNumbers(id int64, schema, name string) (*Sequence, *Dependency) {
	sequence := &Sequence{
		ObjectID: id, Schema: schema, Name: name,
		TypeSchema: "sys", TypeName: "bigint", BaseTypeName: "bigint", Precision: 19,
		StartValue: "1000", Increment: "1", MinimumValue: "1000", MaximumValue: "9223372036854775807",
		CurrentValue: "1000", IsCached: true, CacheSize: 50,
	}
	return sequence, &Dependency{
		ReferencingID: 1000 + id, ReferencingType: TypeDefault, ReferencingParentID: 1,
		ReferencedClass: ClassObject, ReferencedID: id, ReferencedType: TypeSequence,
		ReferencedSchema: schema, ReferencedName: name,
	}
}

func Test_Golden_Sequence(t *testing.T) {
	t.Parallel()
	snapshot := &Snapshot{Tables: []*Table{plainTable(1, "dbo", "t")}}
	add := func(schema, name string, change func(*Sequence)) {
		sequence, dependency := invoiceNumbers(int64(len(snapshot.Sequences)+10), schema, name)
		change(sequence)
		snapshot.Sequences = append(snapshot.Sequences, sequence)
		snapshot.Dependencies = append(snapshot.Dependencies, dependency)
	}
	add("sales", "InvoiceNo", func(*Sequence) {})
	add("sales", "cycling down", func(s *Sequence) {
		s.TypeName, s.BaseTypeName, s.Precision = "tinyint", "tinyint", 3
		s.StartValue, s.Increment, s.MinimumValue, s.MaximumValue = "200", "-5", "0", "255"
		s.IsCycling, s.IsCached, s.CacheSize = true, false, 0
	})
	add("sales", "decimal", func(s *Sequence) {
		s.TypeName, s.BaseTypeName, s.Precision = "decimal", "decimal", 18
		s.MaximumValue, s.CacheSize = "999999999999999999", 0
	})
	add("it's", "by ] alias", func(s *Sequence) {
		s.TypeSchema, s.TypeName, s.BaseTypeName, s.Precision = "core", "Counter", "int", 10
		s.IsUserDefinedType = true
		s.MaximumValue = "2147483647"
	})
	plan := build(t, snapshot)
	requireGolden(t, "sequence.sql", batches(append(dataTypeStatements(plan.AliasTypes), dataTypeStatements(plan.Sequences)...)))
}

func alterStatements(plan *Plan) []string {
	statements := []string{}
	for _, table := range plan.Tables {
		for _, alter := range table.AlterTableStatements {
			statements = append(statements, alter.Statement)
		}
	}
	return statements
}

func Test_Golden_Key(t *testing.T) {
	t.Parallel()
	lines := orderLines()

	second := rowstore(3, "UQ_b", IndexNonClustered, descending("region", 1), key("order id", 2))
	second.IsUnique, second.IsUniqueConstraint = true, true
	second.IsPadded, second.FillFactor, second.IgnoreDupKey = true, 80, true
	second.AllowRowLocks, second.AllowPageLocks = false, false
	first := rowstore(4, "UQ_a", IndexNonClustered, key("it's", 1))
	first.IsUnique, first.IsUniqueConstraint = true, true
	lines.Indexes = append(lines.Indexes, second, first)

	heapKey := plainTable(2, "dbo", "heap")
	nonClustered := rowstore(2, "PK__heap__3213E83F", IndexNonClustered, key("id", 1))
	nonClustered.IsUnique, nonClustered.IsPrimaryKey = true, true
	heapKey.Indexes = []*Index{nonClustered}

	requireGolden(t, "key.sql", batches(alterStatements(build(t, &Snapshot{Tables: []*Table{lines, heapKey}}))))
}

func Test_Golden_Check(t *testing.T) {
	t.Parallel()
	lines := orderLines()
	lines.Indexes = nil
	lines.Checks = []*CheckConstraint{
		{Name: "CK_lines_qty", Definition: "([qty]>(0))"},
		{Name: "CK_disabled", Definition: "([qty]<(1000))", IsDisabled: true, IsNotTrusted: true},
		{Name: "CK it's", Definition: "([it's]<>N'it''s')", IsNotTrusted: true},
		{Name: "CK_replication", Definition: "([region]>(0))", IsNotForReplication: true, IsNotTrusted: true},
	}
	requireGolden(t, "check.sql", batches(alterStatements(build(t, &Snapshot{Tables: []*Table{lines}}))))
}

func Test_Golden_ForeignKey(t *testing.T) {
	t.Parallel()
	lines := orderLines()
	lines.Indexes = nil
	lines.ForeignKeys = []*ForeignKey{
		{
			Name: "FK_lines_order", ReferencedID: 101, ReferencedSchema: "core", ReferencedTable: "Orders",
			DeleteAction: ActionCascade, IsDisabled: true, IsNotTrusted: true,
			Columns: []*ForeignKeyColumn{
				{Name: "order id", ReferencedName: "id"}, {Name: "region", ReferencedName: "region"},
			},
		},
		{
			Name: "FK_self", ReferencedID: 100, ReferencedSchema: "sales", ReferencedTable: "Order ] Lines",
			Columns: []*ForeignKeyColumn{{Name: "order id", ReferencedName: "id"}},
		},
		{
			Name: "FK_actions", ReferencedID: 101, ReferencedSchema: "core", ReferencedTable: "Orders",
			DeleteAction: ActionSetNull, UpdateAction: ActionSetDefault, IsNotForReplication: true, IsNotTrusted: true,
			Columns: []*ForeignKeyColumn{
				{Name: "order id", ReferencedName: "id"}, {Name: "region", ReferencedName: "region"},
			},
		},
	}
	parent := orders()
	parent.Indexes = nil
	requireGolden(t, "foreign_key.sql", batches(alterStatements(build(t, &Snapshot{Tables: []*Table{lines, parent}}))))
}

func Test_Golden_Index(t *testing.T) {
	t.Parallel()
	lines := orderLines()
	unique := rowstore(2, "UX_lines_sku", IndexNonClustered,
		key("it's", 1), descending("id", 2), included("qty"), included("note"))
	unique.IsUnique, unique.HasFilter, unique.FilterDefinition = true, true, "([qty]>(0))"
	options := rowstore(3, "IX options", IndexNonClustered, key("region", 2), key("order id", 1))
	options.IsPadded, options.FillFactor, options.IgnoreDupKey = true, 70, true
	options.IsUnique, options.AllowRowLocks, options.AllowPageLocks = true, false, false
	disabled := rowstore(4, "IX_disabled", IndexNonClustered, key("region", 1))
	disabled.IsDisabled = true
	// A partitioned index holds the partitioning column, which is neither key nor included.
	partitioned := rowstore(5, "IX_partitioned", IndexNonClustered, key("order id", 1), &IndexColumn{Name: "region"})
	lines.Indexes = []*Index{disabled, options, unique, partitioned}

	heap := plainTable(2, "dbo", "heap")
	heap.Indexes = []*Index{rowstore(1, "CIX", IndexClustered, descending("id", 1))}

	facts := plainTable(3, "dw", "facts")
	facts.Indexes = []*Index{{IndexID: 1, Name: "CCI_facts", Type: IndexClusteredColumnstore, Columns: []*IndexColumn{included("id")}}}

	employees := plainTable(4, "hr", "Employees")
	employees.Columns = append(employees.Columns, intColumn(2, "salary"), intColumn(3, "dept"))
	employees.Indexes = []*Index{{
		IndexID: 2, Name: "NCCI_emp", Type: IndexNonClusteredColumnstore,
		HasFilter: true, FilterDefinition: "([dept]>(0))",
		Columns: []*IndexColumn{included("salary"), included("dept")},
	}}

	plan := build(t, &Snapshot{Tables: []*Table{lines, heap, facts, employees}})
	statements := []string{}
	for _, table := range plan.Tables {
		statements = append(statements, table.IndexStatements...)
	}
	requireGolden(t, "index.sql", batches(statements))
}

func Test_Golden_Module(t *testing.T) {
	t.Parallel()
	view := readable(10, "sales", "v_open", TypeView)
	view.Definition = "CREATE VIEW [sales].[v_open] AS SELECT id FROM sales.orders WHERE state = 'open'"
	quotedOff := readable(11, "sales", `v "quoted" 100%`, TypeView)
	quotedOff.UsesQuotedIdentifier = false
	quotedOff.Definition = "create view sales.[v \"quoted\" 100%] as\nselect \"it's\" as [a]]b], 'x''y' as c"
	function := readable(12, "sales", "fn_inline", TypeInlineFunction)
	function.UsesAnsiNulls = false
	function.Definition = "CREATE FUNCTION sales.fn_inline(@id int) RETURNS TABLE AS RETURN (SELECT @id AS id)"
	procedure := readable(13, "sales", "it's a proc", TypeProcedure)
	procedure.Definition = "CREATE PROCEDURE sales.[it's a proc] AS BEGIN SET NOCOUNT ON; SELECT 1; END"

	plan := build(t, &Snapshot{
		Tables:  []*Table{plainTable(1, "sales", "orders")},
		Modules: []*Module{view, quotedOff, function, procedure},
	})
	requireGolden(t, "module.sql", batches(dataTypeStatements(plan.Modules)))
}

func Test_Golden_Trigger(t *testing.T) {
	t.Parallel()
	after := readable(20, "sales", "trg_audit", TypeTrigger)
	after.ParentID = 1
	after.Definition = "CREATE TRIGGER sales.trg_audit ON sales.orders AFTER INSERT AS BEGIN SET NOCOUNT ON; END"
	disabled := readable(21, "sales", "trg ] off", TypeTrigger)
	disabled.ParentID, disabled.IsDisabled = 1, true
	disabled.Definition = "CREATE TRIGGER sales.[trg ]] off] ON sales.orders AFTER DELETE AS RETURN"

	plan := build(t, &Snapshot{
		Tables:  []*Table{plainTable(1, "sales", "orders")},
		Modules: []*Module{after, disabled},
	})
	requireGolden(t, "trigger.sql", batches(plan.Blocks()[7].Statements))
}

// staff is a system-versioned table and its history table.
func staff() (current, history *Table) {
	columns := func() []*Column {
		return []*Column{
			typed(1, "id", "int", 4, 10, 0, false),
			character(2, "name", "nvarchar", 100, false),
			typed(3, "valid_from", "datetime2", 8, 27, 7, false),
			typed(4, "valid_to", "datetime2", 8, 27, 7, false),
		}
	}
	primary := rowstore(1, "PK_staff", IndexClustered, key("id", 1))
	primary.IsUnique, primary.IsPrimaryKey = true, true
	current = &Table{
		ObjectID: 200, Schema: "hr", Name: "staff", TemporalType: TemporalSystemVersioned,
		HistoryID: 201, HistorySchema: "hr", HistoryName: "staff_history",
		RetentionPeriod: 6, RetentionUnit: "MONTH",
		PeriodStartColumn: "valid_from", PeriodEndColumn: "valid_to",
		Columns: columns(), Indexes: []*Index{primary},
	}
	current.Columns[2].GeneratedAlways, current.Columns[2].IsHidden = GeneratedRowStart, true
	current.Columns[3].GeneratedAlways, current.Columns[3].IsHidden = GeneratedRowEnd, true
	history = &Table{
		ObjectID: 201, Schema: "hr", Name: "staff_history", TemporalType: TemporalHistory,
		Columns: columns(),
		Indexes: []*Index{
			rowstore(1, "ix_staff_history", IndexClustered, key("valid_to", 1), key("valid_from", 2)),
			rowstore(2, "ix_staff_history_name", IndexNonClustered, key("name", 1)),
		},
	}
	return current, history
}

// script shows a whole plan: each block under its label, what it leaves out first.
func script(plan *Plan) string {
	var b strings.Builder
	for _, block := range plan.Blocks() {
		b.WriteString("-- " + block.Label + "\n")
		for _, skipped := range block.Skipped {
			b.WriteString("-- skipped: " + skipped.Object + ": " + skipped.Reason + "\n")
		}
		b.WriteString(batches(block.Statements))
	}
	return b.String()
}

func Test_Golden_Temporal(t *testing.T) {
	t.Parallel()
	current, history := staff()
	forEver := plainTable(300, "hr", "kept for ever")
	forEver.TemporalType, forEver.RetentionPeriod, forEver.RetentionUnit = TemporalSystemVersioned, -1, "INFINITE"
	forEver.HistoryID, forEver.HistorySchema, forEver.HistoryName = 301, "archive", "kept ] history"
	oneDay := plainTable(302, "hr", "one day")
	oneDay.TemporalType, oneDay.RetentionPeriod, oneDay.RetentionUnit = TemporalSystemVersioned, 1, "DAY"
	oneDay.HistoryID, oneDay.HistorySchema, oneDay.HistoryName = 303, "hr", "one day history"
	oldHistory := plainTable(301, "archive", "kept ] history")
	oldHistory.TemporalType = TemporalHistory
	dayHistory := plainTable(303, "hr", "one day history")
	dayHistory.TemporalType = TemporalHistory

	// The current table is requested first: its history table is created before it all the same.
	plan := build(t, &Snapshot{Tables: []*Table{current, forEver, oneDay, history, oldHistory, dayHistory}})
	requireGolden(t, "temporal.sql", script(plan))
}

func Test_Golden_Composed(t *testing.T) {
	t.Parallel()
	requireGolden(t, "composed.sql", script(build(t, composed())))
}

func Test_Plan_NoStatementHoldsABatchSeparator(t *testing.T) {
	t.Parallel()
	for _, block := range build(t, composed()).Blocks() {
		require.NotEmpty(t, block.Statements, block.Label)
		for _, statement := range block.Statements {
			for _, line := range strings.Split(statement, "\n") {
				require.NotEqual(t, "GO", strings.ToUpper(strings.TrimSpace(line)), statement)
			}
		}
	}
}

// composed is a schema that fills every block.
func composed() *Snapshot {
	lines := orderLines()
	lines.Columns[4].DefaultDefinition = "(NEXT VALUE FOR [sales].[InvoiceNo])"
	lines.Checks = []*CheckConstraint{{Name: "CK_lines_qty", Definition: "([util].[positive]([qty])=(1))"}}
	lines.ForeignKeys = []*ForeignKey{
		{
			Name: "FK_lines_order", ReferencedID: 101, ReferencedSchema: "core", ReferencedTable: "Orders",
			DeleteAction: ActionCascade,
			Columns: []*ForeignKeyColumn{
				{Name: "order id", ReferencedName: "id"}, {Name: "region", ReferencedName: "region"},
			},
		},
		{
			Name: "FK_lines_product", ReferencedID: 999, ReferencedSchema: "catalog", ReferencedTable: "products",
			Columns: []*ForeignKeyColumn{{Name: "order id", ReferencedName: "id"}},
		},
	}
	unique := rowstore(2, "UX_lines_sku", IndexNonClustered, key("it's", 1))
	unique.IsUnique = true
	lines.Indexes = append(lines.Indexes, unique, &Index{IndexID: 3, Name: "XI_lines", Type: IndexXML})
	current, history := staff()

	sequence, drawn := invoiceNumbers(30, "sales", "InvoiceNo")
	sequence.CurrentValue, sequence.IsUsed = "1042", true
	drawn.ReferencingParentID = 100

	positive := readable(40, "util", "positive", TypeScalarFunction)
	positive.Definition = "CREATE FUNCTION util.positive(@n decimal(9,2)) RETURNS bit AS BEGIN RETURN IIF(@n > 0, 1, 0) END"
	open := readable(41, "sales", "v_open", TypeView)
	open.Definition = "CREATE VIEW [sales].[v_open] AS SELECT id FROM sales.[Order ]] Lines] WHERE qty > 0"
	summary := readable(42, "sales", "a_summary", TypeView)
	summary.Definition = "CREATE VIEW sales.a_summary AS SELECT COUNT(*) AS n FROM sales.v_open"
	outside := readable(43, "sales", "v_products", TypeView)
	secret := &Module{ObjectID: 44, Schema: "core", Name: "p_secret", Type: TypeProcedure}
	audit := readable(45, "sales", "trg_audit", TypeTrigger)
	audit.ParentID, audit.IsDisabled = 100, true
	audit.Definition = "CREATE TRIGGER sales.trg_audit ON sales.[Order ]] Lines] AFTER INSERT AS RETURN"
	instead := readable(46, "sales", "trg_view", TypeTrigger)
	instead.ParentID = 41
	instead.Definition = "CREATE TRIGGER sales.trg_view ON sales.v_open INSTEAD OF INSERT AS RETURN"

	return &Snapshot{
		Tables:    []*Table{lines, orders(), current, history},
		Missing:   []sqlmanager_shared.SchemaTable{{Schema: "sales", Table: "Gone"}},
		Sequences: []*Sequence{sequence},
		Modules:   []*Module{positive, open, summary, outside, secret, audit, instead},
		Dependencies: []*Dependency{
			drawn,
			{
				ReferencingID: 900, ReferencingType: TypeCheck, ReferencingParentID: 100,
				ReferencedClass: ClassObject, ReferencedID: 40, ReferencedType: TypeScalarFunction,
				ReferencedSchema: "util", ReferencedName: "positive",
			},
			reference(41, TypeView, 100, TypeTable, "sales", "Order ] Lines"),
			reference(42, TypeView, 41, TypeView, "sales", "v_open"),
			reference(43, TypeView, 999, TypeTable, "catalog", "products"),
		},
		Notices: []*Notice{
			{ObjectID: 100, Kind: NoticeCompression, Detail: "PAGE"},
			{ObjectID: 101, Kind: NoticeExtendedProperties, Count: 2},
		},
	}
}
