package workflow_shared

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/runevents"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/workflow"
)

// Utility function that handles spawning job run lifecycle hooks: created, success, failed
// Should only be used by root workflows that are responsible for handling the lifecycle of a job run
//
// licensed is the answer the run got from LicenseIsValid at its start: the hooks of its end
// follow it, whatever became of the license meanwhile.
func HandleWorkflowEventLifecycle[T any](
	ctx workflow.Context,
	licensed bool,
	jobId,
	runId string, // typically the temporal workflow execution id
	logger log.Logger,
	getAccountId func() (string, error),
	fn func(ctx workflow.Context, logger log.Logger) (*T, error),
) (*T, error) {
	if !licensed {
		logger.Debug("ee license is not valid, skipping event lifecycle")
		return fn(ctx, logger)
	}

	accountId, err := getAccountId()
	if err != nil {
		return nil, err
	}

	run := runevents.Run{AccountID: accountId, JobID: jobId, RunID: runId}

	if err := spawnLifecycleHook(ctx, run, jobRunCreatedHook, logger); err != nil {
		return nil, err
	}

	resp, err := fn(ctx, logger)
	if err != nil {
		if spawnErr := spawnLifecycleHook(ctx, run, jobRunFailedHook, logger); spawnErr != nil {
			return nil, errors.Join(err, spawnErr)
		}
		return nil, err
	}

	if err := spawnLifecycleHook(ctx, run, jobRunSucceededHook, logger); err != nil {
		return nil, err
	}

	return resp, nil
}

// lifecycleHook is one of the moments of a job run that account hooks are told about.
type lifecycleHook struct {
	name    string // part of the child workflow id
	summary string
	event   func(run runevents.Run, at time.Time) *runevents.Event
}

var (
	jobRunCreatedHook = lifecycleHook{
		name:    "job-run-created",
		summary: "Account Hook: Job Run Created",
		event:   runevents.Run.Created,
	}
	jobRunFailedHook = lifecycleHook{
		name:    "job-run-failed",
		summary: "Account Hook: Job Run Failed",
		event:   runevents.Run.Failed,
	}
	jobRunSucceededHook = lifecycleHook{
		name:    "job-run-succeeded",
		summary: "Account Hook: Job Run Succeeded",
		event:   runevents.Run.Succeeded,
	}
)

// spawnLifecycleHook starts the child workflow that processes the account hooks of the
// event, and waits until it has started. The child outlives its parent. The event is
// stamped with the workflow's time, the same on every replay.
func spawnLifecycleHook(
	ctx workflow.Context,
	run runevents.Run,
	hook lifecycleHook,
	logger log.Logger,
) error {
	now := workflow.Now(ctx)
	future := workflow.ExecuteChildWorkflow(
		workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{
			ParentClosePolicy: enums.PARENT_CLOSE_POLICY_ABANDON,
			WorkflowID:        getAccountHookChildWorkflowId(run.RunID, hook.name, now),
			StaticSummary:     hook.summary,
		}),
		accounthooks.ProcessAccountHook,
		&accounthooks.ProcessAccountHookRequest{
			Event: hook.event(run, now),
		},
	)
	var childWE workflow.Execution
	if err := future.GetChildWorkflowExecution().Get(ctx, &childWE); err != nil {
		return err
	}
	logger.Debug(fmt.Sprintf("child wf event spawned: %s", childWE.ID))
	return nil
}

func getAccountHookChildWorkflowId(parentJobRunId, eventName string, now time.Time) string {
	return BuildChildWorkflowId(parentJobRunId, "hook-"+eventName, now)
}

// Builds a child workflow id that is unique for the given parent execution. Sanitizes the name and cuts to the max allowed limit
func BuildChildWorkflowId(parentExecutionId, name string, ts time.Time) string {
	id := fmt.Sprintf(
		"%s-%s-%d",
		parentExecutionId,
		SanitizeWorkflowID(strings.ToLower(name)),
		ts.UnixNano(),
	)
	if len(id) > 1000 {
		id = id[:1000]
	}
	return id
}

var invalidWorkflowIDChars = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

func SanitizeWorkflowID(id string) string {
	return invalidWorkflowIDChars.ReplaceAllString(id, "_")
}
