package testutil

import (
	"errors"
	"fmt"
	"os"
	"reflect"

	commonpb "go.temporal.io/api/common/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// ReplayWorkflowHistoryFile replays a history exported as JSON under the workflow id and the
// run id it was recorded with. The replayer names the execution itself otherwise, and the
// workflows derive the ids of their children from their own: the replay would not match.
func ReplayWorkflowHistoryFile(replayer worker.WorkflowReplayer, logger log.Logger, path string) error {
	history, err := readWorkflowHistoryFile(path)
	if err != nil {
		return err
	}
	return replayWorkflowHistory(replayer, logger, history)
}

// ReplayWorkflowHistoryFileToItsResult replays a history as ReplayWorkflowHistoryFile does
// and, when the recorded run completed, holds the replay to the result the run recorded.
// The replayer alone checks that the replay completes too, not what it completes on. The
// results are compared once decoded: they are expected to be encoded as JSON.
func ReplayWorkflowHistoryFileToItsResult(replayer worker.WorkflowReplayer, logger log.Logger, path string) error {
	history, err := readWorkflowHistoryFile(path)
	if err != nil {
		return err
	}
	if err := replayWorkflowHistory(replayer, logger, history); err != nil {
		return err
	}

	events := history.GetEvents()
	completed := events[len(events)-1].GetWorkflowExecutionCompletedEventAttributes()
	if completed == nil {
		return nil
	}
	results, ok := replayer.(interface {
		GetWorkflowResult(workflowID string, valuePtr any) error
	})
	if !ok {
		return errors.New("the replayer does not give the result of the replay")
	}
	var replayed any
	workflowId := events[0].GetWorkflowExecutionStartedEventAttributes().GetWorkflowId()
	if err := results.GetWorkflowResult(workflowId, &replayed); err != nil {
		return fmt.Errorf("unable to read the result of the replay of %s: %w", path, err)
	}
	recorded, err := decodeWorkflowResult(completed.GetResult())
	if err != nil {
		return fmt.Errorf("unable to read the result recorded in %s: %w", path, err)
	}
	if !reflect.DeepEqual(recorded, replayed) {
		return fmt.Errorf(
			"the replay of %s does not end on the result the run recorded: recorded %v, replayed %v",
			path, recorded, replayed,
		)
	}
	return nil
}

func decodeWorkflowResult(payloads *commonpb.Payloads) (any, error) {
	var result any
	if err := converter.GetDefaultDataConverter().FromPayloads(payloads, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func readWorkflowHistoryFile(path string) (*historypb.History, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	history, err := client.HistoryFromJSON(file, client.HistoryJSONOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to read the history of %s: %w", path, err)
	}
	if len(history.GetEvents()) == 0 {
		return nil, fmt.Errorf("the history of %s is empty", path)
	}
	if history.GetEvents()[0].GetWorkflowExecutionStartedEventAttributes() == nil {
		return nil, fmt.Errorf("the history of %s does not start with the start of a workflow", path)
	}
	return history, nil
}

func replayWorkflowHistory(replayer worker.WorkflowReplayer, logger log.Logger, history *historypb.History) error {
	started := history.GetEvents()[0].GetWorkflowExecutionStartedEventAttributes()
	return replayer.ReplayWorkflowHistoryWithOptions(logger, history, worker.ReplayWorkflowHistoryOptions{
		OriginalExecution: workflow.Execution{
			ID:    started.GetWorkflowId(),
			RunID: started.GetOriginalExecutionRunId(),
		},
	})
}
