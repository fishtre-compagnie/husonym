package mcp_server

import (
	"context"
	"errors"
	"fmt"

	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type runJobInput struct {
	JobId string `json:"job_id" jsonschema:"the one job to run"`
}

type runJobOutput struct {
	JobId string `json:"job_id"`
	Next  string `json:"next"`
}

func addRunJob(server *mcp.Server, jobReader *jobs.Reader) {
	destructive := true
	mcp.AddTool(server, &mcp.Tool{
		Name: "run_job",
		Description: "Run one job now: it reads its source and writes into its destinations. The person is " +
			"asked first, each time, and shown what the run reads and writes; nothing runs if they " +
			"decline. Refused while a run of the job is in progress.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &destructive},
	}, runJob(jobReader))
}

func runJob(jobReader *jobs.Reader) mcp.ToolHandlerFor[runJobInput, runJobOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		input runJobInput,
	) (*mcp.CallToolResult, runJobOutput, error) {
		questions, err := jobReader.Run(ctx, req, input.JobId)
		switch {
		case errors.Is(err, jobs.ErrDeclined), errors.Is(err, jobs.ErrCannotAsk):
			return nil, runJobOutput{}, err
		case err != nil:
			return nil, runJobOutput{}, fmt.Errorf("unable to run job %s: %w", input.JobId, err)
		case questions != nil:
			return &mcp.CallToolResult{InputRequests: questions}, runJobOutput{}, nil
		}
		return nil, runJobOutput{
			JobId: input.JobId,
			// The API does not say which run it started: it is the next one to show.
			Next: "the run is starting: get_run_status with this job_id follows it",
		}, nil
	}
}
