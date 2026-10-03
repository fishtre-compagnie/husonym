package testutil

import (
	"fmt"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ReplayWorkflowHistoryFile replays a history exported as JSON under the workflow id and the
// run id it was recorded with. The replayer names the execution itself otherwise, and the
// workflows derive the ids of their children from their own: the replay would not match.
func ReplayWorkflowHistoryFile(replayer worker.WorkflowReplayer, logger log.Logger, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	history, err := client.HistoryFromJSON(file, client.HistoryJSONOptions{})
	if err != nil {
		return fmt.Errorf("unable to read the history of %s: %w", path, err)
	}
	if len(history.GetEvents()) == 0 {
		return fmt.Errorf("the history of %s is empty", path)
	}
	started := history.GetEvents()[0].GetWorkflowExecutionStartedEventAttributes()
	if started == nil {
		return fmt.Errorf("the history of %s does not start with the start of a workflow", path)
	}
	return replayer.ReplayWorkflowHistoryWithOptions(logger, history, worker.ReplayWorkflowHistoryOptions{
		OriginalExecution: workflow.Execution{
			ID:    started.GetWorkflowId(),
			RunID: started.GetOriginalExecutionRunId(),
		},
	})
}
