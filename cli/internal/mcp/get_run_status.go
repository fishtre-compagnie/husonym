package mcp_server

import (
	"context"
	"fmt"
	"time"

	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/jobs"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runsShown bounds how many past runs of a job a call lists.
const runsShown = 5

type getRunStatusInput struct {
	JobId string `json:"job_id"`
}

type getRunStatusOutput struct {
	Starting bool         `json:"starting,omitempty" jsonschema:"a run was triggered with run_job and has not shown yet: call again shortly"`
	Runs     []runSummary `json:"runs"               jsonschema:"the latest runs of the job, most recent first"`
	Latest   *runDetail   `json:"latest,omitempty"   jsonschema:"what the most recent run is doing"`
}

type runSummary struct {
	RunId       string `json:"run_id"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"             jsonschema:"RFC 3339, in UTC"`
	CompletedAt string `json:"completed_at,omitempty" jsonschema:"RFC 3339, in UTC"`
}

type runDetail struct {
	RunId         string            `json:"run_id"`
	FailingTables []string          `json:"failing_tables,omitempty" jsonschema:"the tables whose sync recorded an error, as schema.table; get_run_failure says why"`
	Activities    []runActivityInfo `json:"activities,omitempty"     jsonschema:"the activities pending, with those that failed and are retried"`
}

type runActivityInfo struct {
	Activity string `json:"activity"`
	Status   string `json:"status"`
	Failing  bool   `json:"failing,omitempty" jsonschema:"it failed at least once; get_run_failure says why"`
}

func addGetRunStatus(server *mcp.Server, jobReader *jobs.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_run_status",
		Description: "Say where the runs of a job stand: the latest runs and their status, and what the " +
			"most recent one is doing. No failure message: get_run_failure gives them.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, getRunStatus(jobReader))
}

func getRunStatus(jobReader *jobs.Reader) mcp.ToolHandlerFor[getRunStatusInput, getRunStatusOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input getRunStatusInput,
	) (*mcp.CallToolResult, getRunStatusOutput, error) {
		runs, err := jobReader.Runs(ctx, input.JobId)
		if err != nil {
			return nil, getRunStatusOutput{}, err
		}
		out := getRunStatusOutput{Starting: jobReader.Starting(input.JobId, runs), Runs: []runSummary{}}
		for _, run := range runs[:min(len(runs), runsShown)] {
			summary := runSummary{
				RunId:     run.GetId(),
				Status:    enumLabel(run.GetStatus().String(), "JOB_RUN_STATUS_"),
				StartedAt: run.GetStartedAt().AsTime().UTC().Format(time.RFC3339),
			}
			if run.CompletedAt != nil {
				summary.CompletedAt = run.GetCompletedAt().AsTime().UTC().Format(time.RFC3339)
			}
			out.Runs = append(out.Runs, summary)
		}
		if len(runs) == 0 {
			return nil, out, nil
		}

		// Pending activities come only with a run read on its own.
		latest, err := jobReader.GetRun(ctx, runs[0].GetId())
		if err != nil {
			return nil, getRunStatusOutput{}, fmt.Errorf("unable to read run %s: %w", runs[0].GetId(), err)
		}
		events, err := jobReader.Events(ctx, latest.GetId())
		if err != nil {
			return nil, getRunStatusOutput{}, err
		}
		out.Latest = &runDetail{RunId: latest.GetId(), FailingTables: jobs.FailingTables(events)}
		for _, activity := range latest.GetPendingActivities() {
			out.Latest.Activities = append(out.Latest.Activities, runActivityInfo{
				Activity: activity.GetActivityName(),
				Status:   enumLabel(activity.GetStatus().String(), "ACTIVITY_STATUS_"),
				Failing:  activity.LastFailure != nil,
			})
		}
		return nil, out, nil
	}
}
