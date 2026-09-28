package mcp_server

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// driverError is how a driver fails to reach a database: it can quote where and how it
// connects, secret included.
const driverError = "failed to connect to `user=shop password=" + clearPassword + " host=db.internal`: dial tcp: i/o timeout"

// shopReport is what the API finds for the shop job: the API lists findings in the order it
// makes them, not by level.
func shopReport() *mgmtv1alpha1.PreflightJobResponse {
	remedy := `GRANT TRUNCATE ON "public"."users" TO "staging"`
	return &mgmtv1alpha1.PreflightJobResponse{
		CheckedAt: timestamppb.New(time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)),
		Report: &mgmtv1alpha1.PreflightReport{
			Engine: mgmtv1alpha1.JobEngine_JOB_ENGINE_ATHANOR,
			Findings: []*mgmtv1alpha1.PreflightFinding{
				{
					Kind:    mgmtv1alpha1.PreflightFinding_KIND_DESTINATION_TRIGGERS,
					Level:   mgmtv1alpha1.PreflightFinding_LEVEL_INFORMATION,
					Table:   "public.users",
					Message: "the triggers of public.users are taken out of the way of the run",
				},
				{
					Kind:    mgmtv1alpha1.PreflightFinding_KIND_OUTPUT_TOO_LONG,
					Level:   mgmtv1alpha1.PreflightFinding_LEVEL_WARNING,
					Table:   "public.users",
					Columns: []string{"email"},
					Message: "generate_email may write more than the 40 characters email takes",
				},
				{
					Kind:         mgmtv1alpha1.PreflightFinding_KIND_TRUNCATE,
					Level:        mgmtv1alpha1.PreflightFinding_LEVEL_BLOCKING,
					ConnectionId: new(destinationId),
					Table:        "public.users",
					Missing:      []string{"TRUNCATE"},
					Message:      "staging cannot empty public.users",
					Remedy:       &remedy,
				},
			},
		},
	}
}

func Test_PreflightJob(t *testing.T) {
	t.Parallel()

	t.Run("gives the report, what stops a run first", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.preflight = shopReport()
		session := connectJobs(t, jobService, nil)

		res := callTool(t, session, "preflight_job", map[string]any{"job_id": jobId})

		structured, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"job_id": "`+jobId+`",
			"engine": "athanor",
			"checked_at": "2026-09-28T09:00:00Z",
			"runnable": false,
			"findings": [
				{
					"level": "blocking", "kind": "truncate", "connection_id": "`+destinationId+`",
					"table": "public.users", "missing": ["TRUNCATE"], "message": "staging cannot empty public.users",
					"remedy": "GRANT TRUNCATE ON \"public\".\"users\" TO \"staging\""
				},
				{
					"level": "warning", "kind": "output_too_long", "table": "public.users", "columns": ["email"],
					"message": "generate_email may write more than the 40 characters email takes"
				},
				{
					"level": "information", "kind": "destination_triggers", "table": "public.users",
					"message": "the triggers of public.users are taken out of the way of the run"
				}
			]
		}`, string(structured))
	})

	t.Run("a job without blocking findings is runnable", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.preflight = shopReport()
		jobService.preflight.Report.Findings = jobService.preflight.Report.Findings[:2]
		session := connectJobs(t, jobService, nil)

		res := callTool(t, session, "preflight_job", map[string]any{"job_id": jobId})

		out, ok := res.StructuredContent.(map[string]any)
		require.True(t, ok)
		require.Equal(t, true, out["runnable"])
	})

	t.Run("a connection out of reach is named without the driver's words", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		// As the API says it: the reason the worker gave, after its own words.
		jobService.preflightErr = connect.NewError(connect.CodeUnavailable,
			errors.New("the pre-flight check could not end: "+driverError))
		session := connectJobs(t, jobService, nil)

		message := callToolError(t, session, "preflight_job", map[string]any{"job_id": jobId})

		require.NotContains(t, message, clearPassword)
		require.NotContains(t, message, "db.internal")
		require.Contains(t, message, "check_connection")
	})

	t.Run("an error of the API's own making reaches the model", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.preflightErr = connect.NewError(connect.CodeFailedPrecondition,
			errors.New("no worker serves this account: the pre-flight check runs on one, start it and check again"))
		session := connectJobs(t, jobService, nil)

		message := callToolError(t, session, "preflight_job", map[string]any{"job_id": jobId})

		require.Contains(t, message, "no worker serves this account")
	})

	t.Run("any other error is replaced", func(t *testing.T) {
		t.Parallel()
		jobService := newFakeJobService()
		jobService.preflightErr = errors.New("unable to create sql connection: " + driverError)
		session := connectJobs(t, jobService, nil)

		message := callToolError(t, session, "preflight_job", map[string]any{"job_id": jobId})

		require.NotContains(t, message, clearPassword)
		require.Contains(t, message, "could not end")
	})
}
