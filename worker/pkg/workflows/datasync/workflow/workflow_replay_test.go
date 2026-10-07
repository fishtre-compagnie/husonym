package datasync_workflow

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/worker"
)

// About the JSON histories replayed here, kept under worker/pkg/workflows/shared/testdata:
// they were recorded on a Temporal dev server started through the SDK testsuite, with the
// real workflows and stub activities. The "-before" ones were recorded at the commit that
// precedes the introduction of the "license-read-recorded" change, and guard the runs
// started before it. They can be retired once no run started before that change can still
// be open.

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

// Every history kept here was recorded before the account status check named the job. Naming
// it changes what the activity is handed, not which commands the workflow issues nor in what
// order: a run in flight at the upgrade replays as it was recorded.
func Test_Datasync_ReplaysRunsRecordedBeforeTheJobId(t *testing.T) {
	for _, recorded := range []struct {
		history string
		license *testutil.FakeEELicense
	}{
		{"datasync-before.json", testutil.NewFakeEELicense(testutil.WithIsValid())},
		{"datasync-before-unlicensed.json", testutil.NewFakeEELicense()},
		{"datasync-after.json", testutil.NewFakeEELicense()},
	} {
		t.Run(recorded.history, func(t *testing.T) {
			replayer := worker.NewWorkflowReplayer()
			replayer.RegisterWorkflow(New(recorded.license).Workflow)

			err := testutil.ReplayWorkflowHistoryFile(
				replayer, getTestLogger(), "../../shared/testdata/"+recorded.history,
			)
			require.NoError(t, err)
		})
	}
}

// Every history kept here was recorded before the run asked the license for the feature of
// its account hooks: each replays on what it acted on then, the validity of the license,
// under a license that includes no feature at all.
func Test_Datasync_ReplaysRunsRecordedBeforeTheFeatureWasAsked(t *testing.T) {
	for _, recorded := range []struct {
		history string
		license *testutil.FakeEELicense
	}{
		{"datasync-before.json", testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures())},
		{"datasync-before-unlicensed.json", testutil.NewFakeEELicense(testutil.WithFeatures())},
		{"datasync-after.json", testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures())},
	} {
		t.Run(recorded.history, func(t *testing.T) {
			replayer := worker.NewWorkflowReplayer()
			replayer.RegisterWorkflow(New(recorded.license).Workflow)

			err := testutil.ReplayWorkflowHistoryFile(
				replayer, getTestLogger(), "../../shared/testdata/"+recorded.history,
			)
			require.NoError(t, err)
		})
	}
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
