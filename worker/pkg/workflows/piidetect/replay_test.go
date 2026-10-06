package piidetect

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
)

// About the JSON histories replayed here, kept under testdata: they were recorded on a
// Temporal dev server started through the SDK testsuite, with the workflows of this
// package and stub activities. Each took one of the branches that a change id guards, or
// recorded how many tables it scans at once. They are what the server keeps of a run:
// they must not be edited, by hand or by a tool. Test_RecordsHistories records those it
// knows of, on request, and leaves a history that is already there as it is.
//
// The histories of the runs that took none of these branches are replayed by the package
// piidetect_replay.

// A run that recorded a version replays on the branch of that version: the workflow asks
// for the same activities and the same children, in the same order, as the run did, and
// a run that completed ends on the same result.
func Test_ReplaysARunThatRecordedAVersion(t *testing.T) {
	logger := log.NewStructuredLogger(slog.Default())

	for _, history := range []struct {
		name string
		// tablesAtOnce is what the worker that replays is set to.
		tablesAtOnce int
	}{
		// The model of a table could not be asked: the report of the rules is saved, and
		// the run of the table completes.
		{name: "table-model-fails-tolerated"},
		// One table failed and another was scanned without the model: the index is saved,
		// then the run fails.
		{name: "job-table-fails-run-fails", tablesAtOnce: 3},
		// Two tables whose names give the same id: the second child has a suffix.
		{name: "job-two-tables-same-child-id", tablesAtOnce: 3},
		// The run recorded that it scans one table at a time, on a worker set to three:
		// it replays one at a time on a worker set to anything else.
		{name: "job-recorded-one-table-at-once", tablesAtOnce: 5},
		// A table with a free-text column the rules found nothing in: its content is
		// analyzed between the model and the save, and the finding is in the report.
		{name: "table-content-analyzed"},
		// Three tables scanned one at a time, by an API without analyzer: the first
		// learns it, the two others are started knowing it, and the run completes.
		{name: "job-analyzer-absent", tablesAtOnce: 3},
	} {
		t.Run(history.name, func(t *testing.T) {
			// The runs recorded the answer of the license they started with: they follow
			// it under the opposite answer.
			license := testutil.NewFakeEELicense()
			replayer := worker.NewWorkflowReplayer()
			replayer.RegisterWorkflow(NewJobWorkflow(license, history.tablesAtOnce).JobPiiDetect)
			replayer.RegisterWorkflow(TablePiiDetect)

			err := testutil.ReplayWorkflowHistoryFileToItsResult(
				replayer, logger, filepath.Join("testdata", history.name+".json"),
			)
			require.NoError(t, err)
		})
	}
}
