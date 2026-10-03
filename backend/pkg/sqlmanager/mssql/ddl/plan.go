package ddl

import (
	"cmp"
	"fmt"
	"slices"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Labels of the blocks of a plan, beside the two sqlmanager_shared names.
const (
	DataTypesLabel       = "data types"
	ViewsFunctionsLabel  = "view and functions"
	NonFkAlterTableLabel = "non-fk alter table"
	TableIndexLabel      = "table index"
	FkAlterTableLabel    = "fk alter table"
	TableTriggersLabel   = "table triggers"
)

// triggerDisabled is the state of a trigger that never fires.
const triggerDisabled = "D"

// Plan holds the statements of a selection, by kind, in execution order within each kind.
type Plan struct {
	Schemas    []string
	AliasTypes []*sqlmanager_shared.DataType
	Sequences  []*sqlmanager_shared.DataType
	// Functions are those a table calls: they are created before the tables.
	Functions []*sqlmanager_shared.DataType
	// Tables come history tables first: a table is versioned on a history table that exists.
	Tables []*sqlmanager_shared.TableInitStatement
	// Modules are the other views, functions and procedures.
	Modules  []*sqlmanager_shared.DataType
	Triggers []*sqlmanager_shared.TableTrigger
	Skipped  []*Skipped
}

// Skipped is an object or attribute left out of the plan. Label is the block it is missing from.
type Skipped struct{ Label, Object, Reason string }

// Build turns a snapshot into a plan, or returns a *RefusalError naming every object it refuses.
func Build(s *Snapshot) (*Plan, error) {
	sel := selectObjects(s)
	if refused := refusals(s, sel); len(refused) > 0 {
		return nil, &RefusalError{Refusals: refused}
	}

	b := &builder{
		plan: &Plan{
			Schemas:    []string{},
			AliasTypes: []*sqlmanager_shared.DataType{},
			Sequences:  []*sqlmanager_shared.DataType{},
			Functions:  []*sqlmanager_shared.DataType{},
			Tables:     []*sqlmanager_shared.TableInitStatement{},
			Modules:    []*sqlmanager_shared.DataType{},
			Triggers:   []*sqlmanager_shared.TableTrigger{},
			Skipped:    []*Skipped{},
		},
		schemas:    map[string]bool{},
		aliasTypes: map[[2]string]bool{},
		tables:     map[int64]*Table{},
	}
	for _, table := range s.Tables {
		b.tables[table.ObjectID] = table
	}

	for _, missing := range s.Missing {
		b.skip(sqlmanager_shared.CreateTablesLabel, QualifiedName(missing.Schema, missing.Table),
			"not found in the source database")
	}
	b.plan.Skipped = append(b.plan.Skipped, sel.skipped...)
	b.sequences(s, sel)
	b.modules(sel)
	for _, table := range historyFirst(s.Tables) {
		b.table(table)
	}
	b.notices(s)

	for schema := range b.schemas {
		b.plan.Schemas = append(b.plan.Schemas, schema)
	}
	slices.Sort(b.plan.Schemas)
	for i, schema := range b.plan.Schemas {
		b.plan.Schemas[i] = createSchema(schema)
	}
	slices.SortFunc(b.plan.AliasTypes, func(x, y *sqlmanager_shared.DataType) int {
		return cmp.Or(cmp.Compare(x.Schema, y.Schema), cmp.Compare(x.Name, y.Name))
	})
	return b.plan, nil
}

// builder gathers a plan, and what the statements need beside themselves: the schemas that
// hold an object, the alias types that columns and sequences are made of.
type builder struct {
	plan       *Plan
	schemas    map[string]bool
	aliasTypes map[[2]string]bool
	tables     map[int64]*Table
}

func (b *builder) skip(label, object, reason string) {
	b.plan.Skipped = append(b.plan.Skipped, &Skipped{Label: label, Object: object, Reason: reason})
}

// historyFirst puts the history tables before the others, each group in the order it came in.
func historyFirst(tables []*Table) []*Table {
	ordered := slices.Clone(tables)
	slices.SortStableFunc(ordered, func(x, y *Table) int {
		return cmp.Compare(historyRank(x), historyRank(y))
	})
	return ordered
}

func historyRank(table *Table) int {
	if table.TemporalType == TemporalHistory {
		return 0
	}
	return 1
}

func (b *builder) alias(schema, name, baseType string, maxLength, precision, scale int, nullable bool) {
	key := [2]string{schema, name}
	if b.aliasTypes[key] {
		return
	}
	b.aliasTypes[key] = true
	b.schemas[schema] = true
	b.plan.AliasTypes = append(b.plan.AliasTypes, aliasType(schema, name, baseType, maxLength, precision, scale, nullable))
}

// sequences writes the sequences the defaults of the tables draw from, by schema then name.
func (b *builder) sequences(s *Snapshot, sel *selection) {
	drawn := []*Sequence{}
	for _, sequence := range s.Sequences {
		if sel.sequences[sequence.ObjectID] {
			drawn = append(drawn, sequence)
		}
	}
	slices.SortFunc(drawn, func(x, y *Sequence) int {
		return cmp.Or(cmp.Compare(x.Schema, y.Schema), cmp.Compare(x.Name, y.Name))
	})
	for _, sequence := range drawn {
		b.schemas[sequence.Schema] = true
		if sequence.IsUserDefinedType {
			b.alias(sequence.TypeSchema, sequence.TypeName, sequence.BaseTypeName,
				0, sequence.Precision, sequence.Scale, sequence.TypeIsNullable)
		}
		b.plan.Sequences = append(b.plan.Sequences, createSequence(sequence))
		if sequence.IsUsed {
			b.skip(DataTypesLabel, QualifiedName(sequence.Schema, sequence.Name), fmt.Sprintf(
				"created at its declared start %s; the source is at %s", sequence.StartValue, sequence.CurrentValue,
			))
		}
	}
}

func (b *builder) modules(sel *selection) {
	dataType := func(m *Module) *sqlmanager_shared.DataType {
		b.schemas[m.Schema] = true
		return &sqlmanager_shared.DataType{Schema: m.Schema, Name: m.Name, Definition: createModule(m)}
	}
	for _, m := range sel.tableFunctions {
		b.plan.Functions = append(b.plan.Functions, dataType(m))
	}
	for _, m := range sel.modules {
		b.plan.Modules = append(b.plan.Modules, dataType(m))
	}
	for _, t := range sel.triggers {
		created := &sqlmanager_shared.TableTrigger{
			Schema:        t.module.Schema,
			Table:         t.parent,
			TriggerSchema: &t.module.Schema,
			TriggerName:   t.module.Name,
			Definition:    createModule(t.module),
		}
		if t.module.IsDisabled {
			created.EnabledState = triggerDisabled
		}
		b.plan.Triggers = append(b.plan.Triggers, created)
	}
}

// table writes a table and what belongs to it.
func (b *builder) table(table *Table) {
	b.schemas[table.Schema] = true
	name := QualifiedName(table.Schema, table.Name)

	followers := 0
	for _, column := range table.Columns {
		if !column.IsUserDefinedType || column.IsComputed {
			continue
		}
		b.alias(column.TypeSchema, column.TypeName, column.BaseTypeName,
			column.MaxLength, column.Precision, column.Scale, column.TypeIsNullable)
		if column.Collation != "" {
			followers++
		}
	}
	if followers > 0 {
		b.skip(sqlmanager_shared.CreateTablesLabel, name, fmt.Sprintf(
			"collation of %d alias-typed column(s) follows the default of the destination database", followers,
		))
	}

	statements := &sqlmanager_shared.TableInitStatement{
		CreateTableStatement: createTable(table),
		AlterTableStatements: []*sqlmanager_shared.AlterTableStatement{},
		IndexStatements:      []string{},
		PartitionStatements:  []string{},
	}

	// The primary key, then the unique constraints by name, then the checks by name.
	keys := []*Index{}
	indexes := []*Index{}
	for _, index := range table.Indexes {
		switch {
		case index.IsPrimaryKey || index.IsUniqueConstraint:
			keys = append(keys, index)
		case index.Type == IndexXML:
			b.skip(TableIndexLabel, childName(table, index.Name), "XML indexes are not reproduced")
		case index.Type == IndexSpatial:
			b.skip(TableIndexLabel, childName(table, index.Name), "spatial indexes are not reproduced")
		case index.Type == IndexJSON:
			b.skip(TableIndexLabel, childName(table, index.Name), "JSON indexes are not reproduced")
		default:
			indexes = append(indexes, index)
		}
	}
	slices.SortStableFunc(keys, func(x, y *Index) int {
		if x.IsPrimaryKey != y.IsPrimaryKey {
			if x.IsPrimaryKey {
				return -1
			}
			return 1
		}
		return cmp.Compare(x.Name, y.Name)
	})
	for _, key := range keys {
		statements.AlterTableStatements = append(statements.AlterTableStatements, addKey(table, key))
	}
	statements.AlterTableStatements = append(statements.AlterTableStatements, addChecks(table)...)

	// The foreign keys by name: those whose parent is among the tables.
	foreignKeys := slices.Clone(table.ForeignKeys)
	slices.SortFunc(foreignKeys, func(x, y *ForeignKey) int { return cmp.Compare(x.Name, y.Name) })
	for _, key := range foreignKeys {
		if b.tables[key.ReferencedID] == nil {
			b.skip(FkAlterTableLabel, childName(table, key.Name), fmt.Sprintf(
				"foreign key to %s, which is not among the requested tables",
				QualifiedName(key.ReferencedSchema, key.ReferencedTable),
			))
			continue
		}
		statements.AlterTableStatements = append(statements.AlterTableStatements, addForeignKey(table, key)...)
	}

	// The indexes in index_id order, then system versioning: a history kept for a limited
	// time needs the clustered index of its history table, which comes before.
	slices.SortStableFunc(indexes, func(x, y *Index) int { return cmp.Compare(x.IndexID, y.IndexID) })
	for _, index := range indexes {
		statements.IndexStatements = append(statements.IndexStatements, createIndex(table, index))
		if index.IsDisabled {
			statements.IndexStatements = append(statements.IndexStatements, disableIndex(table, index))
		}
	}
	if table.TemporalType == TemporalSystemVersioned {
		statements.IndexStatements = append(statements.IndexStatements, versioning(table))
	}
	b.plan.Tables = append(b.plan.Tables, statements)
}

// noticeLabels tells the block a kind of notice is missing from, when it is not the tables'.
var noticeLabels = map[string]string{
	NoticeFullTextIndex:    TableIndexLabel,
	NoticeColumnstoreOrder: TableIndexLabel,
	NoticeTriggerOrder:     TableTriggersLabel,
}

// notices reports the attributes of the tables that the plan leaves out.
func (b *builder) notices(s *Snapshot) {
	for _, notice := range s.Notices {
		table := b.tables[notice.ObjectID]
		if table == nil {
			continue
		}
		label, ok := noticeLabels[notice.Kind]
		if !ok {
			label = sqlmanager_shared.CreateTablesLabel
		}
		reason := notice.Kind + " not reproduced"
		switch {
		case notice.Detail != "":
			reason += ": " + notice.Detail
		case notice.Count > 0:
			reason += fmt.Sprintf(": %d", notice.Count)
		}
		b.skip(label, QualifiedName(table.Schema, table.Name), reason)
	}
}

// Blocks returns the eight labeled blocks, always all of them, in execution order.
func (p *Plan) Blocks() []*sqlmanager_shared.InitSchemaStatements {
	definitions := func(types ...[]*sqlmanager_shared.DataType) []string {
		statements := []string{}
		for _, list := range types {
			for _, dt := range list {
				statements = append(statements, dt.Definition)
			}
		}
		return statements
	}

	tables := []string{}
	alters := []string{}
	indexes := []string{}
	foreignKeys := []string{}
	for _, table := range p.Tables {
		tables = append(tables, table.CreateTableStatement)
		for _, alter := range table.AlterTableStatements {
			if alter.ConstraintType == sqlmanager_shared.ForeignConstraintType {
				foreignKeys = append(foreignKeys, alter.Statement)
			} else {
				alters = append(alters, alter.Statement)
			}
		}
		indexes = append(indexes, table.IndexStatements...)
	}

	triggers := []string{}
	for _, trigger := range p.Triggers {
		triggers = append(triggers, trigger.Definition)
		if trigger.EnabledState == triggerDisabled {
			triggers = append(triggers, disableTrigger(*trigger.TriggerSchema, trigger.Table, trigger.TriggerName))
		}
	}

	blocks := []*sqlmanager_shared.InitSchemaStatements{
		{Label: sqlmanager_shared.SchemasLabel, Statements: slices.Clone(p.Schemas)},
		{Label: DataTypesLabel, Statements: definitions(p.AliasTypes, p.Sequences, p.Functions)},
		{Label: sqlmanager_shared.CreateTablesLabel, Statements: tables},
		{Label: ViewsFunctionsLabel, Statements: definitions(p.Modules)},
		{Label: NonFkAlterTableLabel, Statements: alters},
		{Label: TableIndexLabel, Statements: indexes},
		{Label: FkAlterTableLabel, Statements: foreignKeys},
		{Label: TableTriggersLabel, Statements: triggers},
	}
	for _, block := range blocks {
		if block.Statements == nil {
			block.Statements = []string{}
		}
		for _, skipped := range p.Skipped {
			if skipped.Label == block.Label {
				block.Skipped = append(block.Skipped, &sqlmanager_shared.SkippedObject{
					Object: skipped.Object, Reason: skipped.Reason,
				})
			}
		}
	}
	return blocks
}
