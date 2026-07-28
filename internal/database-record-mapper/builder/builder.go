package builder

import (
	"fmt"

	husonym_types "github.com/fishtre-compagnie/husonym/internal/types"
)

type DatabaseRecordMapper[T any] interface {
	// MapRecord returns a map where:
	// - Keys are column/field names from the database record
	// - Values are either Go native types (string, int, etc.) or Husonym custom types
	MapRecord(record T) (map[string]any, error)

	// deprecated - use MapRecord instead with husonym types
	MapRecordWithKeyType(
		record T,
	) (valuemap map[string]any, typemap map[string]husonym_types.KeyType, err error)
}

type Builder[T any] struct {
	Mapper DatabaseRecordMapper[T]
}

func (b *Builder[T]) MapRecord(record any) (map[string]any, error) {
	typedRecord, ok := record.(T)
	if !ok {
		return nil, fmt.Errorf("invalid record type: expected %T, got %T", *new(T), record)
	}
	return b.Mapper.MapRecord(typedRecord)
}

// deprecated - use MapRecord instead with husonym types
func (b *Builder[T]) MapRecordWithKeyType(
	record any,
) (valuemap map[string]any, typemap map[string]husonym_types.KeyType, err error) {
	typedRecord, ok := record.(T)
	if !ok {
		return nil, nil, fmt.Errorf("invalid record type: expected %T, got %T", *new(T), record)
	}
	return b.Mapper.MapRecordWithKeyType(typedRecord)
}
