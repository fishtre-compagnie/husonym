package ddl

import (
	"cmp"
	"slices"
)

// byName orders modules by schema then name, byte by byte: the order depends on no collation.
func byName(a, b *Module) int {
	return cmp.Or(cmp.Compare(a.Schema, b.Schema), cmp.Compare(a.Name, b.Name))
}

// orderModules puts each module after every module of the set it references. Among the modules
// that are ready the order is by schema then name. Modules that reference each other in a
// cycle have no such order: each comes when nothing else is ready, by schema then name.
// references gives, by object id, the ids a module references; ids outside the set and a
// module's own id are ignored.
func orderModules(modules []*Module, references map[int64][]int64) []*Module {
	pending := slices.Clone(modules)
	slices.SortFunc(pending, byName)

	waiting := make(map[int64]bool, len(pending))
	for _, m := range pending {
		waiting[m.ObjectID] = true
	}
	ready := func(m *Module) bool {
		for _, id := range references[m.ObjectID] {
			if id != m.ObjectID && waiting[id] {
				return false
			}
		}
		return true
	}

	ordered := make([]*Module, 0, len(pending))
	for len(pending) > 0 {
		next := slices.IndexFunc(pending, ready)
		if next == -1 {
			next = 0
		}
		m := pending[next]
		ordered = append(ordered, m)
		delete(waiting, m.ObjectID)
		pending = slices.Delete(pending, next, next+1)
	}
	return ordered
}
