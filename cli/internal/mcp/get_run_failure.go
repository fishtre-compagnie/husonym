package mcp_server

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/fishtre-compagnie/husonym/cli/internal/mcp/rowvalues"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxFailureMessage bounds each message handed to the model: a failure can carry a whole batch.
const maxFailureMessage = 2000

type getRunFailureInput struct {
	RunId string `json:"run_id" jsonschema:"the run, as get_run_status gives it"`
}

type getRunFailureOutput struct {
	RunId      string            `json:"run_id"`
	Status     string            `json:"status"`
	Activities []activityFailure `json:"activities" jsonschema:"the pending activities that failed, and their last failure"`
	Tasks      []taskFailure     `json:"tasks"      jsonschema:"the failures the run recorded, table by table"`
}

type activityFailure struct {
	Activity string `json:"activity"`
	Message  string `json:"message"`
}

type taskFailure struct {
	Table      string `json:"table,omitempty"       jsonschema:"schema.table, when the failure is a table's"`
	Type       string `json:"type"`
	Message    string `json:"message"`
	RetryState string `json:"retry_state,omitempty"`
}

func addGetRunFailure(server *mcp.Server, values *rowvalues.Reader) {
	openWorld := false
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_run_failure",
		Description: "Say why a run failed, in the words of the databases and of the engine. A failure " +
			"message can quote the values it failed on, so the person is asked first, once per source " +
			"connection for the session, as for preview_column; nothing is read if they decline.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
	}, getRunFailure(values))
}

func getRunFailure(values *rowvalues.Reader) mcp.ToolHandlerFor[getRunFailureInput, getRunFailureOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		input getRunFailureInput,
	) (*mcp.CallToolResult, getRunFailureOutput, error) {
		failure, questions, err := values.Failure(ctx, req, input.RunId)
		switch {
		case errors.Is(err, rowvalues.ErrDeclined), errors.Is(err, rowvalues.ErrCannotAsk):
			return nil, getRunFailureOutput{}, err
		case err != nil:
			return nil, getRunFailureOutput{}, fmt.Errorf("unable to read why run %s failed: %w", input.RunId, err)
		case questions != nil:
			return &mcp.CallToolResult{InputRequests: questions}, getRunFailureOutput{}, nil
		}

		out := getRunFailureOutput{
			RunId:      failure.Run.GetId(),
			Status:     enumLabel(failure.Run.GetStatus().String(), "JOB_RUN_STATUS_"),
			Activities: []activityFailure{},
			Tasks:      []taskFailure{},
		}
		for _, activity := range failure.Run.GetPendingActivities() {
			if activity.LastFailure == nil {
				continue
			}
			out.Activities = append(out.Activities, activityFailure{
				Activity: activity.GetActivityName(),
				Message:  bounded(activity.GetLastFailure().GetMessage()),
			})
		}
		for _, event := range failure.Events {
			table := ""
			if sync := event.GetMetadata().GetSyncMetadata(); sync != nil {
				table = tableKey(sync.GetSchema(), sync.GetTable())
			}
			for _, task := range event.GetTasks() {
				if task.GetError() == nil {
					continue
				}
				out.Tasks = append(out.Tasks, taskFailure{
					Table:      table,
					Type:       task.GetType(),
					Message:    bounded(task.GetError().GetMessage()),
					RetryState: task.GetError().GetRetryState(),
				})
			}
		}
		return nil, out, nil
	}
}

// bounded cuts a message to maxFailureMessage bytes, on a rune boundary, and says so.
func bounded(message string) string {
	if len(message) <= maxFailureMessage {
		return message
	}
	cut := maxFailureMessage
	for cut > 0 && !utf8.RuneStart(message[cut]) {
		cut--
	}
	return message[:cut] + " […cut]"
}
