package sqlmanager_mssql

import (
	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
)

// The rows of the querier become the facts of a snapshot here, and nowhere else.

func toTable(row *mssql_queries.ResolveTablesRow) *ddl.Table {
	return &ddl.Table{
		ObjectID:          row.ObjectID,
		Schema:            row.Schema,
		Name:              row.Name,
		TemporalType:      row.TemporalType,
		HistoryID:         row.HistoryID,
		HistorySchema:     row.HistorySchema,
		HistoryName:       row.HistoryName,
		RetentionPeriod:   row.RetentionPeriod,
		RetentionUnit:     row.RetentionUnit,
		PeriodStartColumn: row.PeriodStartColumn,
		PeriodEndColumn:   row.PeriodEndColumn,
		IsMemoryOptimized: row.IsMemoryOptimized,
		IsFileTable:       row.IsFileTable,
		IsExternal:        row.IsExternal,
		IsNode:            row.IsNode,
		IsEdge:            row.IsEdge,
		AnsiNullsOff:      row.AnsiNullsOff,
	}
}

// toHistoryTable is the history table a system-versioned table names.
func toHistoryTable(row *mssql_queries.ResolveTablesRow) *ddl.Table {
	return &ddl.Table{
		ObjectID:     row.HistoryID,
		Schema:       row.HistorySchema,
		Name:         row.HistoryName,
		TemporalType: ddl.TemporalHistory,
		AnsiNullsOff: row.HistoryAnsiNullsOff,
	}
}

func toColumn(row *mssql_queries.GetColumnsRow) *ddl.Column {
	base := row.BaseTypeName
	if base == "" && !row.IsUserDefinedType {
		// A system type held by an assembly is stored as itself.
		base = row.TypeName
	}
	return &ddl.Column{
		ColumnID:                  row.ColumnID,
		Name:                      row.Name,
		TypeSchema:                row.TypeSchema,
		TypeName:                  row.TypeName,
		BaseTypeName:              base,
		IsUserDefinedType:         row.IsUserDefinedType,
		IsAssemblyType:            row.IsAssemblyType,
		TypeIsNullable:            row.TypeIsNullable,
		TypeHasRule:               row.TypeRuleID != 0,
		TypeHasDefault:            row.TypeDefaultID != 0,
		MaxLength:                 row.MaxLength,
		Precision:                 row.Precision,
		Scale:                     row.Scale,
		Collation:                 row.Collation,
		IsNullable:                row.IsNullable,
		IsAnsiPadded:              row.IsAnsiPadded,
		IsRowGuidCol:              row.IsRowGuidCol,
		IsFilestream:              row.IsFilestream,
		IsSparse:                  row.IsSparse,
		IsColumnSet:               row.IsColumnSet,
		IsHidden:                  row.IsHidden,
		IsEncrypted:               row.IsEncrypted,
		HasXMLCollection:          row.XMLCollectionID != 0,
		HasRule:                   row.RuleID != 0,
		GeneratedAlways:           row.GeneratedAlways,
		HasDefault:                row.DefaultID != 0,
		DefaultName:               row.DefaultName,
		DefaultDefinition:         row.DefaultDefinition,
		IsComputed:                row.IsComputed,
		ComputedDefinition:        row.ComputedDefinition,
		IsPersisted:               row.IsPersisted,
		IsIdentity:                row.IsIdentity,
		IdentitySeed:              row.IdentitySeed,
		IdentityIncrement:         row.IdentityIncrement,
		IdentityNotForReplication: row.IdentityNotForReplication,
		IsMasked:                  row.IsMasked,
		MaskingFunction:           row.MaskingFunction,
	}
}

// groupIndexes gathers the rows of the index columns into indexes, by table, in index_id order.
func groupIndexes(rows []*mssql_queries.GetIndexesRow) map[int64][]*ddl.Index {
	grouped := map[int64][]*ddl.Index{}
	var current *ddl.Index
	var table int64
	for _, row := range rows {
		if current == nil || table != row.ObjectID || current.IndexID != row.IndexID {
			current = &ddl.Index{
				IndexID:            row.IndexID,
				Name:               row.Name,
				Type:               row.Type,
				IsUnique:           row.IsUnique,
				IsPrimaryKey:       row.IsPrimaryKey,
				IsUniqueConstraint: row.IsUniqueConstraint,
				IsDisabled:         row.IsDisabled,
				IsPadded:           row.IsPadded,
				IgnoreDupKey:       row.IgnoreDupKey,
				AllowRowLocks:      row.AllowRowLocks,
				AllowPageLocks:     row.AllowPageLocks,
				FillFactor:         row.FillFactor,
				HasFilter:          row.HasFilter,
				FilterDefinition:   row.FilterDefinition,
			}
			table = row.ObjectID
			grouped[table] = append(grouped[table], current)
		}
		if row.IndexColumnID != 0 {
			current.Columns = append(current.Columns, &ddl.IndexColumn{
				Name:         row.ColumnName,
				KeyOrdinal:   row.KeyOrdinal,
				IsDescending: row.IsDescending,
				IsIncluded:   row.IsIncluded,
			})
		}
	}
	return grouped
}

// groupForeignKeys gathers the rows of the key columns into foreign keys, by table.
func groupForeignKeys(rows []*mssql_queries.GetForeignKeysRow) map[int64][]*ddl.ForeignKey {
	grouped := map[int64][]*ddl.ForeignKey{}
	var current *ddl.ForeignKey
	var constraint int64
	for _, row := range rows {
		if current == nil || constraint != row.ConstraintID {
			current = &ddl.ForeignKey{
				Name:                row.Name,
				ReferencedID:        row.ReferencedID,
				ReferencedSchema:    row.ReferencedSchema,
				ReferencedTable:     row.ReferencedTable,
				DeleteAction:        row.DeleteAction,
				UpdateAction:        row.UpdateAction,
				IsDisabled:          row.IsDisabled,
				IsNotTrusted:        row.IsNotTrusted,
				IsNotForReplication: row.IsNotForReplication,
			}
			constraint = row.ConstraintID
			grouped[row.ObjectID] = append(grouped[row.ObjectID], current)
		}
		current.Columns = append(current.Columns, &ddl.ForeignKeyColumn{
			Name:           row.ColumnName,
			IsNullable:     row.ColumnIsNullable,
			ReferencedName: row.ReferencedColumn,
		})
	}
	return grouped
}

func toCheck(row *mssql_queries.GetCheckConstraintsRow) *ddl.CheckConstraint {
	return &ddl.CheckConstraint{
		Name:                row.Name,
		Definition:          row.Definition,
		IsDisabled:          row.IsDisabled,
		IsNotTrusted:        row.IsNotTrusted,
		IsNotForReplication: row.IsNotForReplication,
	}
}

func toModule(row *mssql_queries.GetModuleHeadersRow) *ddl.Module {
	return &ddl.Module{
		ObjectID:             row.ObjectID,
		Schema:               row.Schema,
		Name:                 row.Name,
		Type:                 row.Type,
		ParentID:             row.ParentID,
		UsesAnsiNulls:        row.UsesAnsiNulls,
		UsesQuotedIdentifier: row.UsesQuotedIdentifier,
		IsSchemaBound:        row.IsSchemaBound,
		HasDefinition:        row.HasDefinition,
		IsDisabled:           row.IsDisabled,
		HasIndex:             row.HasIndex,
	}
}

// toTrigger is a trigger read with its text, in the schema of its table.
func toTrigger(row *mssql_queries.GetTableTriggersRow) *ddl.Module {
	return &ddl.Module{
		ObjectID:             row.ObjectID,
		Schema:               row.TableSchema,
		Name:                 row.Name,
		Type:                 row.Type,
		ParentID:             row.ParentID,
		UsesAnsiNulls:        row.UsesAnsiNulls,
		UsesQuotedIdentifier: row.UsesQuotedIdentifier,
		HasDefinition:        row.HasDefinition,
		IsDisabled:           row.IsDisabled,
		Definition:           row.Definition,
	}
}

func toDependency(row *mssql_queries.GetDependenciesRow) *ddl.Dependency {
	return &ddl.Dependency{
		ReferencingID:            row.ReferencingID,
		ReferencingType:          row.ReferencingType,
		ReferencingParentID:      row.ReferencingParentID,
		ReferencedClass:          row.ReferencedClass,
		ReferencedID:             row.ReferencedID,
		ReferencedType:           row.ReferencedType,
		ReferencedIsTableType:    row.ReferencedIsTableType,
		ReferencedIsAssemblyType: row.ReferencedIsAssemblyType,
		ReferencedSchema:         row.ReferencedSchema,
		ReferencedName:           row.ReferencedName,
	}
}

func toSequence(row *mssql_queries.GetSequencesRow) *ddl.Sequence {
	return &ddl.Sequence{
		ObjectID:          row.ObjectID,
		Schema:            row.Schema,
		Name:              row.Name,
		TypeSchema:        row.TypeSchema,
		TypeName:          row.TypeName,
		BaseTypeName:      row.BaseTypeName,
		IsUserDefinedType: row.IsUserDefinedType,
		TypeIsNullable:    row.TypeIsNullable,
		Precision:         row.Precision,
		Scale:             row.Scale,
		StartValue:        row.StartValue,
		Increment:         row.Increment,
		MinimumValue:      row.MinimumValue,
		MaximumValue:      row.MaximumValue,
		CurrentValue:      row.CurrentValue,
		IsCycling:         row.IsCycling,
		IsCached:          row.IsCached,
		CacheSize:         row.CacheSize,
		IsUsed:            row.IsUsed,
	}
}

func toNotice(row *mssql_queries.GetTableNoticesRow) *ddl.Notice {
	return &ddl.Notice{ObjectID: row.ObjectID, Kind: row.Kind, Detail: row.Detail, Count: row.Count}
}

// attachDefinitions gives the wanted modules their text. A wanted module whose text did not
// come back cannot be read: it was dropped or encrypted since its header was read.
func attachDefinitions(
	snapshot *ddl.Snapshot,
	wanted []int64,
	definitions []*mssql_queries.GetModuleDefinitionsRow,
) {
	texts := make(map[int64]string, len(definitions))
	for _, row := range definitions {
		texts[row.ObjectID] = row.Definition
	}
	isWanted := make(map[int64]bool, len(wanted))
	for _, id := range wanted {
		isWanted[id] = true
	}
	for _, module := range snapshot.Modules {
		if !isWanted[module.ObjectID] {
			continue
		}
		text, ok := texts[module.ObjectID]
		module.Definition = text
		module.HasDefinition = module.HasDefinition && ok
	}
}
