package mcp_server

import (
	"context"
	"fmt"
	"maps"
	"slices"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type suggestMappingsInput struct {
	ConnectionId string   `json:"connection_id"          jsonschema:"the id of the source connection, as list_connections gives it"`
	Tables       []string `json:"tables"                 jsonschema:"tables to look at, as schema.table, at most 20 per call"`
	ScanContent  bool     `json:"scan_content,omitempty" jsonschema:"also have the server scan a sample of each table for PII: slower, catches what column names hide, and returns what it found, never a value"`
}

type suggestMappingsOutput struct {
	Tables []tableSuggestions `json:"tables"`
}

type tableSuggestions struct {
	Table     string             `json:"table"                jsonschema:"schema.table"`
	Columns   []columnSuggestion `json:"columns"`
	ScanError string             `json:"scan_error,omitempty" jsonschema:"why the content scan failed on this table; its suggestions then come from column names alone"`
}

type columnSuggestion struct {
	Column               string   `json:"column"`
	Sensitive            bool     `json:"sensitive"                       jsonschema:"true when the column holds personal data"`
	Category             string   `json:"category,omitempty"              jsonschema:"what the data is, such as email or nir"`
	SuggestedTransformer string   `json:"suggested_transformer,omitempty" jsonschema:"the transformer source that fits the category, such as generate_email"`
	Confidence           string   `json:"confidence,omitempty"            jsonschema:"confirmed: proven, may be applied as is; needs_review: a clue, to put to a person before acting on it"`
	Method               string   `json:"method,omitempty"                jsonschema:"how it was found: column_name, checksum, content or format"`
	Evidence             string   `json:"evidence,omitempty"              jsonschema:"the proof, in words"`
	Keys                 []string `json:"keys,omitempty"                  jsonschema:"primary, foreign and referenced: the keys the column takes part in; see the tool description"`
}

func addSuggestMappings(server *mcp.Server, reader *novalues.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "suggest_mappings",
		Description: "Say which columns hold personal data and which transformer fits each, with how sure " +
			"the detection is and why. A column in a key is flagged: transformed on its own, it breaks the " +
			"references between tables, so it takes the same treatment on both sides of each reference — " +
			"never apply a suggestion to one blindly.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, suggestMappings(reader))
}

func suggestMappings(reader *novalues.Reader) mcp.ToolHandlerFor[suggestMappingsInput, suggestMappingsOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input suggestMappingsInput,
	) (*mcp.CallToolResult, suggestMappingsOutput, error) {
		columns, err := reader.Columns(ctx, input.ConnectionId)
		if err != nil {
			return nil, suggestMappingsOutput{}, fmt.Errorf(
				"unable to read the schema of connection %s: %w", input.ConnectionId, err,
			)
		}
		columnsByTable := map[string][]*mgmtv1alpha1.DatabaseColumn{}
		known := map[string]bool{}
		for _, column := range columns {
			key := tableKey(column.GetSchema(), column.GetTable())
			columnsByTable[key] = append(columnsByTable[key], column)
			known[key] = true
		}
		selected, err := selectTables(input.Tables, known)
		if err != nil {
			return nil, suggestMappingsOutput{}, err
		}
		constraints, err := reader.Constraints(ctx, input.ConnectionId)
		if err != nil {
			return nil, suggestMappingsOutput{}, fmt.Errorf(
				"unable to read the keys of connection %s: %w", input.ConnectionId, err,
			)
		}

		referencedBy := referencesByTable(constraints.GetForeignKeyConstraints())

		out := make([]tableSuggestions, 0, len(selected))
		for _, table := range selected {
			tableColumns := columnsByTable[table]
			suggestions := tableSuggestions{Table: table}

			content := map[string]*mgmtv1alpha1.ColumnPiiDetection{}
			if input.ScanContent {
				// One table that fails — too large, locked, not readable — must not cost the
				// others their scan: its failure is reported and its names still speak.
				detections, err := reader.DetectPii(
					ctx, input.ConnectionId, tableColumns[0].GetSchema(), tableColumns[0].GetTable(),
				)
				if err != nil {
					suggestions.ScanError = err.Error()
				}
				for _, detection := range detections {
					content[detection.GetColumn()] = detection
				}
			}

			keys := keyColumns(constraints, referencedBy[table], table)
			for _, column := range tableColumns {
				suggestion := resolvePii(column, content[column.GetColumn()])
				suggestion.Keys = keys[column.GetColumn()]
				suggestions.Columns = append(suggestions.Columns, suggestion)
			}
			out = append(out, suggestions)
		}
		return nil, suggestMappingsOutput{Tables: out}, nil
	}
}

// resolvePii decides between what the column's name says and what its content showed. It is
// the rule of resolvePiiWith in the job's schema table (frontend SchemaTable.tsx), so that an
// agent and a person looking at the same column are told the same thing:
//   - a name that establishes the nature of the data wins, unless the content leaves the
//     format in doubt — "it is a birth date" does not tell whether it is dd/mm or mm/dd;
//   - otherwise the content decides;
//   - otherwise the column is not personal data, and only the name's category remains.
func resolvePii(column *mgmtv1alpha1.DatabaseColumn, content *mgmtv1alpha1.ColumnPiiDetection) columnSuggestion {
	if column.GetIsSensitive() {
		suggestion := columnSuggestion{
			Column:               column.GetColumn(),
			Sensitive:            true,
			Category:             column.GetDataCategory(),
			SuggestedTransformer: transformerLabel(column.GetSuggestedTransformerSource()),
			Confidence:           confidenceLabel(column.GetPiiConfidence()),
			Method:               methodLabel(column.GetPiiDetectionMethod()),
			Evidence:             column.GetPiiEvidence(),
		}
		if content.GetPiiConfidence() == mgmtv1alpha1.PiiConfidence_PII_CONFIDENCE_NEEDS_REVIEW &&
			content.GetPiiDetectionMethod() == mgmtv1alpha1.PiiDetectionMethod_PII_DETECTION_METHOD_FORMAT {
			suggestion.Confidence = confidenceLabel(content.GetPiiConfidence())
			suggestion.Method = methodLabel(content.GetPiiDetectionMethod())
			suggestion.Evidence = content.GetPiiEvidence()
		}
		return suggestion
	}
	if content != nil {
		return columnSuggestion{
			Column:               column.GetColumn(),
			Sensitive:            content.GetIsSensitive(),
			Category:             content.GetDataCategory(),
			SuggestedTransformer: transformerLabel(content.GetSuggestedTransformerSource()),
			Confidence:           confidenceLabel(content.GetPiiConfidence()),
			Method:               methodLabel(content.GetPiiDetectionMethod()),
			Evidence:             content.GetPiiEvidence(),
		}
	}
	return columnSuggestion{
		Column:               column.GetColumn(),
		Category:             column.GetDataCategory(),
		SuggestedTransformer: transformerLabel(column.GetSuggestedTransformerSource()),
	}
}

// keyColumns gives, for each column of a table, the keys it takes part in: its primary key, a
// foreign key towards another table, or the target of another table's foreign key — which need
// not be a primary key, a unique column will do.
func keyColumns(
	constraints *mgmtv1alpha1.GetConnectionTableConstraintsResponse,
	referencedBy []reference,
	table string,
) map[string][]string {
	roles := map[string]map[string]bool{}
	mark := func(column, role string) {
		if roles[column] == nil {
			roles[column] = map[string]bool{}
		}
		roles[column][role] = true
	}
	for _, column := range constraints.GetPrimaryKeyConstraints()[table].GetColumns() {
		mark(column, "primary")
	}
	for _, constraint := range constraints.GetForeignKeyConstraints()[table].GetConstraints() {
		for _, column := range constraint.GetColumns() {
			mark(column, "foreign")
		}
	}
	for _, ref := range referencedBy {
		for _, column := range ref.ReferencedColumns {
			mark(column, "referenced")
		}
	}
	keys := make(map[string][]string, len(roles))
	for column, set := range roles {
		keys[column] = slices.Sorted(maps.Keys(set))
	}
	return keys
}

func transformerLabel(source mgmtv1alpha1.TransformerSource) string {
	return enumLabel(source.String(), "TRANSFORMER_SOURCE_")
}

func confidenceLabel(confidence mgmtv1alpha1.PiiConfidence) string {
	return enumLabel(confidence.String(), "PII_CONFIDENCE_")
}

func methodLabel(method mgmtv1alpha1.PiiDetectionMethod) string {
	return enumLabel(method.String(), "PII_DETECTION_METHOD_")
}
