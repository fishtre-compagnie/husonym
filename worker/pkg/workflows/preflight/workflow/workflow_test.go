package preflight_workflow

import (
	"context"
	"errors"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

var planned = &preflight.Finding{
	Kind:    mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM,
	Level:   preflight.Information,
	Table:   "public.journal",
	Message: "public.journal is read in one stream",
}

// The plan and what the connections tell make the report, blocking findings included:
// the check reports them, it does not fail on them.
func Test_JobPreflight(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	tables := []*preflight_activity.TableColumns{{Schema: "public", Table: "journal", Columns: []string{"message"}}}
	mappings := []*mgmtv1alpha1.JobMapping{{Schema: "public", Table: "journal", Column: "message"}}

	var generate *genbenthosconfigs_activity.Activity
	env.OnActivity(generate.PlanPreflight, mock.Anything, &genbenthosconfigs_activity.PlanPreflightRequest{JobId: "job"}).
		Return(&genbenthosconfigs_activity.PlanPreflightResponse{
			AccountId: "account", Tables: tables, Findings: []*preflight.Finding{planned}, Mappings: mappings,
		}, nil).Once()
	report := &mgmtv1alpha1.PreflightReport{
		Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
		Findings: []*mgmtv1alpha1.PreflightFinding{
			{Kind: mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, Level: mgmtv1alpha1.PreflightFinding_LEVEL_BLOCKING},
			{Kind: planned.Kind, Level: mgmtv1alpha1.PreflightFinding_LEVEL_INFORMATION, Table: planned.Table},
		},
	}
	var check *preflight_activity.Activity
	env.OnActivity(check.CheckPreflight, mock.Anything, mock.Anything).
		Return(func(_ context.Context, req *preflight_activity.CheckPreflightRequest) (*preflight_activity.CheckPreflightResponse, error) {
			assert.Equal(t, "job", req.JobId)
			assert.Equal(t, tables, req.Tables)
			assert.Equal(t, []*preflight.Finding{planned}, req.Findings)
			// What the engine is asked about is what the run would map.
			assert.Equal(t, mappings, req.Mappings)
			return &preflight_activity.CheckPreflightResponse{Report: report}, nil
		}).Once()

	env.ExecuteWorkflow(New().JobPreflight, &Request{JobId: "job"})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	var resp Response
	require.NoError(t, env.GetWorkflowResult(&resp))
	require.Len(t, resp.Report.GetFindings(), 2)
	assert.Equal(t, mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS, resp.Report.GetEngine())
	env.AssertExpectations(t)
}

// A plan that cannot be computed — a source out of reach — fails the check after a second
// try, and asks nothing of the connections.
func Test_JobPreflight_PlanFails(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	var generate *genbenthosconfigs_activity.Activity
	env.OnActivity(generate.PlanPreflight, mock.Anything, mock.Anything).
		Return(nil, errors.New("unable to reach the source")).Twice()

	env.ExecuteWorkflow(New().JobPreflight, &Request{JobId: "job"})
	require.True(t, env.IsWorkflowCompleted())
	require.ErrorContains(t, env.GetWorkflowError(), "unable to reach the source")
	env.AssertExpectations(t)
}
