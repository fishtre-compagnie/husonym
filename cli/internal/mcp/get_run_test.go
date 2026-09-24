package mcp_server

import (
	"encoding/json"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

var (
	earlier = time.Date(2026, 9, 20, 2, 0, 0, 0, time.UTC)
	later   = time.Date(2026, 9, 21, 2, 0, 0, 0, time.UTC)
)

// withFailedRuns returns the shop job with two failed runs, listed oldest first, and the events
// of a table that failed on a value.
func withFailedRuns() *fakeJobService {
	jobService := newFakeJobService()
	jobService.runs = []*mgmtv1alpha1.JobRun{
		failedRun("run-1", earlier, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_FAILED),
		failedRun("run-2", later, mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING),
	}
	// A table is synced by a workflow of its own: its failure is an event of the run, not one
	// of the run's pending activities.
	tableFailed := func() *mgmtv1alpha1.JobRunEvent {
		return &mgmtv1alpha1.JobRunEvent{
			Id:   1,
			Type: "TableSync",
			Metadata: &mgmtv1alpha1.JobRunEventMetadata{Metadata: &mgmtv1alpha1.JobRunEventMetadata_SyncMetadata{
				SyncMetadata: &mgmtv1alpha1.JobRunSyncMetadata{Schema: "public", Table: "users"},
			}},
			Tasks: []*mgmtv1alpha1.JobRunEventTask{
				{Id: 1, Type: "StartChildWorkflowExecutionInitiated"},
				{Id: 2, Type: "ChildWorkflowExecutionFailed", Error: &mgmtv1alpha1.JobRunEventTaskError{
					Message: "invalid input syntax for type uuid: \"" + failedOn + "\"", RetryState: "RETRY_STATE_IN_PROGRESS",
				}},
			},
		}
	}
	jobService.events = map[string][]*mgmtv1alpha1.JobRunEvent{
		"run-1": {tableFailed()},
		"run-2": {tableFailed()},
	}
	return jobService
}

func Test_GetRunStatus(t *testing.T) {
	t.Parallel()
	session := connectJobs(t, withFailedRuns(), nil)

	res := callTool(t, session, "get_run_status", map[string]any{"job_id": jobId})

	structured, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NotContains(t, string(structured), failedOn, "what a run failed on is read through get_run_failure only")
	require.JSONEq(t, `{
		"runs": [
			{"run_id": "run-2", "status": "running", "started_at": "2026-09-21T02:00:00Z"},
			{"run_id": "run-1", "status": "failed", "started_at": "2026-09-20T02:00:00Z"}
		],
		"latest": {
			"run_id": "run-2",
			"failing_tables": ["public.users"],
			"activities": [{"activity": "RunSqlInitTableStatements", "status": "started", "failing": true}]
		}
	}`, string(structured))
}

func Test_GetRunFailure(t *testing.T) {
	t.Parallel()

	t.Run("asks about the source of the job, then gives the messages", func(t *testing.T) {
		t.Parallel()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, withFailedRuns(), somebody.client())

		res := callTool(t, session, "get_run_failure", map[string]any{"run_id": "run-1"})

		questions := somebody.asked()
		require.Len(t, questions, 1)
		require.Contains(t, questions[0], `run run-1 of the job "shop-anon"`)
		require.Contains(t, questions[0], `the connection "production"`)

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"run_id": "run-1",
			"status": "failed",
			"activities": [{"activity": "RunSqlInitTableStatements", "message": "Duplicate entry '`+failedOn+`' for key 'email'"}],
			"tasks": [{
				"table": "public.users",
				"type": "ChildWorkflowExecutionFailed",
				"message": "invalid input syntax for type uuid: \"`+failedOn+`\"",
				"retry_state": "RETRY_STATE_IN_PROGRESS"
			}]
		}`, string(structured))
	})

	t.Run("asks nothing about a run with nothing failed", func(t *testing.T) {
		t.Parallel()
		jobService := withFailedRuns()
		jobService.start(&mgmtv1alpha1.JobRun{
			Id: "run-3", JobId: jobId, Status: mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE,
		})
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())

		res := callTool(t, session, "get_run_failure", map[string]any{"run_id": "run-3"})
		require.Empty(t, somebody.asked())
		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"run_id": "run-3", "status": "complete", "activities": [], "tasks": []}`, string(structured))
	})

	t.Run("asks about a running run whose activity fails and is retried", func(t *testing.T) {
		t.Parallel()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, withFailedRuns(), somebody.client())

		callTool(t, session, "get_run_failure", map[string]any{"run_id": "run-2"})
		require.Len(t, somebody.asked(), 1)
	})

	t.Run("reads nothing when the person declines", func(t *testing.T) {
		t.Parallel()
		session := connectJobs(t, withFailedRuns(), (&person{answer: "decline"}).client())

		message := callToolError(t, session, "get_run_failure", map[string]any{"run_id": "run-1"})
		require.Contains(t, message, "declined")
		require.NotContains(t, message, failedOn)
	})

	t.Run("refuses a client that cannot ask the person", func(t *testing.T) {
		t.Parallel()
		session := connectJobs(t, withFailedRuns(), nil)

		message := callToolError(t, session, "get_run_failure", map[string]any{"run_id": "run-1"})
		require.Contains(t, message, "cannot ask the person")
	})

	t.Run("shares the consent of preview_column on the same connection", func(t *testing.T) {
		t.Parallel()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, withFailedRuns(), somebody.client())

		callTool(t, session, "preview_column", previewEmail)
		callTool(t, session, "get_run_failure", map[string]any{"run_id": "run-1"})
		require.Len(t, somebody.asked(), 1, "the consent covers the connection, whichever way its values come")
	})
}

func Test_Bounded(t *testing.T) {
	t.Parallel()
	require.Equal(t, "short", bounded("short"))

	long := string(make([]byte, maxFailureMessage-1)) + "é"
	cut := bounded(long)
	require.Equal(t, string(make([]byte, maxFailureMessage-1))+" […cut]", cut, "a rune is never cut in half")
}
