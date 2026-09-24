package mcp_server

import (
	"cmp"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

var runShop = map[string]any{"job_id": jobId}

func Test_RunJob(t *testing.T) {
	t.Parallel()

	// Since 2026-07-28 the question travels in the tool's result; before, the server puts it to
	// the client itself. The rule is one.
	for _, protocolVersion := range []string{"", "2025-11-25"} {
		t.Run("asks before each run, protocol "+cmp.Or(protocolVersion, "latest"), func(t *testing.T) {
			t.Parallel()
			jobService := newFakeJobService()
			somebody := &person{answer: "accept"}
			session := connectAPI(t, fakeAPI{
				connections: &fakeConnectionService{}, data: &fakeDataService{}, jobs: jobService,
			}, somebody.client(), protocolVersion)

			callTool(t, session, "run_job", runShop)
			jobService.start(failedRun("run-1", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE))
			callTool(t, session, "run_job", runShop)

			_, _, triggered := jobService.seen()
			require.Equal(t, []string{jobId, jobId}, triggered)
			questions := somebody.asked()
			require.Len(t, questions, 2, "a yes covers one run, never the next")
			require.Contains(t, questions[0], `"shop-anon"`)
			require.Contains(t, questions[0], `reads 3 columns of 1 table from "production" (PostgreSQL)`)
			require.Contains(t, questions[0], "2 of them copied as they are (passthrough)")
			require.Contains(t, questions[0], `"staging" (PostgreSQL) (its tables are emptied first)`)
		})
	}

	t.Run("runs nothing when the person declines", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, (&person{answer: "decline"}).client())

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "declined")
		_, _, triggered := jobService.seen()
		require.Empty(t, triggered)
	})

	t.Run("refuses a client that cannot ask the person", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, nil)

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "cannot ask the person")
		_, _, triggered := jobService.seen()
		require.Empty(t, triggered)
	})

	t.Run("refuses while a run is in progress, before asking", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.runs = []*mgmtv1alpha1.JobRun{
			failedRun("run-1", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_RUNNING),
		}
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "a run of this job is in progress (run-1)")
		require.Empty(t, somebody.asked())
		_, _, triggered := jobService.seen()
		require.Empty(t, triggered)
	})

	t.Run("refuses while the run it triggered has not shown, before asking", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())
		callTool(t, session, "run_job", runShop)

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "was just triggered and has not started yet")
		require.Len(t, somebody.asked(), 1, "the second call asks nothing")
		status := callTool(t, session, "get_run_status", runShop)
		require.Equal(t, true, status.StructuredContent.(map[string]any)["starting"])

		// The run shows, and ends: the job can run again.
		jobService.start(failedRun("run-1", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE))
		status = callTool(t, session, "get_run_status", runShop)
		require.Nil(t, status.StructuredContent.(map[string]any)["starting"])
		callTool(t, session, "run_job", runShop)
		_, _, triggered := jobService.seen()
		require.Len(t, triggered, 2)
	})

	t.Run("a yes to the job as it was does not run it as it is", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, byHand())
		asked := questionOf(t, session, "run_job", runShop, nil)

		jobService.touch()
		questionOf(t, session, "run_job", runShop, accept(asked))
		_, _, triggered := jobService.seen()
		require.Empty(t, triggered)
	})

	t.Run("tells all the run does, not the part the agent changed", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.hook("purge-audit")
		options := jobService.job.Destinations[0].Options.GetPostgresOptions()
		options.TruncateTable.Cascade = true
		options.InitTableSchema = true
		jobService.job.Mappings[2].Transformer = &mgmtv1alpha1.JobMappingTransformer{Config: &mgmtv1alpha1.TransformerConfig{
			Config: &mgmtv1alpha1.TransformerConfig_TransformJavascriptConfig{
				TransformJavascriptConfig: &mgmtv1alpha1.TransformJavascript{Code: "return value"},
			},
		}}
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())

		callTool(t, session, "run_job", runShop)
		question := somebody.asked()[0]
		require.Contains(t, question, "1 of them copied as they are (passthrough), 1 through JavaScript written in the job")
		require.Contains(t, question, "the tables it lacks are created; its tables are emptied first, and the tables referencing them too")
		require.Contains(t, question, `runs the SQL of 1 hook: "purge-audit" before the sync on "staging" (PostgreSQL)`)
	})

	t.Run("a yes before a hook was added does not run the job with it", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, byHand())
		asked := questionOf(t, session, "run_job", runShop, nil)

		jobService.hook("purge-audit")
		questionOf(t, session, "run_job", runShop, accept(asked))
		_, _, triggered := jobService.seen()
		require.Empty(t, triggered)
	})

	t.Run("a yes counts once", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, byHand())
		asked := questionOf(t, session, "run_job", runShop, nil)

		answer(t, session, "run_job", runShop, accept(asked))
		jobService.start(failedRun("run-1", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE))
		questionOf(t, session, "run_job", runShop, accept(asked))
		_, _, triggered := jobService.seen()
		require.Len(t, triggered, 1)
	})

	t.Run("a yes to reading values runs nothing", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, byHand())
		asked := questionOf(t, session, "preview_column", previewEmail, nil)

		questionOf(t, session, "run_job", runShop, accept(asked))
		_, _, triggered := jobService.seen()
		require.Empty(t, triggered)
	})
}

// answer calls a tool with answers to the questions it put, and must succeed.
func answer(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any, answers mcp.InputResponseMap) {
	t.Helper()
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args, InputResponses: answers})
	require.NoError(t, err)
	require.False(t, res.IsError, "%s failed: %v", tool, res.Content)
	require.False(t, res.NeedsInput(), "%s asked again", tool)
}
