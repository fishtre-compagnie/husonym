package ddl

import (
	"strings"
)

// Refusal is an object the plan cannot reproduce.
type Refusal struct{ Object, Reason string }

// RefusalError tells every object of a selection that the plan cannot reproduce as it is.
type RefusalError struct{ Refusals []Refusal }

// Error gives one line per refusal.
func (e *RefusalError) Error() string {
	lines := make([]string, len(e.Refusals))
	for i, refusal := range e.Refusals {
		lines[i] = refusal.Object + ": " + refusal.Reason
	}
	return strings.Join(lines, "\n")
}

// childName names what belongs to a table: a column, an index, a constraint.
func childName(table *Table, name string) string {
	return QualifiedName(table.Schema, table.Name) + "." + QuoteIdentifier(name)
}

// refusals lists every object of the snapshot that cannot be reproduced faithfully: creating
// something close would give the destination another object under the same name.
func refusals(s *Snapshot, sel *selection) []Refusal {
	refused := []Refusal{}
	for _, table := range s.Tables {
		if reason := tableRefusal(table); reason != "" {
			// A table that is refused is refused whole: its columns and indexes are not looked at.
			refused = append(refused, Refusal{Object: QualifiedName(table.Schema, table.Name), Reason: reason})
			continue
		}
		for _, column := range table.Columns {
			if reason := columnRefusal(column); reason != "" {
				refused = append(refused, Refusal{Object: childName(table, column.Name), Reason: reason})
			}
		}
		for _, index := range table.Indexes {
			if reason := indexRefusal(index); reason != "" {
				refused = append(refused, Refusal{Object: childName(table, index.Name), Reason: reason})
			}
		}
	}
	return append(refused, functionRefusals(s, sel)...)
}

func tableRefusal(table *Table) string {
	switch {
	case table.IsMemoryOptimized:
		return "memory-optimized table"
	case table.IsNode:
		return "graph node table"
	case table.IsEdge:
		return "graph edge table"
	case table.IsExternal:
		return "external table"
	case table.IsFileTable:
		return "FileTable"
	}
	for _, column := range table.Columns {
		if column.GeneratedAlways > GeneratedRowEnd {
			return "ledger table"
		}
	}
	return ""
}

func columnRefusal(column *Column) string {
	if column.IsComputed {
		return ""
	}
	typeName := QualifiedName(column.TypeSchema, column.TypeName)
	switch {
	case column.IsEncrypted:
		return "Always Encrypted column"
	case column.IsFilestream:
		return "FILESTREAM column"
	case column.HasXMLCollection:
		return "xml column bound to an XML schema collection"
	case column.IsUserDefinedType && column.IsAssemblyType:
		return "CLR user-defined type " + typeName
	case column.IsUserDefinedType && column.TypeHasRule:
		return "alias type " + typeName + " has a bound rule"
	case column.IsUserDefinedType && column.TypeHasDefault:
		return "alias type " + typeName + " has a bound default"
	case column.HasRule:
		return "a rule is bound to the column"
	case column.HasDefault && column.DefaultName == "":
		return "a stand-alone default is bound to the column"
	}
	// The type an alias is made from is written when the alias is created.
	written := column.TypeName
	if column.IsUserDefinedType {
		written = column.BaseTypeName
	}
	if !isSystemType(written) {
		return "type " + written + " is not supported"
	}
	// char, varchar, binary and varbinary are the types whose trailing blanks the setting decides.
	if !column.IsAnsiPadded && systemTypes[column.BaseTypeName] == shapeBytes {
		return "created under ANSI_PADDING OFF"
	}
	return ""
}

func indexRefusal(index *Index) string {
	switch {
	case index.Type == IndexHash:
		return "hash index"
	case index.IsDisabled && (index.Type == IndexClustered || index.Type == IndexClusteredColumnstore):
		return "disabled clustered index"
	case index.IsDisabled && (index.IsPrimaryKey || index.IsUniqueConstraint):
		return "disabled index backing a key"
	}
	return ""
}

// functionRefusals refuses the functions the tables call that cannot be created before them:
// those whose definition cannot be read, and those bound to a table, which would have to exist
// first.
func functionRefusals(s *Snapshot, sel *selection) []Refusal {
	boundTo := map[int64]*Dependency{}
	for _, d := range s.Dependencies {
		if d.ReferencedClass == ClassObject && d.ReferencedType == TypeTable && boundTo[d.ReferencingID] == nil {
			boundTo[d.ReferencingID] = d
		}
	}
	refused := []Refusal{}
	for _, function := range sel.tableFunctions {
		table := sel.neededBy[function.ObjectID]
		var reason string
		switch {
		case isCLR(function.Type):
			reason = "CLR function"
		case !function.HasDefinition:
			reason = "encrypted, its definition cannot be read"
		case function.IsSchemaBound && boundTo[function.ObjectID] != nil:
			d := boundTo[function.ObjectID]
			reason = "schema-bound to table " + QualifiedName(d.ReferencedSchema, d.ReferencedName)
		default:
			continue
		}
		refused = append(refused, Refusal{
			Object: QualifiedName(function.Schema, function.Name),
			Reason: "needed by table " + QualifiedName(table.Schema, table.Name) + ": " + reason,
		})
	}
	return refused
}
