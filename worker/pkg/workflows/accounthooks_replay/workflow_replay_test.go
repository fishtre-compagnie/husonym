package accounthooks_replay_test

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	accounthook_workflow "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/ee/account_hooks/workflow"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
)

// About the JSON histories replayed here, kept under testdata: they were recorded on a
// Temporal dev server started through the SDK testsuite, with the real workflow, started as
// the child of a job run, and stub activities. They are what the server keeps of a run:
// they must not be edited, by hand or by a tool.

// A run of the account hook workflow recorded earlier replays: the workflow asks for the
// same activities, in the same order, as the run did.
func Test_ProcessAccountHook_ReplaysARecordedRun(t *testing.T) {
	logger := log.NewStructuredLogger(slog.Default())

	for _, history := range []string{
		// The request carries no event: the run fails before any activity.
		"missing-event",
		// The lookup of the hooks of the event fails, and the run with it.
		"lookup-fails",
		// No hook listens to the event.
		"no-hook",
		"one-hook",
		// The only hook fails, and the run with it.
		"one-hook-fails",
		// The hooks are all started before the first one is awaited.
		"three-hooks",
		// The second hook fails once the two others have completed, and the run with it.
		"three-hooks-one-fails",
	} {
		t.Run(history, func(t *testing.T) {
			replayer := worker.NewWorkflowReplayer()
			replayer.RegisterWorkflow(accounthook_workflow.ProcessAccountHook)

			err := testutil.ReplayWorkflowHistoryFile(
				replayer, logger, filepath.Join("testdata", history+".json"),
			)
			require.NoError(t, err)
		})
	}
}
