// Package preflight_workflow tells what a run of a job would meet, before any run.
//
// It computes the plan of the run the way the run computes it — the same activity code, on
// the same worker, so with the same engine and page size — then asks the connections what
// their roles need, and returns the report. Nothing is read from the tables nor written:
// not the job, not a run context, not the key of the account. A run started afterwards
// computes the same report again, keeps it, and stops on a blocking finding.
package preflight_workflow

import (
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	genbenthosconfigs_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/gen-benthos-configs"
	preflight_activity "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/preflight"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type Request struct {
	JobId string
}

type Response struct {
	Report *mgmtv1alpha1.PreflightReport
}

// WorkflowId is the id of the check of a job: one runs at a time, and a check asked while
// one runs waits for it.
func WorkflowId(jobId string) string {
	return "preflight-" + jobId
}

type Workflow struct{}

func New() *Workflow {
	return &Workflow{}
}

// JobPreflight returns the pre-flight report of a job. Its activities only read: a failure
// to ask is asked again, once.
func (w *Workflow) JobPreflight(ctx workflow.Context, req *Request) (*Response, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		HeartbeatTimeout:    1 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 2},
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var plan *genbenthosconfigs_activity.PlanPreflightResponse
	var generate *genbenthosconfigs_activity.Activity
	err := workflow.ExecuteActivity(ctx, generate.PlanPreflight,
		&genbenthosconfigs_activity.PlanPreflightRequest{JobId: req.JobId}).Get(ctx, &plan)
	if err != nil {
		return nil, err
	}

	var checked *preflight_activity.CheckPreflightResponse
	var check *preflight_activity.Activity
	err = workflow.ExecuteActivity(ctx, check.CheckPreflight, &preflight_activity.CheckPreflightRequest{
		JobId:    req.JobId,
		Tables:   plan.Tables,
		Findings: plan.Findings,
		Mappings: plan.Mappings,
	}).Get(ctx, &checked)
	if err != nil {
		return nil, err
	}
	return &Response{Report: checked.Report}, nil
}
