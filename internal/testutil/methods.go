package testutil

import (
	"reflect"
	"slices"
)

// MethodNames returns the names of the methods of a type, sorted. It lets a test pin the
// method set of an interface, so that widening it fails a test instead of passing unseen.
func MethodNames(typ reflect.Type) []string {
	names := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		names = append(names, typ.Method(i).Name)
	}
	slices.Sort(names)
	return names
}
