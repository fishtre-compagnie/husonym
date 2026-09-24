package mcp_server

import (
	"encoding/json"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// transformEmail maps users.email anew.
var transformEmail = map[string]any{
	"job_id": jobId,
	"mappings": []map[string]any{
		{"table": "public.users", "column": "email", "transformer": "transform_email"},
	},
}

// scheduled returns the shop job running every night, with its schedule in status.
func scheduled(status mgmtv1alpha1.JobStatus) *fakeJobService {
	jobService := newFakeJobService()
	schedule := "0 2 * * *"
	jobService.job.CronSchedule = &schedule
	jobService.status = status
	return jobService
}

func Test_UpdateJobMappings(t *testing.T) {
	t.Parallel()

	t.Run("maps a column anew and leaves the others", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())

		res := callTool(t, session, "update_job_mappings", transformEmail)

		_, updated, triggered := jobService.seen()
		require.Len(t, updated, 1)
		require.Empty(t, triggered)
		require.Empty(t, somebody.asked(), "a job that runs only when asked changes without a question")
		mappings := updated[0].GetMappings()
		require.Len(t, mappings, 3)
		require.Equal(t, "email", mappings[1].GetColumn())
		require.NotNil(t, mappings[1].GetTransformer().GetConfig().GetTransformEmailConfig())
		require.NotNil(t, mappings[0].GetTransformer().GetConfig().GetPassthroughConfig())
		require.Equal(
			t,
			connectionId,
			updated[0].GetSource().GetOptions().GetPostgres().GetConnectionId(),
			"the source is sent back as it was",
		)
		require.True(t, updated[0].GetExpectedUpdatedAt().AsTime().Equal(updatedAt.AsTime()),
			"the write expects the job as it was read, so that a change meanwhile is refused, not overwritten")

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"job_id": "`+jobId+`", "changed": 1, "added": 0, "columns": 3}`, string(structured))
	})

	t.Run("adds a table whole, and refuses it in part", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		message := callToolError(t, session, "update_job_mappings", map[string]any{
			"job_id": jobId,
			"mappings": []map[string]any{
				{"table": "public.orders", "column": "note", "transformer": "passthrough"},
			},
		})
		require.Contains(t, message, "missing: public.orders.id, public.orders.user_id")
		_, updated, _ := jobService.seen()
		require.Empty(t, updated)

		res := callTool(t, session, "update_job_mappings", map[string]any{
			"job_id": jobId,
			"mappings": []map[string]any{
				{"table": "public.orders", "column": "id", "transformer": "passthrough"},
				{"table": "public.orders", "column": "user_id", "transformer": "passthrough"},
				{"table": "public.orders", "column": "note", "transformer": "passthrough"},
			},
		})
		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{"job_id": "`+jobId+`", "changed": 0, "added": 3, "columns": 6}`, string(structured))
	})

	t.Run("refuses while a run is going or starting", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, (&person{answer: "accept"}).client())

		callTool(t, session, "run_job", runShop)
		message := callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "was just triggered")

		jobService.start(failedRun("run-1", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING))
		message = callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "a run of this job is in progress (run-1)")
		_, updated, _ := jobService.seen()
		require.Empty(t, updated)
	})

	t.Run("asks first when the job runs on a schedule", func(t *testing.T) {
		t.Parallel()
		jobService := scheduled(mgmtv1alpha1.JobStatus_JOB_STATUS_ENABLED)
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())

		callTool(t, session, "update_job_mappings", transformEmail)

		questions := somebody.asked()
		require.Len(t, questions, 1)
		require.Contains(t, questions[0], `runs on the schedule "0 2 * * *"`)
		require.Contains(t, questions[0], "2 of them copied as they are (passthrough)")
		_, updated, _ := jobService.seen()
		require.Len(t, updated, 1)
	})

	t.Run("changes nothing when the person declines", func(t *testing.T) {
		t.Parallel()
		jobService := scheduled(mgmtv1alpha1.JobStatus_JOB_STATUS_ENABLED)
		session := connectJobs(t, jobService, (&person{answer: "decline"}).client())

		message := callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "declined")
		_, updated, _ := jobService.seen()
		require.Empty(t, updated)
	})

	t.Run("does not ask for a paused job", func(t *testing.T) {
		t.Parallel()
		jobService := scheduled(mgmtv1alpha1.JobStatus_JOB_STATUS_PAUSED)
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())

		callTool(t, session, "update_job_mappings", transformEmail)
		require.Empty(t, somebody.asked())
	})

	t.Run("a yes to one change does not apply another", func(t *testing.T) {
		t.Parallel()
		jobService := scheduled(mgmtv1alpha1.JobStatus_JOB_STATUS_ENABLED)
		session := connectJobs(t, jobService, byHand())
		asked := questionOf(t, session, "update_job_mappings", transformEmail, nil)

		other := map[string]any{
			"job_id": jobId,
			"mappings": []map[string]any{
				{"table": "public.users", "column": "email", "transformer": "passthrough"},
			},
		}
		questionOf(t, session, "update_job_mappings", other, accept(asked))
		_, updated, _ := jobService.seen()
		require.Empty(t, updated)

		answer(t, session, "update_job_mappings", transformEmail, accept(asked))
		_, updated, _ = jobService.seen()
		require.Len(t, updated, 1, "the question about the first change still stands")
	})
}
