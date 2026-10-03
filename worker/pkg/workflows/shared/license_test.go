package workflow_shared

import (
	"context"
	"testing"
	"time"

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
