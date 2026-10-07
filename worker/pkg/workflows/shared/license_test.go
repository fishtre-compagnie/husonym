package workflow_shared

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

// A run reads the license once, at its start, and holds that answer to its end: the license
// lapsing meanwhile does not reach it. That the answer also survives a replay is pinned by
// the replay tests of the workflows, on recorded histories.
func Test_LicenseIsValid_KeepsItsFirstAnswer(t *testing.T) {
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestWorkflowEnvironment()

	eelicense := testutil.NewFakeEELicense(testutil.WithIsValid())
	env.RegisterActivityWithOptions(
		func(ctx context.Context) error {
			eelicense.SetValid(false)
			return nil
		},
		activity.RegisterOptions{Name: "lapse-license"},
	)

	env.ExecuteWorkflow(func(ctx workflow.Context) (bool, error) {
		licensed := LicenseIsValid(ctx, eelicense)

		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute}),
			"lapse-license",
		).Get(ctx, nil)
		return licensed, err
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var licensed bool
	require.NoError(t, env.GetWorkflowResult(&licensed))
	assert.True(t, licensed, "the run keeps the answer it started with")
	assert.False(t, eelicense.IsValid(), "the license lapsed during the run")
}

// consulted is a license that counts how often it is asked anything.
type consulted struct {
	license.EEInterface
	asked atomic.Int32
}

func (c *consulted) IsValid() bool {
	c.asked.Add(1)
	return c.EEInterface.IsValid()
}

func (c *consulted) HasFeature(f license.Feature) bool {
	c.asked.Add(1)
	return c.EEInterface.HasFeature(f)
}

// readFeature runs a workflow that asks for the feature at its start, has the license changed
// by an activity, and returns the answer it started with.
func readFeature(
	t *testing.T,
	env *testsuite.TestWorkflowEnvironment,
	lic license.EEInterface,
	before bool,
	change func(),
) bool {
	t.Helper()
	env.RegisterActivityWithOptions(
		func(ctx context.Context) error {
			change()
			return nil
		},
		activity.RegisterOptions{Name: "change-license"},
	)

	env.ExecuteWorkflow(func(ctx workflow.Context) (bool, error) {
		allowed := LicenseAllows(ctx, lic, license.FeatureAccountHooks, before)

		err := workflow.ExecuteActivity(
			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute}),
			"change-license",
		).Get(ctx, nil)
		return allowed, err
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var allowed bool
	require.NoError(t, env.GetWorkflowResult(&allowed))
	return allowed
}

// A run asks the license for a feature once, at its start, and holds that answer to its end:
// the answer is the license's, not the one the code acted on before the feature was asked,
// and a license that changes meanwhile does not reach the run. That the answer also survives
// a replay is pinned by the replay tests of the workflows, on recorded histories.
func Test_LicenseAllows_RecordsItsAnswer(t *testing.T) {
	t.Run("a feature the license includes", func(t *testing.T) {
		var ts testsuite.WorkflowTestSuite
		eelicense := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureAccountHooks))

		allowed := readFeature(t, ts.NewTestWorkflowEnvironment(), eelicense, false, func() { eelicense.SetFeatures() })

		assert.True(t, allowed, "the run keeps the answer it started with")
		assert.False(t, eelicense.HasFeature(license.FeatureAccountHooks), "the license lost the feature during the run")
	})

	t.Run("a feature the license lacks", func(t *testing.T) {
		var ts testsuite.WorkflowTestSuite
		eelicense := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureJobHooks))

		allowed := readFeature(t, ts.NewTestWorkflowEnvironment(), eelicense, true, eelicense.ClearFeatures)

		assert.False(t, allowed, "the run keeps the answer it started with")
		assert.True(t, eelicense.HasFeature(license.FeatureAccountHooks), "the license gained the feature during the run")
	})
}

// A run started before the feature was asked replays as it ran: it is given the answer the
// code acted on then, and the license is not consulted.
func Test_LicenseAllows_EarlierRunsKeepWhatTheyActedOn(t *testing.T) {
	for _, tt := range []struct {
		name    string
		before  bool
		license *testutil.FakeEELicense
	}{
		{
			name: "it acted as allowed, the license lacks the feature", before: true,
			license: testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures()),
		},
		{
			name: "it acted as refused, the license includes the feature", before: false,
			license: testutil.NewFakeEELicense(testutil.WithIsValid()),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var ts testsuite.WorkflowTestSuite
			env := ts.NewTestWorkflowEnvironment()
			env.OnGetVersion(licenseFeatureReadChangeId, workflow.DefaultVersion, 1).Return(workflow.DefaultVersion)
			watched := &consulted{EEInterface: tt.license}

			allowed := readFeature(t, env, watched, tt.before, func() {})

			assert.Equal(t, tt.before, allowed)
			assert.Zero(t, watched.asked.Load(), "the license is not consulted")
		})
	}
}

// The ids of the changes are written in the histories of the runs that met them: they stay.
func Test_LicenseChangeIds(t *testing.T) {
	require.Equal(t, "license-read-recorded", licenseReadChangeId)
	require.Equal(t, "license-feature-read-recorded", licenseFeatureReadChangeId)
}
