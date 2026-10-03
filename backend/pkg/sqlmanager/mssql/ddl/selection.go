package ddl

import (
	"cmp"
	"slices"
)

// selection is what the requested tables bring with them: the functions they call, the
// sequences their defaults draw from, the views, functions and procedures of their schemas
// whose every dependency is reproduced, and the triggers of those tables and views.
type selection struct {
	// tableFunctions are the functions the tables call, with the functions those call, in the
	// order they are created in: before the tables.
	tableFunctions []*Module
	// neededBy tells, by the id of a function the tables call, the first table that needs it.
	neededBy map[int64]*Table
	// modules are the other views, functions and procedures, in the order they are created in.
	modules []*Module
	// triggers come by parent, then by name.
	triggers []*trigger
	// sequences holds the ids of the sequences the defaults of the tables draw from.
	sequences map[int64]bool
	skipped   []*Skipped
}

// trigger is a trigger with the table or view it belongs to.
type trigger struct {
	module *Module
	parent string
}

func isFunction(typeCode string) bool {
	switch typeCode {
	case TypeScalarFunction, TypeInlineFunction, TypeTableFunction, TypeCLRScalar, TypeCLRTableValued:
		return true
	}
	return false
}

// isCLR tells the type codes of the modules an assembly holds: they have no text.
func isCLR(typeCode string) bool {
	switch typeCode {
	case TypeCLRScalar, TypeCLRTableValued, TypeCLRProcedure, TypeCLRTrigger, TypeCLRAggregate:
		return true
	}
	return false
}

// isSchemaModule tells the type codes of the views, functions and procedures.
func isSchemaModule(typeCode string) bool {
	switch typeCode {
	case TypeView, TypeProcedure, TypeCLRProcedure, TypeCLRAggregate:
		return true
	}
	return isFunction(typeCode)
}

func isTrigger(typeCode string) bool {
	return typeCode == TypeTrigger || typeCode == TypeCLRTrigger
}

// unreadable tells why the definition of a module cannot be read, or nothing when it can.
func unreadable(module *Module) string {
	switch {
	case isCLR(module.Type):
		return "CLR module: it has no definition to read"
	case !module.HasDefinition:
		return "encrypted: its definition cannot be read"
	}
	return ""
}

// selectObjects works out the selection of a snapshot. It reads the headers of the modules and
// the dependencies only, never a definition: it tells which definitions are worth reading.
func selectObjects(s *Snapshot) *selection {
	sel := &selection{neededBy: map[int64]*Table{}, sequences: map[int64]bool{}}

	tables := make(map[int64]*Table, len(s.Tables))
	schemas := map[string]bool{}
	for _, table := range s.Tables {
		tables[table.ObjectID] = table
		schemas[table.Schema] = true
	}
	modules := make(map[int64]*Module, len(s.Modules))
	for _, m := range s.Modules {
		modules[m.ObjectID] = m
	}
	references := map[int64][]*Dependency{}
	for _, d := range s.Dependencies {
		if d.ReferencedID != 0 {
			references[d.ReferencingID] = append(references[d.ReferencingID], d)
		}
	}
	for _, list := range references {
		slices.SortStableFunc(list, func(a, b *Dependency) int {
			return cmp.Or(cmp.Compare(a.ReferencedSchema, b.ReferencedSchema), cmp.Compare(a.ReferencedName, b.ReferencedName))
		})
	}

	// What the tables themselves reference: through a computed column the table is the
	// referencing object, through a check or a default the constraint is, and the table is its
	// parent.
	called := []*Module{}
	for _, d := range s.Dependencies {
		var table *Table
		switch d.ReferencingType {
		case TypeTable:
			table = tables[d.ReferencingID]
		case TypeCheck, TypeDefault:
			table = tables[d.ReferencingParentID]
		}
		if table == nil || d.ReferencedID == 0 || d.ReferencedClass != ClassObject {
			continue
		}
		if d.ReferencedType == TypeSequence && d.ReferencingType == TypeDefault {
			sel.sequences[d.ReferencedID] = true
		}
		if m := modules[d.ReferencedID]; m != nil && isFunction(m.Type) && sel.neededBy[m.ObjectID] == nil {
			sel.neededBy[m.ObjectID] = table
			called = append(called, m)
		}
	}
	// The functions those functions call are needed by the same table.
	for i := 0; i < len(called); i++ {
		for _, d := range references[called[i].ObjectID] {
			if m := modules[d.ReferencedID]; m != nil && isFunction(m.Type) && sel.neededBy[m.ObjectID] == nil {
				sel.neededBy[m.ObjectID] = sel.neededBy[called[i].ObjectID]
				called = append(called, m)
			}
		}
	}

	// The views, functions and procedures of the schemas that hold a table, less those that
	// cannot be read.
	kept := map[int64]*Module{}
	candidates := []*Module{}
	for _, m := range s.Modules {
		if !isSchemaModule(m.Type) || !schemas[m.Schema] || sel.neededBy[m.ObjectID] != nil {
			continue
		}
		if reason := unreadable(m); reason != "" {
			sel.skip(ViewsFunctionsLabel, QualifiedName(m.Schema, m.Name), reason)
			continue
		}
		kept[m.ObjectID] = m
		candidates = append(candidates, m)
	}
	slices.SortFunc(candidates, byName)

	// A module goes when it depends on what is not reproduced; those that depend on it follow,
	// until nothing changes.
	reproduced := func(d *Dependency) bool {
		return kept[d.ReferencedID] != nil || sel.neededBy[d.ReferencedID] != nil
	}
	for changed := true; changed; {
		changed = false
		for _, m := range candidates {
			if kept[m.ObjectID] == nil {
				continue
			}
			for _, d := range references[m.ObjectID] {
				if d.ReferencedID == m.ObjectID && d.ReferencedClass == ClassObject {
					continue
				}
				if reason := missingDependency(d, tables, reproduced); reason != "" {
					sel.skip(ViewsFunctionsLabel, QualifiedName(m.Schema, m.Name), reason)
					delete(kept, m.ObjectID)
					changed = true
					break
				}
			}
		}
	}

	moduleReferences := func(set []*Module) map[int64][]int64 {
		ids := make(map[int64][]int64, len(set))
		for _, m := range set {
			for _, d := range references[m.ObjectID] {
				if d.ReferencedClass == ClassObject {
					ids[m.ObjectID] = append(ids[m.ObjectID], d.ReferencedID)
				}
			}
		}
		return ids
	}
	sel.tableFunctions = orderModules(called, moduleReferences(called))
	remaining := slices.DeleteFunc(candidates, func(m *Module) bool { return kept[m.ObjectID] == nil })
	sel.modules = orderModules(remaining, moduleReferences(remaining))

	// The triggers of the tables and of the views that are kept.
	for _, m := range s.Modules {
		if !isTrigger(m.Type) {
			continue
		}
		var parent string
		if table := tables[m.ParentID]; table != nil {
			parent = table.Name
		} else if view := kept[m.ParentID]; view != nil {
			parent = view.Name
		} else {
			continue
		}
		if reason := unreadable(m); reason != "" {
			sel.skip(TableTriggersLabel, QualifiedName(m.Schema, m.Name), reason)
			continue
		}
		sel.triggers = append(sel.triggers, &trigger{module: m, parent: parent})
	}
	slices.SortFunc(sel.triggers, func(a, b *trigger) int {
		return cmp.Or(
			cmp.Compare(a.module.Schema, b.module.Schema),
			cmp.Compare(a.parent, b.parent),
			cmp.Compare(a.module.Name, b.module.Name),
		)
	})
	return sel
}

func (sel *selection) skip(label, object, reason string) {
	sel.skipped = append(sel.skipped, &Skipped{Label: label, Object: object, Reason: reason})
}

// missingDependency tells why a module cannot be created on a destination that holds the
// selection only: what it references that the plan does not reproduce. References the server
// did not resolve are not given to it: the server resolves them when the module runs.
func missingDependency(d *Dependency, tables map[int64]*Table, reproduced func(*Dependency) bool) string {
	name := QualifiedName(d.ReferencedSchema, d.ReferencedName)
	if d.ReferencedClass == ClassType {
		switch {
		case d.ReferencedIsTableType:
			return "depends on table type " + name
		case d.ReferencedIsAssemblyType:
			return "depends on CLR type " + name
		}
		return ""
	}
	if d.ReferencedClass != ClassObject {
		return ""
	}
	switch {
	case d.ReferencedType == TypeTable:
		if tables[d.ReferencedID] == nil {
			return "depends on table " + name + ", which is outside the selection"
		}
	case d.ReferencedType == TypeSynonym:
		return "depends on synonym " + name
	case isCLR(d.ReferencedType):
		return "depends on CLR object " + name
	case isSchemaModule(d.ReferencedType):
		if !reproduced(d) {
			return "depends on " + name + ", which is not reproduced"
		}
	}
	return ""
}

// Wanted tells what the tables of the snapshot bring with them, by object id, in ascending
// order: the modules whose definition is to be read, and the sequences to read. It needs the
// tables, the module headers and the dependencies of the snapshot, nothing else.
func (s *Snapshot) Wanted() (modules, sequences []int64) {
	sel := selectObjects(s)
	modules = []int64{}
	for _, m := range sel.tableFunctions {
		modules = append(modules, m.ObjectID)
	}
	for _, m := range sel.modules {
		modules = append(modules, m.ObjectID)
	}
	for _, t := range sel.triggers {
		modules = append(modules, t.module.ObjectID)
	}
	sequences = []int64{}
	for id := range sel.sequences {
		sequences = append(sequences, id)
	}
	slices.Sort(modules)
	slices.Sort(sequences)
	return modules, sequences
}
