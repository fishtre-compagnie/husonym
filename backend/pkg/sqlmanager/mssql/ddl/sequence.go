package ddl

import (
	"fmt"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// createSequence writes a sequence at its declared start, every clause spelled out.
func createSequence(sequence *Sequence) *sqlmanager_shared.DataType {
	typeName := QualifiedName(sequence.TypeSchema, sequence.TypeName)
	if !sequence.IsUserDefinedType {
		typeName = systemType(sequence.TypeName, 0, sequence.Precision, sequence.Scale)
	}
	cycle := "NO CYCLE"
	if sequence.IsCycling {
		cycle = "CYCLE"
	}
	cache := "NO CACHE"
	if sequence.IsCached {
		cache = "CACHE"
		if sequence.CacheSize > 0 {
			cache = fmt.Sprintf("CACHE %d", sequence.CacheSize)
		}
	}
	return &sqlmanager_shared.DataType{
		Schema: sequence.Schema,
		Name:   sequence.Name,
		Definition: guarded(
			objectMissing(sequence.Schema, sequence.Name, TypeSequence),
			fmt.Sprintf(
				"CREATE SEQUENCE %s AS %s START WITH %s INCREMENT BY %s MINVALUE %s MAXVALUE %s %s %s",
				QualifiedName(sequence.Schema, sequence.Name), typeName,
				sequence.StartValue, sequence.Increment, sequence.MinimumValue, sequence.MaximumValue,
				cycle, cache,
			),
		),
	}
}
