package jobs

import (
	"context"
	"fmt"
	"slices"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// Runs returns the runs of a job, most recent first, without the message of any failure.
func (r *Reader) Runs(ctx context.Context, jobId string) ([]*mgmtv1alpha1.JobRun, error) {
	res, err := r.client.GetJobRuns(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunsRequest{
		Id: &mgmtv1alpha1.GetJobRunsRequest_JobId{JobId: jobId},
	}))
	if err != nil {
		return nil, fmt.Errorf("unable to read the runs of job %s: %w", jobId, err)
	}
	runs := res.Msg.GetJobRuns()
	for _, run := range runs {
		withoutFailures(run)
	}
	slices.SortStableFunc(runs, func(a, b *mgmtv1alpha1.JobRun) int {
		return b.GetStartedAt().AsTime().Compare(a.GetStartedAt().AsTime())
	})
	return runs, nil
}

// GetRun returns one run with its pending activities, without the message of any failure.
func (r *Reader) GetRun(ctx context.Context, runId string) (*mgmtv1alpha1.JobRun, error) {
	res, err := r.client.GetJobRun(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunRequest{
		JobRunId:  runId,
		AccountId: r.accountId,
	}))
	if err != nil {
		return nil, err
	}
	return withoutFailures(res.Msg.GetJobRun()), nil
}

// Events returns what a run recorded, table by table, without the message of any failure. The
// tables are synced by workflows of their own, whose failures show here and not among the
// run's pending activities.
func (r *Reader) Events(ctx context.Context, runId string) ([]*mgmtv1alpha1.JobRunEvent, error) {
	res, err := r.client.GetJobRunEvents(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRunEventsRequest{
		JobRunId:  runId,
		AccountId: r.accountId,
	}))
	if err != nil {
		return nil, fmt.Errorf("unable to read the events of run %s: %w", runId, err)
	}
	events := res.Msg.GetEvents()
	for _, event := range events {
		for _, task := range event.GetTasks() {
			if task.Error != nil {
				task.Error.Message = ""
			}
		}
	}
	return events, nil
}

// withoutFailures empties the message of each failure of a run, and keeps the failure: that an
// activity failed is no value, what it failed on can be one.
func withoutFailures(run *mgmtv1alpha1.JobRun) *mgmtv1alpha1.JobRun {
	for _, activity := range run.GetPendingActivities() {
		if activity.LastFailure != nil {
			activity.LastFailure = &mgmtv1alpha1.ActivityFailure{}
		}
	}
	return run
}

// Failed says whether a run has something failed to tell: it ended otherwise than complete,
// one of its activities failed and is retried, or one of its events recorded an error.
func Failed(run *mgmtv1alpha1.JobRun, events []*mgmtv1alpha1.JobRunEvent) bool {
	switch run.GetStatus() {
	case mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_PENDING,
		mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING:
	default:
		return true
	}
	if slices.ContainsFunc(run.GetPendingActivities(), func(activity *mgmtv1alpha1.PendingActivity) bool {
		return activity.LastFailure != nil
	}) {
		return true
	}
	return slices.ContainsFunc(events, func(event *mgmtv1alpha1.JobRunEvent) bool {
		return slices.ContainsFunc(event.GetTasks(), func(task *mgmtv1alpha1.JobRunEventTask) bool { return task.Error != nil })
	})
}

// FailingTables lists the tables, as schema.table, whose events recorded an error.
func FailingTables(events []*mgmtv1alpha1.JobRunEvent) []string {
	var tables []string
	for _, event := range events {
		sync := event.GetMetadata().GetSyncMetadata()
		if sync == nil || !slices.ContainsFunc(event.GetTasks(), func(task *mgmtv1alpha1.JobRunEventTask) bool {
			return task.Error != nil
		}) {
			continue
		}
		table := sync.GetSchema() + "." + sync.GetTable()
		if !slices.Contains(tables, table) {
			tables = append(tables, table)
		}
	}
	return tables
}
