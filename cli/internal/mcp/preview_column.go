package mcp_server

import (
	"context"
	"errors"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/rowvalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type previewColumnInput struct {
	ConnectionId string `json:"connection_id"   jsonschema:"the id of the source connection, as list_connections gives it"`
	Table        string `json:"table"           jsonschema:"schema.table"`
	Column       string `json:"column"`
	Transformer  string `json:"transformer"     jsonschema:"a system transformer, as suggest_mappings names it, such as generate_email; tried with its default configuration"`
	Limit        uint32 `json:"limit,omitempty" jsonschema:"how many rows to read: 20 by default, 50 at most"`
}

type previewColumnOutput struct {
	Values          []previewValue `json:"values"`
	DistinctInputs  uint32         `json:"distinct_inputs"`
	DistinctOutputs uint32         `json:"distinct_outputs" jsonschema:"fewer than distinct_inputs means the transformer sends different values to the same one, which a unique column cannot take; a sign on a sample, never a proof"`
}

type previewValue struct {
	Input  *string `json:"input"           jsonschema:"the value read; null for NULL"`
	Output *string `json:"output"          jsonschema:"what the transformer made of it; null for NULL, or when it failed"`
	Error  string  `json:"error,omitempty" jsonschema:"why the transformer failed on this value"`
}

func addPreviewColumn(server *mcp.Server, data *novalues.Reader, values *rowvalues.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "preview_column",
		Description: "Show what a transformer makes of real values of a column, and whether it collapses " +
			"distinct values together. This sends real production values to the model: the person is " +
			"asked first, once per connection for the session, and nothing is read if they decline. " +
			"Prefer suggest_mappings and introspect_schema, which read no value, when they are enough.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, previewColumn(data, values))
}

func previewColumn(
	data *novalues.Reader,
	values *rowvalues.Reader,
) mcp.ToolHandlerFor[previewColumnInput, previewColumnOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		input previewColumnInput,
	) (*mcp.CallToolResult, previewColumnOutput, error) {
		source, ok := transformerSource(input.Transformer)
		if !ok {
			return nil, previewColumnOutput{}, fmt.Errorf(
				"no system transformer %q: name one as suggest_mappings does, such as generate_email",
				input.Transformer,
			)
		}
		column, err := findColumn(ctx, data, input.ConnectionId, input.Table, input.Column)
		if err != nil {
			return nil, previewColumnOutput{}, err
		}
		transformer, err := data.DefaultTransformer(ctx, source)
		if err != nil {
			if errors.Is(err, novalues.ErrRunsCode) {
				return nil, previewColumnOutput{}, fmt.Errorf("%s %w", input.Transformer, err)
			}
			return nil, previewColumnOutput{}, fmt.Errorf("unable to find the transformer %s: %w", input.Transformer, err)
		}

		preview, questions, err := values.Preview(ctx, req, &rowvalues.Column{
			ConnectionId: input.ConnectionId,
			Schema:       column.GetSchema(),
			Table:        column.GetTable(),
			Column:       column.GetColumn(),
		}, transformer, input.Limit)
		switch {
		case errors.Is(err, rowvalues.ErrDeclined), errors.Is(err, rowvalues.ErrCannotAsk):
			return nil, previewColumnOutput{}, err
		case err != nil:
			return nil, previewColumnOutput{}, fmt.Errorf(
				"unable to preview %s on %s.%s: %w", input.Transformer, input.Table, input.Column, err,
			)
		case questions != nil:
			return &mcp.CallToolResult{InputRequests: questions}, previewColumnOutput{}, nil
		}

		out := previewColumnOutput{
			Values:          make([]previewValue, 0, len(preview.GetValues())),
			DistinctInputs:  preview.GetDistinctInputs(),
			DistinctOutputs: preview.GetDistinctOutputs(),
		}
		for _, value := range preview.GetValues() {
			out.Values = append(out.Values, previewValue{
				Input:  sampleText(value.GetInput()),
				Output: sampleText(value.GetOutput()),
				Error:  value.GetError(),
			})
		}
		return nil, out, nil
	}
}

// findColumn checks that the column exists, and splits its table into schema and table the
// way the API knows them — a name may hold a dot of its own.
func findColumn(
	ctx context.Context,
	data *novalues.Reader,
	connectionId, table, column string,
) (*mgmtv1alpha1.DatabaseColumn, error) {
	columns, err := data.Columns(ctx, connectionId)
	if err != nil {
		return nil, fmt.Errorf("unable to read the schema of connection %s: %w", connectionId, err)
	}
	for _, candidate := range columns {
		if tableKey(candidate.GetSchema(), candidate.GetTable()) == table && candidate.GetColumn() == column {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf(
		"no column %s in %s on this connection: introspect_schema gives the columns of a table",
		column, table,
	)
}

// sampleText gives a sampled value as text, and NULL as nothing.
func sampleText(value *mgmtv1alpha1.ColumnSampleValue) *string {
	if value == nil || value.GetIsNull() {
		return nil
	}
	text := value.GetValue()
	return &text
}
