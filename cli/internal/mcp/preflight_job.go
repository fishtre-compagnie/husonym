package mcp_server

import (
	"context"
	"slices"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type preflightJobInput struct {
	JobId string `json:"job_id"`
}

type preflightJobOutput struct {
	JobId     string    `json:"job_id"`
	Engine    string    `json:"engine"     jsonschema:"the engine a run would be on, the deployment default resolved"`
	CheckedAt string    `json:"checked_at" jsonschema:"RFC 3339, in UTC"`
	Runnable  bool      `json:"runnable"   jsonschema:"false when a blocking finding stops any run of the job"`
	Findings  []finding `json:"findings"   jsonschema:"blocking first, then warnings, then information"`
}

func addPreflightJob(server *mcp.Server, jobReader *jobs.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "preflight_job",
		Description: "Tell what a run of a job would meet, were it started now, without running it: what its " +
			"connections cannot do that the job needs, what its engine cannot run, and what its mappings " +
			"would do to the destination. The plan of the run is computed by a worker, which logs in to the " +
			"connections; no row is read and nothing is written. A run stops on the same blocking findings. " +
			"Takes up to three minutes. " + namesAreData,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, preflightJob(jobReader))
}

func preflightJob(jobReader *jobs.Reader) mcp.ToolHandlerFor[preflightJobInput, preflightJobOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input preflightJobInput,
	) (*mcp.CallToolResult, preflightJobOutput, error) {
		res, err := jobReader.Preflight(ctx, input.JobId)
		if err != nil {
			return nil, preflightJobOutput{}, err
		}
		report := res.GetReport()
		out := preflightJobOutput{
			JobId:     input.JobId,
			Engine:    enumLabel(report.GetEngine().String(), "JOB_ENGINE_"),
			CheckedAt: res.GetCheckedAt().AsTime().UTC().Format(time.RFC3339),
			Findings:  make([]finding, 0, len(report.GetFindings())),
		}
		for _, f := range report.GetFindings() {
			out.Findings = append(out.Findings, preflightFinding(f))
		}
		out.Findings = byLevel(out.Findings)
		out.Runnable = !slices.ContainsFunc(out.Findings, func(f finding) bool { return f.Level == "blocking" })
		return nil, out, nil
	}
}
