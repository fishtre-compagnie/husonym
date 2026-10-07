package runusage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

// recorder is a usage client that keeps what it is sent, and answers with its error.
// It embeds the client interface, so a call to a method it does not define fails loudly
// and the fake keeps compiling when the service gains a procedure.
type recorder struct {
	mgmtv1alpha1connect.UsageServiceClient

	started []*mgmtv1alpha1.RecordRunStartedRequest
	ended   []*mgmtv1alpha1.RecordRunEndedRequest
	err     error
}

func (r *recorder) RecordRunStarted(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.RecordRunStartedRequest],
) (*connect.Response[mgmtv1alpha1.RecordRunStartedResponse], error) {
	r.started = append(r.started, req.Msg)
	if r.err != nil {
		return nil, r.err
	}
	return connect.NewResponse(&mgmtv1alpha1.RecordRunStartedResponse{}), nil
}

func (r *recorder) RecordRunEnded(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.RecordRunEndedRequest],
) (*connect.Response[mgmtv1alpha1.RecordRunEndedResponse], error) {
	r.ended = append(r.ended, req.Msg)
	if r.err != nil {
		return nil, r.err
	}
	return connect.NewResponse(&mgmtv1alpha1.RecordRunEndedResponse{}), nil
}

var (
	testStartedAt = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	testEndedAt   = time.Date(2026, 10, 7, 9, 12, 30, 0, time.UTC)
)

func Test_RecordRunStarted_SendsWhatItIsGiven(t *testing.T) {
	client := &recorder{}
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	activities := New(client)
	Register(env, activities)

	_, err := env.ExecuteActivity(activities.RecordRunStarted, &RunStartedRequest{
		JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt,
	})
	require.NoError(t, err)

	require.Len(t, client.started, 1)
	sent := client.started[0]
	require.Equal(t, "job-1", sent.GetJobId())
	require.Equal(t, "run-1", sent.GetRunId())
	require.True(t, testStartedAt.Equal(sent.GetStartedAt().AsTime()))
}

func Test_RecordRunEnded_SendsWhatItIsGiven(t *testing.T) {
	for _, tt := range []struct {
		outcome string
		sent    mgmtv1alpha1.RunOutcome
	}{
		{OutcomeCompleted, mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED},
		{OutcomeFailed, mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED},
		{OutcomeCanceled, mgmtv1alpha1.RunOutcome_RUN_OUTCOME_CANCELED},
	} {
		t.Run(tt.outcome, func(t *testing.T) {
			client := &recorder{}
			env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			activities := New(client)
			Register(env, activities)

			_, err := env.ExecuteActivity(activities.RecordRunEnded, &RunEndedRequest{
				JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt, EndedAt: testEndedAt,
				Outcome: tt.outcome, RowsRead: 42, RowsDiscarded: 3, Retries: 2,
				TablesUncounted: 4, SourceVersionMajor: "8.0",
			})
			require.NoError(t, err)

			require.Len(t, client.ended, 1)
			sent := client.ended[0]
			require.Equal(t, "job-1", sent.GetJobId())
			require.Equal(t, "run-1", sent.GetRunId())
			require.True(t, testStartedAt.Equal(sent.GetStartedAt().AsTime()))
			require.True(t, testEndedAt.Equal(sent.GetEndedAt().AsTime()))
			require.Equal(t, tt.sent, sent.GetOutcome())
			require.Equal(t, int64(42), sent.GetRowsRead())
			require.Equal(t, int64(3), sent.GetRowsDiscarded())
			require.Equal(t, int64(2), sent.GetRetries())
			require.Equal(t, int64(4), sent.GetTablesUncounted())
			require.Equal(t, "8.0", sent.GetSourceVersionMajor())
		})
	}
}

// One worker serves every kind of run, and each kind registers the two activities with its
// workflow: a worker refuses a name it already holds unless it is told otherwise, and stops
// the process.
func Test_Register_TwiceOnOneWorker(t *testing.T) {
	temporalclient, err := client.NewLazyClient(client.Options{})
	require.NoError(t, err)
	defer temporalclient.Close()
	w := worker.New(temporalclient, "run-usage-test", worker.Options{})

	require.NotPanics(t, func() {
		Register(w, New(&recorder{}))
		Register(w, New(&recorder{}))
	})
}

// An outcome the API does not know is not sent, and asking again would not change it.
func Test_RecordRunEnded_RefusesAnUnknownOutcome(t *testing.T) {
	client := &recorder{}
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	activities := New(client)
	Register(env, activities)

	_, err := env.ExecuteActivity(activities.RecordRunEnded, &RunEndedRequest{
		JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt, EndedAt: testEndedAt, Outcome: "paused",
	})

	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.True(t, applicationErr.NonRetryable())
	require.Empty(t, client.ended)
}

// A failure of the API is one the activity may be asked again for.
func Test_Activities_ReturnTheFailureOfTheAPI(t *testing.T) {
	client := &recorder{err: connect.NewError(connect.CodeUnavailable, errors.New("the API is away"))}
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
	activities := New(client)
	Register(env, activities)

	_, err := env.ExecuteActivity(activities.RecordRunStarted, &RunStartedRequest{
		JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt,
	})
	require.ErrorContains(t, err, "the API is away")

	_, err = env.ExecuteActivity(activities.RecordRunEnded, &RunEndedRequest{
		JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt, EndedAt: testEndedAt, Outcome: OutcomeFailed,
	})
	require.ErrorContains(t, err, "the API is away")
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.False(t, applicationErr.NonRetryable())
}

// An answer that asking again cannot change is not retried: a new worker told to an API that does
// not have the procedures yet, or a request the API refuses.
func Test_Activities_DoNotRetryWhatTheAPIWillNeverAccept(t *testing.T) {
	for code, retryable := range map[connect.Code]bool{
		connect.CodeUnimplemented:     false,
		connect.CodeInvalidArgument:   false,
		connect.CodePermissionDenied:  false,
		connect.CodeUnavailable:       true,
		connect.CodeDeadlineExceeded:  true,
		connect.CodeInternal:          true,
		connect.CodeResourceExhausted: true,
	} {
		t.Run(code.String(), func(t *testing.T) {
			client := &recorder{err: connect.NewError(code, errors.New("the API answered"))}
			env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()
			activities := New(client)
			Register(env, activities)

			_, err := env.ExecuteActivity(activities.RecordRunStarted, &RunStartedRequest{
				JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt,
			})
			requireRetryable(t, err, retryable)

			_, err = env.ExecuteActivity(activities.RecordRunEnded, &RunEndedRequest{
				JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt, EndedAt: testEndedAt, Outcome: OutcomeFailed,
			})
			requireRetryable(t, err, retryable)
		})
	}
}

func requireRetryable(t *testing.T, err error, retryable bool) {
	t.Helper()
	require.ErrorContains(t, err, "the API answered")
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, err, &applicationErr)
	require.Equal(t, retryable, !applicationErr.NonRetryable())
}

// The serialized form of the requests is in the histories of the runs: a count that is zero
// is left out.
func Test_RunEndedRequest_LeavesOutEmptyCounts(t *testing.T) {
	payload, err := json.Marshal(&RunEndedRequest{
		JobId: "job-1", RunId: "run-1", StartedAt: testStartedAt, EndedAt: testEndedAt, Outcome: OutcomeCompleted,
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"JobId": "job-1", "RunId": "run-1",
		"StartedAt": "2026-10-07T09:00:00Z", "EndedAt": "2026-10-07T09:12:30Z",
		"Outcome": "completed"
	}`, string(payload))
}
