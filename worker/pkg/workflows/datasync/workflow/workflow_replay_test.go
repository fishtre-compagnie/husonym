package datasync_workflow

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/worker"
)

// The history was recorded under a valid license, its answer kept in the run. Replayed once
// the license lapsed, the run follows the answer it recorded: the hooks of its start and of
// its end are those of the history.
func Test_Datasync_ReplaysTheLicenseAnswerOfTheRun(t *testing.T) {
	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(New(testutil.NewFakeEELicense()).Workflow)

	err := testutil.ReplayWorkflowHistoryFile(
		replayer, getTestLogger(), "../../shared/testdata/datasync-after.json",
	)
	require.NoError(t, err)
}

// The histories were recorded before the license was read through a side effect: a run in
// flight at the upgrade replays on the path it started with.
func Test_Datasync_ReplaysAHistoryRecordedBefore(t *testing.T) {
	t.Run("licensed", func(t *testing.T) {
		replayer := worker.NewWorkflowReplayer()
		replayer.RegisterWorkflow(New(testutil.NewFakeEELicense(testutil.WithIsValid())).Workflow)

		err := testutil.ReplayWorkflowHistoryFile(
			replayer, getTestLogger(), "../../shared/testdata/datasync-before.json",
		)
		require.NoError(t, err)
	})

	t.Run("unlicensed", func(t *testing.T) {
		replayer := worker.NewWorkflowReplayer()
		replayer.RegisterWorkflow(New(testutil.NewFakeEELicense()).Workflow)

		err := testutil.ReplayWorkflowHistoryFile(
			replayer, getTestLogger(), "../../shared/testdata/datasync-before-unlicensed.json",
		)
		require.NoError(t, err)
	})
}
