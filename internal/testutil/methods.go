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

// InterfaceFields returns the types of the fields of a struct that are interfaces, in order. With
// MethodNames it pins what a struct can call: the interfaces it holds, and what each one offers.
func InterfaceFields(typ reflect.Type) []reflect.Type {
	var fields []reflect.Type
	for i := range typ.NumField() {
		if field := typ.Field(i).Type; field.Kind() == reflect.Interface {
			fields = append(fields, field)
		}
	}
	return fields
}

// FieldPackages returns the packages the types of a struct's fields come from, pointers
// followed, so that a test can refuse a field of a concrete type it did not mean to hold.
func FieldPackages(typ reflect.Type) []string {
	var packages []string
	for i := range typ.NumField() {
		field := typ.Field(i).Type
		for field.Kind() == reflect.Pointer {
			field = field.Elem()
		}
		if field.PkgPath() != "" {
			packages = append(packages, field.PkgPath())
		}
	}
	return packages
}
