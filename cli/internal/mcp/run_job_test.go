package mcp_server

import (
	"cmp"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
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

			started := callTool(t, session, "run_job", runShop)
			require.Equal(t, "run-1", started.StructuredContent.(map[string]any)["run_id"], "the run started is named")
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

	t.Run("refuses while the run it started has not shown, before asking", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())
		callTool(t, session, "run_job", runShop)

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "the run run-1 of this job was just started and does not show among its runs yet")
		require.Len(t, somebody.asked(), 1, "the second call asks nothing")

		// Another run showing is not the one started here: the job is still held.
		jobService.start(failedRun("run-0", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE))
		message = callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "the run run-1 of this job was just started")

		// The run shows, and ends: the job can run again.
		jobService.start(failedRun("run-1", time.Now(), mgmtv1alpha1.JobRunStatus_JOB_RUN_STATUS_COMPLETE))
		callTool(t, session, "run_job", runShop)
		_, _, triggered := jobService.seen()
		require.Len(t, triggered, 2)
	})

	t.Run("holds the job while the trigger is being sent", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())
		// Set once the session is: on a failure the gate opens before the session closes.
		jobService.pauseTrigger = newGate(t)

		done := make(chan error, 1)
		go func() {
			_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "run_job", Arguments: runShop})
			done <- err
		}()
		select {
		case <-jobService.pauseTrigger.entered:
		case err := <-done:
			require.FailNow(t, "run_job ended before its trigger", "%v", err)
		}

		// The API answers once the run has started: until then the run is not listed, and the
		// job is held all the same.
		message := callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "was just triggered and may be starting")
		message = callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "was just triggered and may be starting")

		jobService.pauseTrigger.open()
		require.NoError(t, <-done)
		_, updated, triggered := jobService.seen()
		require.Empty(t, updated)
		require.Len(t, triggered, 1)
		require.Len(t, somebody.asked(), 1)
	})

	// Finding the job idle and acting on it are two steps: a call arriving between the two of
	// another is refused.
	t.Run("is refused while a change of the mappings is being written", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		somebody := &person{answer: "accept"}
		session := connectJobs(t, jobService, somebody.client())
		// Set once the session is: on a failure the gate opens before the session closes.
		jobService.pauseUpdate = newGate(t)

		done := make(chan error, 1)
		go func() {
			_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "update_job_mappings", Arguments: transformEmail})
			done <- err
		}()
		select {
		case <-jobService.pauseUpdate.entered:
		case err := <-done:
			require.FailNow(t, "update_job_mappings ended before its write", "%v", err)
		}

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "another call is running or changing this job")
		require.Empty(t, somebody.asked())

		jobService.pauseUpdate.open()
		require.NoError(t, <-done)
		_, updated, triggered := jobService.seen()
		require.Len(t, updated, 1)
		require.Empty(t, triggered)
	})

	// The job is taken before it is read: the person answers for the job as read then.
	t.Run("refuses a change of the mappings while it reads the job to run it", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		session := connectJobs(t, jobService, (&person{answer: "accept"}).client())
		// Set once the session is: on a failure the gate opens before the session closes.
		jobService.pauseJob = newGate(t)

		done := make(chan error, 1)
		go func() {
			_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "run_job", Arguments: runShop})
			done <- err
		}()
		select {
		case <-jobService.pauseJob.entered:
		case err := <-done:
			require.FailNow(t, "run_job ended before reading the job", "%v", err)
		}

		message := callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "another call is running or changing this job")

		jobService.pauseJob.open()
		require.NoError(t, <-done)
		_, updated, triggered := jobService.seen()
		require.Empty(t, updated)
		require.Len(t, triggered, 1)
	})

	t.Run("holds the job when the trigger fails without telling whether a run started", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.triggerErr = connect.NewError(connect.CodeUnknown, errors.New("the run started, but it could not be read"))
		session := connectJobs(t, jobService, (&person{answer: "accept"}).client())

		callToolError(t, session, "run_job", runShop)
		message := callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "was just triggered and may be starting")
	})

	t.Run("does not hold the job when the API refused the trigger", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.triggerErr = connect.NewError(connect.CodePermissionDenied, errors.New("missing job:execute"))
		session := connectJobs(t, jobService, (&person{answer: "accept"}).client())

		message := callToolError(t, session, "run_job", runShop)
		require.Contains(t, message, "missing job:execute")
		callTool(t, session, "update_job_mappings", transformEmail)
	})

	t.Run("holds the job when the API does not name the run", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.unnamed = true
		session := connectJobs(t, jobService, (&person{answer: "accept"}).client())

		callTool(t, session, "run_job", runShop)
		message := callToolError(t, session, "update_job_mappings", transformEmail)
		require.Contains(t, message, "was just triggered and may be starting")
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
