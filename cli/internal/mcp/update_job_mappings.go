package mcp_server

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/novalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type updateJobMappingsInput struct {
	JobId    string         `json:"job_id"`
	Mappings []mappingInput `json:"mappings" jsonschema:"the columns to map, or map anew; the other mappings of the job stay as they are. A table the job did not read yet takes a mapping for each of its columns"`
}

type updateJobMappingsOutput struct {
	JobId   string `json:"job_id"`
	Changed int    `json:"changed" jsonschema:"how many columns were mapped anew"`
	Added   int    `json:"added"   jsonschema:"how many columns the job did not map before"`
	Columns int    `json:"columns" jsonschema:"how many columns the job maps now"`
}

func addUpdateJobMappings(server *mcp.Server, data *novalues.Reader, jobReader *jobs.Reader) {
	openWorld, destructive := false, true
	mcp.AddTool(server, &mcp.Tool{
		Name: "update_job_mappings",
		Description: "Map columns of a job anew, or map columns it did not read yet; the other mappings stay. " +
			"When the job runs on a schedule, the change takes effect at its next run, so the person is " +
			"asked first.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &openWorld},
	}, updateJobMappings(data, jobReader))
}

func updateJobMappings(
	data *novalues.Reader,
	jobReader *jobs.Reader,
) mcp.ToolHandlerFor[updateJobMappingsInput, updateJobMappingsOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		input updateJobMappingsInput,
	) (*mcp.CallToolResult, updateJobMappingsOutput, error) {
		job, err := jobReader.Get(ctx, input.JobId)
		if err != nil {
			return nil, updateJobMappingsOutput{}, fmt.Errorf("unable to read job %s: %w", input.JobId, err)
		}
		sourceId := jobs.SourceConnectionId(job)
		if sourceId == "" {
			return nil, updateJobMappingsOutput{}, fmt.Errorf(
				"the job %q reads from neither PostgreSQL nor MySQL: its mappings are not changed here", job.GetName(),
			)
		}
		columns, err := data.Columns(ctx, sourceId)
		if err != nil {
			return nil, updateJobMappingsOutput{}, fmt.Errorf("unable to read the schema of connection %s: %w", sourceId, err)
		}
		changes, err := buildMappings(ctx, data, columns, input.Mappings)
		if err != nil {
			return nil, updateJobMappingsOutput{}, err
		}

		merged := slices.Clone(job.GetMappings())
		out := updateJobMappingsOutput{JobId: job.GetId()}
		for _, change := range changes {
			i := slices.IndexFunc(merged, func(m *mgmtv1alpha1.JobMapping) bool { return keyOf(m) == keyOf(change) })
			if i < 0 {
				merged = append(merged, change)
				out.Added++
				continue
			}
			merged[i] = change
			out.Changed++
		}
		// Only the tables this change touches are held to having each column mapped: a table
		// the job already reads with a column left out is someone else's decision.
		touched := mappedTables(changes)
		if err := checkComplete(slices.DeleteFunc(slices.Clone(merged), func(m *mgmtv1alpha1.JobMapping) bool {
			return !slices.Contains(touched, tableKey(m.GetSchema(), m.GetTable()))
		}), columns); err != nil {
			return nil, updateJobMappingsOutput{}, err
		}
		if err := checkMappings(
			ctx, data, sourceId, job.GetSource(), merged, job.GetVirtualForeignKeys(), touched,
		); err != nil {
			return nil, updateJobMappingsOutput{}, err
		}

		questions, err := jobReader.SetMappings(ctx, req, job, merged)
		switch {
		case errors.Is(err, jobs.ErrDeclined), errors.Is(err, jobs.ErrCannotAsk):
			return nil, updateJobMappingsOutput{}, err
		case err != nil:
			return nil, updateJobMappingsOutput{}, fmt.Errorf("unable to change the mappings of job %s: %w", job.GetId(), err)
		case questions != nil:
			return &mcp.CallToolResult{InputRequests: questions}, updateJobMappingsOutput{}, nil
		}
		out.Columns = len(merged)
		return nil, out, nil
	}
}
