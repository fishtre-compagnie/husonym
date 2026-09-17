package orchestrate

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/env"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
)

const pollInterval = time.Second

var jobEngines = map[env.Engine]mgmtv1alpha1.JobEngine{
	env.Benthos: mgmtv1alpha1.JobEngine_JOB_ENGINE_BENTHOS,
	env.Athanor: mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR,
}

// CreateJob creates the job syncing one case with one engine. Both engines receive the
// same mappings, subset and options: only the engine and the destination differ.
//
// Table syncs get the attempts the case asks for, one by default (see cases.Job).
func (c *Client) CreateJob(
	ctx context.Context,
	cs *cases.Case,
	engine env.Engine,
	sourceConnID, destConnID, runTag string,
) (string, error) {
	jobEngine, ok := jobEngines[engine]
	if !ok {
		return "", fmt.Errorf("orchestrate: unknown engine %q", engine)
	}
	database := cs.Database()

	var mappings []*mgmtv1alpha1.JobMapping
	tables := make([]*mgmtv1alpha1.MysqlSourceTableOption, 0, len(cs.Tables))
	var virtualFks []*mgmtv1alpha1.VirtualForeignConstraint
	for _, t := range cs.Tables {
		if cs.IsExcluded(t.Name) {
			continue
		}
		for i := range t.Columns {
			column := t.Columns[i].Name
			transformer := passthrough()
			if spec, ok := cs.Spec(t.Name, column); ok && spec.Transformer != nil {
				transformer = spec.Transformer
			}
			mappings = append(mappings, &mgmtv1alpha1.JobMapping{
				Schema: database, Table: t.Name, Column: column,
				Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: transformer},
			})
		}
		option := &mgmtv1alpha1.MysqlSourceTableOption{Table: t.Name}
		if where, ok := cs.Job.Where[t.Name]; ok {
			option.WhereClause = &where
		}
		tables = append(tables, option)
		for _, fk := range t.ForeignKeys {
			if !fk.Virtual {
				continue
			}
			virtualFks = append(virtualFks, &mgmtv1alpha1.VirtualForeignConstraint{
				Schema: database, Table: t.Name, Columns: fk.Columns,
				ForeignKey: &mgmtv1alpha1.VirtualForeignKey{Schema: database, Table: fk.RefTable, Columns: fk.RefColumns},
			})
		}
	}

	attempts := max(cs.Job.SyncAttempts, 1)
	var onConflict *mgmtv1alpha1.MysqlOnConflictConfig
	if cs.Job.OnConflictUpdate {
		onConflict = &mgmtv1alpha1.MysqlOnConflictConfig{
			Strategy: &mgmtv1alpha1.MysqlOnConflictConfig_Update{Update: &mgmtv1alpha1.MysqlOnConflictConfig_MysqlOnConflictUpdate{}},
		}
	}
	resp, err := c.jobs.CreateJob(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
		AccountId: c.accountID,
		JobName:   jobName(cs.ID, engine, runTag),
		Mappings:  mappings,
		Source: &mgmtv1alpha1.JobSource{Options: &mgmtv1alpha1.JobSourceOptions{
			Config: &mgmtv1alpha1.JobSourceOptions_Mysql{Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{
				ConnectionId:                  sourceConnID,
				Schemas:                       []*mgmtv1alpha1.MysqlSourceSchemaOption{{Schema: database, Tables: tables}},
				SubsetByForeignKeyConstraints: cs.Job.SubsetByForeignKeys,
			}},
		}},
		Destinations: []*mgmtv1alpha1.CreateJobDestination{{
			ConnectionId: destConnID,
			Options: &mgmtv1alpha1.JobDestinationOptions{
				Config: &mgmtv1alpha1.JobDestinationOptions_MysqlOptions{MysqlOptions: &mgmtv1alpha1.MysqlDestinationConnectionOptions{
					SkipForeignKeyViolations: cs.Job.SkipForeignKeyViolations,
					TruncateTable: &mgmtv1alpha1.MysqlTruncateTableConfig{
						TruncateBeforeInsert: cs.Job.TruncateBeforeInsert,
					},
					OnConflict: onConflict,
				}},
			},
		}},
		VirtualForeignKeys: virtualFks,
		WorkflowOptions:    &mgmtv1alpha1.WorkflowOptions{Engine: jobEngine},
		SyncOptions: &mgmtv1alpha1.ActivityOptions{
			RetryPolicy: &mgmtv1alpha1.RetryPolicy{MaximumAttempts: &attempts},
		},
	}))
	if err != nil {
		return "", fmt.Errorf("orchestrate: create job for %s on %s: %w", cs.ID, engine, err)
	}
	return resp.Msg.GetJob().GetId(), nil
}

func passthrough() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{PassthroughConfig: &mgmtv1alpha1.Passthrough{}},
	}
}

// jobName fits the API pattern ^[a-z0-9-]{3,100}$; job names are unique per account, so
// each bench run tags its own.
func jobName(caseID string, engine env.Engine, runTag string) string {
	name := strings.ToLower(fmt.Sprintf("bench-%s-%s-%s", runTag, caseID, engine))
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// RunResult is the outcome of one job run.
type RunResult struct {
	RunID    string
	Status   mgmtv1alpha1.JobRunStatus
	Duration time.Duration
	// Errors are the distinct activity errors of the run, in order of appearance.
	Errors []string
	// TimedOut is set when the run had not finished in the time the bench gives it; it
	// was then terminated.
	TimedOut bool
}

// Succeeded reports whether the run completed.
func (r *RunResult) Succeeded() bool {
	return r.Status == mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE
}

// Run starts a run of the job and waits for its final status, at most for timeout. A
// run still going by then is terminated and reported as timed out, not as an error of
// the bench: an engine that retries a failing write for minutes is a finding.
func (c *Client) Run(ctx context.Context, jobID string, timeout time.Duration) (*RunResult, error) {
	// The runs a job already has: the perf mode runs the same job several times, and the
	// one being waited for is the one that was not there before.
	previous, err := c.runIDs(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if _, err := c.jobs.CreateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.CreateJobRunRequest{JobId: jobID})); err != nil {
		return nil, fmt.Errorf("orchestrate: start run of %s: %w", jobID, err)
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	var lastSeen *mgmtv1alpha1.JobRun
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("orchestrate: run of %s: %w", jobID, ctx.Err())
		case <-deadline.C:
			return c.terminate(ctx, jobID, lastSeen, timeout)
		case <-ticker.C:
		}
		run, err := c.newRun(ctx, jobID, previous)
		if err != nil {
			return nil, err
		}
		if run != nil {
			lastSeen = run
		}
		if run == nil || !isFinal(run.GetStatus()) {
			continue
		}
		result := &RunResult{RunID: run.GetId(), Status: run.GetStatus()}
		if run.GetStartedAt() != nil && run.GetCompletedAt() != nil {
			result.Duration = run.GetCompletedAt().AsTime().Sub(run.GetStartedAt().AsTime())
		}
		if !result.Succeeded() {
			result.Errors, err = c.runErrors(ctx, run.GetId())
			if err != nil {
				return nil, err
			}
		}
		return result, nil
	}
}

// terminate stops a run that outlived the time the bench gives it, and keeps the errors
// it was retrying on.
func (c *Client) terminate(
	ctx context.Context,
	jobID string,
	run *mgmtv1alpha1.JobRun,
	timeout time.Duration,
) (*RunResult, error) {
	if run == nil {
		return nil, fmt.Errorf("orchestrate: run of %s never showed up within %s", jobID, timeout)
	}
	if _, err := c.jobs.TerminateJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.TerminateJobRunRequest{
		JobRunId: run.GetId(), AccountId: c.accountID,
	})); err != nil {
		return nil, fmt.Errorf("orchestrate: terminate run %s: %w", run.GetId(), err)
	}
	errs, err := c.runErrors(ctx, run.GetId())
	if err != nil {
		return nil, err
	}
	return &RunResult{RunID: run.GetId(), Status: run.GetStatus(), Duration: timeout, Errors: errs, TimedOut: true}, nil
}

// newRun returns the run of a job that is not among the ones it already had, or nil while
// the API does not list it yet: CreateJobRun returns before the workflow is visible.
// Waiting for a new run, rather than for the only one, is what lets a job be run again:
// an earlier run is already final, and would be reported at once as the result of this one.
func (c *Client) newRun(
	ctx context.Context,
	jobID string,
	previous map[string]bool,
) (*mgmtv1alpha1.JobRun, error) {
	resp, err := c.jobs.GetJobRuns(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunsRequest{
		Id: &mgmtv1alpha1.GetJobRunsRequest_JobId{JobId: jobID},
	}))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: runs of %s: %w", jobID, err)
	}
	var found *mgmtv1alpha1.JobRun
	for _, run := range resp.Msg.GetJobRuns() {
		if previous[run.GetId()] {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("orchestrate: job %s started %s and %s at once",
				jobID, found.GetId(), run.GetId())
		}
		found = run
	}
	return found, nil
}

// runIDs returns the runs a job already has.
func (c *Client) runIDs(ctx context.Context, jobID string) (map[string]bool, error) {
	resp, err := c.jobs.GetJobRuns(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunsRequest{
		Id: &mgmtv1alpha1.GetJobRunsRequest_JobId{JobId: jobID},
	}))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: runs of %s: %w", jobID, err)
	}
	ids := map[string]bool{}
	for _, run := range resp.Msg.GetJobRuns() {
		ids[run.GetId()] = true
	}
	return ids, nil
}

func isFinal(status mgmtv1alpha1.JobRunStatus) bool {
	switch status {
	case mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_ERROR,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_FAILED,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_CANCELED,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TERMINATED,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_TIMED_OUT:
		return true
	default:
		return false
	}
}

func (c *Client) runErrors(ctx context.Context, runID string) ([]string, error) {
	resp, err := c.jobs.GetJobRunEvents(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunEventsRequest{
		JobRunId: runID, AccountId: c.accountID,
	}))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: events of %s: %w", runID, err)
	}
	var messages []string
	seen := map[string]bool{}
	for _, event := range resp.Msg.GetEvents() {
		for _, task := range event.GetTasks() {
			message := task.GetError().GetMessage()
			if message != "" && !seen[message] {
				seen[message] = true
				messages = append(messages, message)
			}
		}
	}
	return messages, nil
}

// PlanPageLimit returns the page size the worker planned one table of a run with, read
// from the engine-neutral plan both engines share. The bench sizes its rows from its own
// page limit: a worker paging differently would let the pagination cases pass untested.
func (c *Client) PlanPageLimit(ctx context.Context, runID, database, table string) (int, error) {
	runConfigID := fmt.Sprintf("%s.%s.%s", database, table, runconfigs.RunTypeInsert)
	resp, err := c.jobs.GetRunContext(ctx, connect.NewRequest(&mgmtv1alpha1.GetRunContextRequest{
		Id: &mgmtv1alpha1.RunContextKey{
			JobRunId:   runID,
			ExternalId: shared.GetTablePlanExternalId(runConfigID),
			AccountId:  c.accountID,
		},
	}))
	if err != nil {
		return 0, fmt.Errorf("orchestrate: plan of %s in run %s: %w", runConfigID, runID, err)
	}
	plan, err := tableplan.Unmarshal(resp.Msg.GetValue())
	if err != nil {
		return 0, err
	}
	return plan.PageLimit, nil
}

// Activity is one activity of a run, as the event history tells it.
type Activity struct {
	Type     string        `json:"type"`
	Duration time.Duration `json:"duration"`
}

// Activities returns the activities of a run with the time each of them took. The perf
// mode reads there what a run spends its time on: generating the configs, initializing
// the schema, syncing each table, checking the integrity at the end.
func (c *Client) Activities(ctx context.Context, runID string) ([]Activity, error) {
	resp, err := c.jobs.GetJobRunEvents(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunEventsRequest{
		JobRunId: runID, AccountId: c.accountID,
	}))
	if err != nil {
		return nil, fmt.Errorf("orchestrate: events of %s: %w", runID, err)
	}
	var activities []Activity
	for _, event := range resp.Msg.GetEvents() {
		start, closed := event.GetStartTime(), event.GetCloseTime()
		if start == nil || closed == nil {
			continue
		}
		activities = append(activities, Activity{
			Type:     event.GetType(),
			Duration: closed.AsTime().Sub(start.AsTime()),
		})
	}
	return activities, nil
}
