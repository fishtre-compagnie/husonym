package piidetect_replay_test

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/piidetect"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
)

// About the JSON histories replayed here, kept under testdata: they were recorded on a
// Temporal dev server started through the SDK testsuite, with the real workflows and stub
// activities. The runs of a table are the children of a run of the job. They are what the
// server keeps of a run: they must not be edited, by hand or by a tool.
//
// The two "piidetect-before" ones were recorded at the commit that precedes the
// introduction of the "license-read-recorded" change, and guard the runs started before
// it. They can be retired once no run started before that change can still be open.

// tablesAtOnceKey is the setting that says how many tables a run of the job scans at once.
const tablesAtOnceKey = "TABLE_PII_DETECT_MAX_CONCURRENCY"

// A run of the job recorded earlier replays: the workflow asks for the same activities and
// the same children, in the same order, as the run did, and a run that completed ends on
// the same result.
func Test_JobPiiDetect_ReplaysARecordedRun(t *testing.T) {
	logger := log.NewStructuredLogger(slog.Default())

	for _, history := range []struct {
		name string
		// licensed is what the license answers during the replay. A run that recorded the
		// answer it started with follows it: those are replayed under the opposite answer.
		licensed bool
		// tablesAtOnce is the setting the run was recorded under, when it is not the default.
		tablesAtOnce int
	}{
		// Recorded before the license was read through a side effect: the run reads it again.
		{name: "piidetect-before", licensed: true},
		{name: "piidetect-before-unlicensed", licensed: false},
		// Recorded under a valid license, its answer kept in the run.
		{name: "piidetect-after", licensed: false},
		// Recorded without a valid license: the run fails before any activity.
		{name: "job-unlicensed", licensed: true},
		// The details of the job cannot be read: the run fails, no event is announced.
		{name: "job-details-fail", licensed: false},
		// Nothing to scan: an empty report is saved.
		{name: "job-no-table", licensed: false},
		// A table filter, data sampling and a user prompt are set.
		{name: "job-one-table", licensed: false},
		// No detection settings at all, as many tables as are scanned at once.
		{name: "job-three-tables", licensed: false},
		// More tables than are scanned at once: a table starts when another one ends.
		{name: "job-seven-tables", licensed: false},
		// Four of the seven tables fail: the run goes on and reports the three others.
		{name: "job-seven-tables-four-fail", licensed: false},
		// No table could be scanned: the run still completes, on an empty report.
		{name: "job-every-table-fails", licensed: false},
		// Incremental, and no earlier run to start from.
		{name: "job-incremental-first-run", licensed: false},
		// Incremental: the report of the earlier run is completed by the tables scanned again.
		{name: "job-incremental", licensed: false},
		// Incremental, and the earlier run cannot be looked up: the run fails.
		{name: "job-incremental-lookup-fails", licensed: false},
		// The tables cannot be listed: the run fails.
		{name: "job-table-listing-fails", licensed: false},
		// The report cannot be saved: the run fails.
		{name: "job-report-save-fails", licensed: false},
		// One table at a time, the second of the four fails.
		{name: "job-four-tables-one-at-a-time", licensed: false, tablesAtOnce: 1},
		// Cancelled while three tables are scanned: they are cancelled, the fourth never starts.
		{name: "job-cancelled", licensed: false},
	} {
		t.Run(history.name, func(t *testing.T) {
			if history.tablesAtOnce != 0 {
				viper.Set(tablesAtOnceKey, history.tablesAtOnce)
				t.Cleanup(func() { viper.Set(tablesAtOnceKey, 0) })
			}
			eelicense := testutil.NewFakeEELicense()
			eelicense.SetValid(history.licensed)

			replay := func() error {
				replayer := worker.NewWorkflowReplayer()
				replayer.RegisterWorkflow(piidetect.NewJobWorkflow(eelicense, viper.GetInt(tablesAtOnceKey)).JobPiiDetect)
				return testutil.ReplayWorkflowHistoryFileToItsResult(
					replayer, logger, filepath.Join("testdata", history.name+".json"),
				)
			}
			require.NoError(t, replay())

			// Every history kept here was recorded before the run asked the license for its
			// features: it replays on what it acted on then, the validity of the license,
			// under a license that includes no feature at all.
			eelicense.SetFeatures()
			require.NoError(t, replay(), "under a license that includes no feature")
		})
	}
}

// A run of a table recorded earlier replays: the workflow asks for the same activities, in
// the same order, as the run did, and a run that completed ends on the same result.
func Test_TablePiiDetect_ReplaysARecordedRun(t *testing.T) {
	logger := log.NewStructuredLogger(slog.Default())

	for _, history := range []string{
		// The table has no column: its report is empty.
		"table-no-column",
		// The table has columns, none of them is found to hold personal data.
		"table-nothing-found",
		// One column, found by its name only.
		"table-regex-only",
		// One column, found by the model only.
		"table-llm-only",
		// Six columns: found by both, by one, by the other, by none.
		"table-several-columns",
		// A table that an earlier run had already scanned.
		"table-scanned-again",
		// The model answers on its second attempt.
		"table-llm-second-attempt",
		// The columns cannot be read: the run fails before any detection.
		"table-column-read-fails",
		// The detection by name fails, and the run with it: the model is not asked.
		"table-regex-fails",
		// The model fails, and the run with it: no report is saved.
		"table-llm-fails",
		// The report cannot be saved: the run fails.
		"table-report-save-fails",
		// Cancelled while the model is asked.
		"table-cancelled",
	} {
		t.Run(history, func(t *testing.T) {
			replayer := worker.NewWorkflowReplayer()
			replayer.RegisterWorkflow(piidetect.TablePiiDetect)

			err := testutil.ReplayWorkflowHistoryFileToItsResult(
				replayer, logger, filepath.Join("testdata", history+".json"),
			)
			require.NoError(t, err)
		})
	}
}
