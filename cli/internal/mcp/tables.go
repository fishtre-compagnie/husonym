package mcp_server

import (
	"fmt"
	"slices"
	"strings"
)

// maxTablesPerCall bounds what one call describes, so that a schema of four hundred tables is
// read in steps rather than poured into the model's context at once.
const maxTablesPerCall = 20

// tableKey names a table the way the API keys its maps: schema.table.
func tableKey(schema, table string) string {
	return schema + "." + table
}

// selectTables checks the tables asked for against those that exist, and returns them sorted
// and without repeats.
func selectTables(asked []string, known map[string]bool) ([]string, error) {
	selected := slices.Compact(slices.Sorted(slices.Values(asked)))
	if len(selected) == 0 {
		return nil, fmt.Errorf("no table given: name at least one, as schema.table")
	}
	if len(selected) > maxTablesPerCall {
		return nil, fmt.Errorf(
			"%d tables asked for, at most %d per call: split the call", len(selected), maxTablesPerCall,
		)
	}
	var unknown []string
	for _, table := range selected {
		if !known[table] {
			unknown = append(unknown, table)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf(
			"no table %s on this connection: introspect_schema without tables lists them, as schema.table",
			strings.Join(unknown, ", "),
		)
	}
	return selected, nil
}

// enumLabel turns a proto enum name into the word a model reads: PII_CONFIDENCE_NEEDS_REVIEW
// with its prefix gives needs_review. The unspecified value gives nothing.
func enumLabel(name, prefix string) string {
	label := strings.TrimPrefix(name, prefix)
	if label == "UNSPECIFIED" {
		return ""
	}
	return strings.ToLower(label)
}
