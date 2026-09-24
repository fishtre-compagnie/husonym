package mcp_server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"google.golang.org/protobuf/encoding/protojson"
)

type mappingInput struct {
	Table       string         `json:"table"            jsonschema:"schema.table"`
	Column      string         `json:"column"`
	Transformer string         `json:"transformer"      jsonschema:"a system transformer, as suggest_mappings names it, such as generate_email; passthrough copies the value as it is"`
	Config      map[string]any `json:"config,omitempty" jsonschema:"fields of the transformer's configuration to set over its default one, named as the API names them, such as {\"preserve_length\": true}"`
}

// mappingKey names a column the way the mappings of a job are matched: schema, table, column.
type mappingKey struct {
	schema, table, column string
}

func (k mappingKey) String() string {
	return tableKey(k.schema, k.table) + "." + k.column
}

func keyOf(mapping *mgmtv1alpha1.JobMapping) mappingKey {
	return mappingKey{mapping.GetSchema(), mapping.GetTable(), mapping.GetColumn()}
}

// buildMappings checks each mapping against the columns of the source, and turns it into a
// mapping of the job, its transformer configured over the default of the catalogue.
func buildMappings(
	ctx context.Context,
	data *novalues.Reader,
	columns []*mgmtv1alpha1.DatabaseColumn,
	inputs []mappingInput,
) ([]*mgmtv1alpha1.JobMapping, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("no mapping given: give one per column, as schema.table and column")
	}
	known := map[string]*mgmtv1alpha1.DatabaseColumn{}
	for _, column := range columns {
		known[tableKey(column.GetSchema(), column.GetTable())+"."+column.GetColumn()] = column
	}

	seen := map[mappingKey]bool{}
	mappings := make([]*mgmtv1alpha1.JobMapping, 0, len(inputs))
	for _, input := range inputs {
		column, ok := known[input.Table+"."+input.Column]
		if !ok {
			return nil, fmt.Errorf(
				"no column %s in %s on the source: introspect_schema gives the columns of a table",
				input.Column, input.Table,
			)
		}
		mapping := &mgmtv1alpha1.JobMapping{Schema: column.GetSchema(), Table: column.GetTable(), Column: column.GetColumn()}
		if seen[keyOf(mapping)] {
			return nil, fmt.Errorf("%s is mapped twice: give one mapping per column", keyOf(mapping))
		}
		seen[keyOf(mapping)] = true

		source, ok := transformerSource(input.Transformer)
		if !ok {
			return nil, fmt.Errorf(
				"no system transformer %q for %s: name one as suggest_mappings does, such as generate_email",
				input.Transformer, keyOf(mapping),
			)
		}
		defaults, err := data.DefaultTransformer(ctx, source)
		if err != nil {
			if errors.Is(err, novalues.ErrRunsCode) {
				return nil, fmt.Errorf("%s: %s %w", keyOf(mapping), input.Transformer, err)
			}
			return nil, fmt.Errorf("unable to find the transformer %s: %w", input.Transformer, err)
		}
		config, err := configure(defaults, input.Config)
		if err != nil {
			return nil, fmt.Errorf("the config of %s for %s: %w", input.Transformer, keyOf(mapping), err)
		}
		if novalues.RunsCode(config) {
			return nil, fmt.Errorf("%s: %w", keyOf(mapping), novalues.ErrRunsCode)
		}
		mapping.Transformer = &mgmtv1alpha1.JobMappingTransformer{Config: config}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

// configure sets fields of a transformer's configuration over its defaults. Each field given
// replaces the default one whole, false and zero included; the API validates the result.
func configure(defaults *mgmtv1alpha1.TransformerConfig, fields map[string]any) (*mgmtv1alpha1.TransformerConfig, error) {
	if len(fields) == 0 {
		return defaults, nil
	}
	// The configuration is a oneof: its JSON has a single key, the kind of transformer, whose
	// value holds the fields.
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(defaults)
	if err != nil {
		return nil, err
	}
	var byKind map[string]map[string]any
	if err := json.Unmarshal(raw, &byKind); err != nil {
		return nil, err
	}
	if len(byKind) != 1 {
		return nil, fmt.Errorf("the catalogue gives no configuration to set fields on")
	}
	for kind, set := range byKind {
		if set == nil {
			set = map[string]any{}
		}
		maps.Copy(set, fields)
		byKind[kind] = set
	}
	raw, err = json.Marshal(byKind)
	if err != nil {
		return nil, err
	}
	config := &mgmtv1alpha1.TransformerConfig{}
	if err := protojson.Unmarshal(raw, config); err != nil {
		return nil, err
	}
	return config, nil
}

// checkComplete refuses mappings that leave a column of a table they touch unmapped. A column
// with no mapping would be copied or dropped as nobody decided; passthrough, when it is meant,
// is said.
func checkComplete(mappings []*mgmtv1alpha1.JobMapping, columns []*mgmtv1alpha1.DatabaseColumn) error {
	mapped := map[mappingKey]bool{}
	tables := map[string]bool{}
	for _, mapping := range mappings {
		mapped[keyOf(mapping)] = true
		tables[tableKey(mapping.GetSchema(), mapping.GetTable())] = true
	}
	var missing []string
	for _, column := range columns {
		key := mappingKey{column.GetSchema(), column.GetTable(), column.GetColumn()}
		if tables[tableKey(key.schema, key.table)] && !mapped[key] {
			missing = append(missing, key.String())
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf(
			"every column of a table the job reads takes a mapping, passthrough included, so that "+
				"nothing is copied in clear without someone deciding it; missing: %s",
			strings.Join(missing, ", "),
		)
	}
	return nil
}

// checkMappings has the API check mappings against their source, as it checks them for the UI,
// and refuses them with what it found: a transformer that does not fit its column, a required
// column or foreign key left out, a cycle. With tables given, only what concerns those tables
// and the database as a whole is held against them: a table of the job a change does not touch
// is not the agent's to answer for. Without, everything is.
func checkMappings(
	ctx context.Context,
	data *novalues.Reader,
	connectionId string,
	source *mgmtv1alpha1.JobSource,
	mappings []*mgmtv1alpha1.JobMapping,
	virtualForeignKeys []*mgmtv1alpha1.VirtualForeignConstraint,
	tables []string,
) error {
	res, err := data.ValidateMappings(ctx, connectionId, source, mappings, virtualForeignKeys)
	if err != nil {
		return fmt.Errorf("unable to check the mappings: %w", err)
	}
	var found []string
	for _, report := range res.GetDatabaseErrors().GetErrorReports() {
		found = append(found, report.GetMessage())
	}
	for _, table := range res.GetTableErrors() {
		key := tableKey(table.GetSchema(), table.GetTable())
		if tables != nil && !slices.Contains(tables, key) {
			continue
		}
		for _, report := range table.GetErrorReports() {
			found = append(found, key+": "+report.GetMessage())
		}
	}
	for _, column := range res.GetColumnErrors() {
		key := tableKey(column.GetSchema(), column.GetTable())
		if tables != nil && !slices.Contains(tables, key) {
			continue
		}
		for _, report := range column.GetErrorReports() {
			found = append(found, key+"."+column.GetColumn()+": "+report.GetMessage())
		}
	}
	if len(found) == 0 {
		return nil
	}
	slices.Sort(found)
	return fmt.Errorf("the API refuses these mappings — %s", strings.Join(found, "; "))
}

// mappedTables lists the tables mappings touch, sorted, as schema.table.
func mappedTables(mappings []*mgmtv1alpha1.JobMapping) []string {
	tables := map[string]bool{}
	for _, mapping := range mappings {
		tables[tableKey(mapping.GetSchema(), mapping.GetTable())] = true
	}
	return slices.Sorted(maps.Keys(tables))
}
