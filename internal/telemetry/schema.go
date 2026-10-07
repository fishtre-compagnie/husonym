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

// periodSchemaFile is the published JSON Schema of version 1 of the report for a period.
//
//go:embed schema/usage-period-report.v1.schema.json
var periodSchemaFile []byte

// Schema is the published JSON Schema of the report, as the file holds it.
func Schema() []byte { return bytes.Clone(schemaFile) }

var (
	resolveSchema       = resolver(schemaFile, "the usage report")
	resolvePeriodSchema = resolver(periodSchemaFile, "the usage report for a period")
)

// resolver reads a schema file once, the first time a document is checked against it.
func resolver(file []byte, what string) func() (*jsonschema.Resolved, error) {
	return sync.OnceValues(func() (*jsonschema.Resolved, error) {
		var schema jsonschema.Schema
		if err := json.Unmarshal(file, &schema); err != nil {
			return nil, fmt.Errorf("reading the schema of %s: %w", what, err)
		}
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return nil, fmt.Errorf("resolving the schema of %s: %w", what, err)
		}
		return resolved, nil
	})
}

// Validate checks a document against the schema of the report: a field the schema does not know,
// a value outside of its list or a malformed date is an error.
func Validate(document []byte) error {
	return validate(resolveSchema, "the usage report", document)
}

// ValidatePeriod checks a document against the schema of the report for a period, the way
// Validate checks the report of a day.
func ValidatePeriod(document []byte) error {
	return validate(resolvePeriodSchema, "the usage report for a period", document)
}

func validate(resolve func() (*jsonschema.Resolved, error), what string, document []byte) error {
	resolved, err := resolve()
	if err != nil {
		return err
	}
	var instance any
	if err := json.Unmarshal(document, &instance); err != nil {
		return fmt.Errorf("%s is not JSON: %w", what, err)
	}
	if err := resolved.Validate(instance); err != nil {
		return fmt.Errorf("%s does not match its schema: %w", what, err)
	}
	return nil
}
