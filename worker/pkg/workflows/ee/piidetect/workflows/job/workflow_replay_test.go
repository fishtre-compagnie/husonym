package piidetect_job_workflow

import (
	"log/slog"
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
)

// The history was recorded under a valid license, its answer kept in the run. Replayed once
// the license lapsed, the run follows the answer it recorded instead of refusing to run.
func Test_PiiDetect_ReplaysTheLicenseAnswerOfTheRun(t *testing.T) {
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(New(testutil.NewFakeEELicense()).JobPiiDetect)

	err := testutil.ReplayWorkflowHistoryFile(
		replayer, log.NewStructuredLogger(slog.Default()), "testdata/piidetect-after.json",
	)
	require.NoError(t, err)
}

// The histories were recorded before the license was read through a side effect: a run in
// flight at the upgrade replays on the path it started with.
func Test_PiiDetect_ReplaysAHistoryRecordedBefore(t *testing.T) {
	logger := log.NewStructuredLogger(slog.Default())

	t.Run("licensed", func(t *testing.T) {
		replayer := worker.NewWorkflowReplayer()
		replayer.RegisterWorkflow(New(testutil.NewFakeEELicense(testutil.WithIsValid())).JobPiiDetect)

		err := testutil.ReplayWorkflowHistoryFile(replayer, logger, "testdata/piidetect-before.json")
		require.NoError(t, err)
	})

	t.Run("unlicensed", func(t *testing.T) {
		replayer := worker.NewWorkflowReplayer()
		replayer.RegisterWorkflow(New(testutil.NewFakeEELicense()).JobPiiDetect)

		err := testutil.ReplayWorkflowHistoryFile(replayer, logger, "testdata/piidetect-before-unlicensed.json")
		require.NoError(t, err)
	})
}
