package mcp_cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/require"
)

// silentAPI is an API that never answers: each call is held until its caller gives up.
func silentAPI(t *testing.T) *httptest.Server {
	t.Helper()
	ended := make(chan struct{})
	api := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-ended:
		}
	}))
	// The calls still held are let go before the server waits for them to close.
	t.Cleanup(func() {
		close(ended)
		api.Close()
	})
	return api
}

// inTime fails the test when a call that should be cut short is not.
func inTime(t *testing.T, call func() error) error {
	t.Helper()
	answered := make(chan error, 1)
	go func() { answered <- call() }()
	select {
	case err := <-answered:
		return err
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the call is still waiting for an API that never answers")
		return nil
	}
}

func getJob(ctx context.Context, client mgmtv1alpha1connect.JobServiceClient) error {
	_, err := client.GetJob(ctx, connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: "job-1"}))
	return err
}

// A call the API never answers ends at its limit, and says so in words the agent can act on.
func Test_timeLimits_CutsACallTheAPINeverAnswers(t *testing.T) {
	api := silentAPI(t)
	limits := timeLimits{byDefault: 50 * time.Millisecond}
	client := mgmtv1alpha1connect.NewJobServiceClient(api.Client(), api.URL, connect.WithInterceptors(limits.interceptor()))

	err := inTime(t, func() error { return getJob(context.Background(), client) })

	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	require.ErrorContains(t, err, "the API did not answer within 50ms")
}

// A call that takes longer by nature is given longer, and no other.
func Test_timeLimits_GivesASlowCallItsOwnLimit(t *testing.T) {
	api := silentAPI(t)
	limits := timeLimits{
		byDefault: time.Hour,
		slow:      map[string]time.Duration{mgmtv1alpha1connect.JobServiceGetJobProcedure: 50 * time.Millisecond},
	}
	client := mgmtv1alpha1connect.NewJobServiceClient(api.Client(), api.URL, connect.WithInterceptors(limits.interceptor()))

	err := inTime(t, func() error { return getJob(context.Background(), client) })
	require.ErrorContains(t, err, "the API did not answer within 50ms")

	require.Equal(t, 50*time.Millisecond, limits.of(mgmtv1alpha1connect.JobServiceGetJobProcedure))
	require.Equal(t, time.Hour, limits.of(mgmtv1alpha1connect.JobServiceCreateJobRunProcedure))
}

// A call that comes with a deadline keeps it: the pre-flight check has its own, longer than
// any here.
func Test_timeLimits_KeepsTheDeadlineOfACall(t *testing.T) {
	api := silentAPI(t)
	limits := timeLimits{byDefault: 50 * time.Millisecond}
	client := mgmtv1alpha1connect.NewJobServiceClient(api.Client(), api.URL, connect.WithInterceptors(limits.interceptor()))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := inTime(t, func() error { return getJob(ctx, client) })

	require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	require.GreaterOrEqual(t, time.Since(start), 400*time.Millisecond, "the call was cut at the limit, before its own deadline")
	require.NotContains(t, err.Error(), "the API did not answer within")
}

// A caller that gives up is not told the API did not answer.
func Test_timeLimits_ACallerThatGivesUpIsNotToldOfALimit(t *testing.T) {
	api := silentAPI(t)
	limits := timeLimits{byDefault: time.Hour}
	client := mgmtv1alpha1connect.NewJobServiceClient(api.Client(), api.URL, connect.WithInterceptors(limits.interceptor()))
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	err := inTime(t, func() error { return getJob(ctx, client) })

	require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))
}

// An answer in time is passed on as it is.
func Test_timeLimits_PassesOnAnAnswerInTime(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.NewJobServiceHandler(answeringJobs{}))
	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)
	client := mgmtv1alpha1connect.NewJobServiceClient(
		api.Client(), api.URL, connect.WithInterceptors(apiTimeLimits().interceptor()),
	)

	res, err := client.GetJob(context.Background(), connect.NewRequest(&mgmtv1alpha1.GetJobRequest{Id: "job-1"}))

	require.NoError(t, err)
	require.Equal(t, "job-1", res.Msg.GetJob().GetId())
}

type answeringJobs struct {
	mgmtv1alpha1connect.UnimplementedJobServiceHandler
}

func (answeringJobs) GetJob(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobRequest],
) (*connect.Response[mgmtv1alpha1.GetJobResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetJobResponse{Job: &mgmtv1alpha1.Job{Id: req.Msg.GetId()}}), nil
}

// The limits of the server: the calls that read the database of a connection are given longer
// than those the API answers by itself, and the check of a connection longer than the API
// gives it.
func Test_apiTimeLimits(t *testing.T) {
	limits := apiTimeLimits()

	require.Equal(t, 30*time.Second, limits.of(mgmtv1alpha1connect.JobServiceCreateJobRunProcedure))
	require.Equal(t, 30*time.Second, limits.of(mgmtv1alpha1connect.JobServiceGetJobRunsProcedure))
	require.Equal(t, 2*time.Minute, limits.of(mgmtv1alpha1connect.ConnectionDataServiceGetConnectionSchemaProcedure))
	require.Equal(t, 2*time.Minute, limits.of(mgmtv1alpha1connect.ConnectionDataServiceDetectPiiInConnectionDataProcedure))
	require.Greater(t, limits.of(mgmtv1alpha1connect.ConnectionServiceCheckConnectionConfigByIdProcedure), 2*time.Minute)
}

// Every reader of the server makes its calls with the limits it is given.
func Test_readers_MakeTheirCallsWithinTheLimits(t *testing.T) {
	api := silentAPI(t)
	limits := timeLimits{byDefault: 50 * time.Millisecond}
	options := readers(api.Client(), api.URL, "account-1", connect.WithInterceptors(limits.interceptor()))
	ctx := context.Background()

	calls := map[string]func() error{
		"connections": func() error { _, err := options.Connections.Get(ctx, "connection-1"); return err },
		"data":        func() error { _, err := options.Data.Columns(ctx, "connection-1"); return err },
		"jobs":        func() error { _, err := options.Jobs.Get(ctx, "job-1"); return err },
		// The reader of values reads a run through the reader of jobs first, then by itself.
		"values": func() error { _, _, err := options.Values.Failure(ctx, nil, "run-1"); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := inTime(t, call)
			require.Error(t, err)
		})
	}
}
