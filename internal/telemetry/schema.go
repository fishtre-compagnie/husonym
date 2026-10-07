package telemetry

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// schemaFile is the published JSON Schema of version 1 of the report.
//
//go:embed schema/usage-report.v1.schema.json
var schemaFile []byte

// Schema is the published JSON Schema of the report, as the file holds it.
func Schema() []byte { return bytes.Clone(schemaFile) }

var resolveSchema = sync.OnceValues(func() (*jsonschema.Resolved, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaFile, &schema); err != nil {
		return nil, fmt.Errorf("reading the schema of the usage report: %w", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolving the schema of the usage report: %w", err)
	}
	return resolved, nil
})

// Validate checks a document against the schema of the report: a field the schema does not know,
// a value outside of its list or a malformed date is an error.
func Validate(document []byte) error {
	resolved, err := resolveSchema()
	if err != nil {
		return err
	}
	var instance any
	if err := json.Unmarshal(document, &instance); err != nil {
		return fmt.Errorf("the usage report is not JSON: %w", err)
	}
	if err := resolved.Validate(instance); err != nil {
		return fmt.Errorf("the usage report does not match its schema: %w", err)
	}
	return nil
}
