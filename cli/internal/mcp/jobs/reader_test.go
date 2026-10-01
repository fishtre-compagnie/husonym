package jobs

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// A method that answers with a run must have its failures emptied at its call site, and a
// method that makes a job run, now or on a schedule, must ask the person first, and a method
// that fails with the error of a database driver must have it replaced. Before adding one,
// make sure its call site does.
func Test_JobClient_MethodsArePinned(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{
		"CreateJob",
		"CreateJobRun",
		"GetJob",
		"GetJobHooks",
		"GetJobRun",
		"GetJobRunEvents",
		"GetJobRuns",
		"GetJobStatus",
		"PreflightJob",
		"UpdateJobSourceConnection",
	}, testutil.MethodNames(reflect.TypeFor[jobClient]()))
}

// The Reader holds its clients through the pinned interface and through nothing else: a second
// client, or a concrete one, would reach methods no test pins.
func Test_Reader_HoldsOnlyPinnedClients(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[Reader]()
	require.Equal(t, []reflect.Type{reflect.TypeFor[jobClient]()}, testutil.InterfaceFields(typ))
	for _, pkg := range testutil.FieldPackages(typ) {
		require.NotContains(t, []string{
			"connectrpc.com/connect",
			"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect",
		}, pkg)
	}
}

// runsClient answers with runs whose failures quote the value they failed on.
type runsClient struct {
	jobClient
}

const quoted = "Duplicate entry 'jean.dupont@example.com' for key 'email'"

func failing(id string) *mgmtv1alpha1.JobRun {
	return &mgmtv1alpha1.JobRun{
		Id: id,
		PendingActivities: []*mgmtv1alpha1.PendingActivity{
			{ActivityName: "sync", LastFailure: &mgmtv1alpha1.ActivityFailure{Message: quoted}},
			{ActivityName: "init"},
		},
	}
}

func (runsClient) GetJobRuns(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetJobRunsRequest],
) (*connect.Response[mgmtv1alpha1.GetJobRunsResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetJobRunsResponse{JobRuns: []*mgmtv1alpha1.JobRun{failing("a")}}), nil
}

func (runsClient) GetJobRun(
	_ context.Context,
	req *connect.Request[mgmtv1alpha1.GetJobRunRequest],
) (*connect.Response[mgmtv1alpha1.GetJobRunResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetJobRunResponse{JobRun: failing(req.Msg.GetJobRunId())}), nil
}

// A run read here says that an activity failed, never what on: the message goes through
// rowvalues, which asks first.
func Test_Reader_RunsComeWithoutFailureMessages(t *testing.T) {
	t.Parallel()
	reader := &Reader{client: runsClient{}}

	runs, err := reader.Runs(t.Context(), "job")
	require.NoError(t, err)
	run, err := reader.GetRun(t.Context(), "b")
	require.NoError(t, err)

	for _, run := range append(runs, run) {
		require.NotNil(t, run.GetPendingActivities()[0].GetLastFailure(), "the failure itself is kept")
		require.Empty(t, run.GetPendingActivities()[0].GetLastFailure().GetMessage())
		require.Nil(t, run.GetPendingActivities()[1].GetLastFailure())
		require.NotContains(t, run.String(), "jean.dupont")
	}
}

// A run removed before it showed never does: past the timeout, it no longer holds the job.
func Test_Reader_StartingIsGivenUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	reader := &Reader{now: func() time.Time { return now }, launched: map[string]launch{
		"job": {runId: "new", at: now},
	}}
	old := []*mgmtv1alpha1.JobRun{{Id: "old"}}

	runId, ok := reader.starting("job", old)
	require.True(t, ok)
	require.Equal(t, "new", runId)
	now = now.Add(launchTimeout + time.Second)
	_, ok = reader.starting("job", old)
	require.False(t, ok)
	require.Empty(t, reader.launched)
}

// Only a refusal for what the caller may do tells that no trigger was sent.
func Test_startedNothing(t *testing.T) {
	t.Parallel()
	for code, nothing := range map[connect.Code]bool{
		connect.CodePermissionDenied:   true,
		connect.CodeUnauthenticated:    true,
		connect.CodeNotFound:           true,
		connect.CodeFailedPrecondition: false,
		connect.CodeInvalidArgument:    false,
		connect.CodeCanceled:           false,
		connect.CodeDeadlineExceeded:   false,
		connect.CodeUnavailable:        false,
		connect.CodeUnknown:            false,
	} {
		require.Equal(t, nothing, startedNothing(connect.NewError(code, errors.New("refused"))), code.String())
	}
	require.False(t, startedNothing(errors.New("no code")))
}

// A job is claimed by one call at a time, and free again once the call lets it go.
func Test_Reader_ClaimIsTakenOnce(t *testing.T) {
	t.Parallel()
	reader := &Reader{claimed: map[string]bool{}}

	release, err := reader.claim("job")
	require.NoError(t, err)
	_, err = reader.claim("job")
	require.ErrorIs(t, err, errClaimed)
	other, err := reader.claim("other")
	require.NoError(t, err)
	other()

	release()
	release()
	_, err = reader.claim("job")
	require.NoError(t, err)
}

// Two calls that found the job idle at once: one holds it, the other does not trigger it.
func Test_Reader_HoldIsTakenOnce(t *testing.T) {
	t.Parallel()
	reader := &Reader{now: time.Now, launched: map[string]launch{}}

	require.True(t, reader.hold("job"))
	require.False(t, reader.hold("job"))
	require.True(t, reader.hold("other"))
	_, ok := reader.starting("job", nil)
	require.True(t, ok, "the job is held before its run is known")
}

// The run started here is told by its id: another run of the job showing does not stand for it.
func Test_Reader_StartingEndsWhenTheRunShows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	reader := &Reader{now: func() time.Time { return now }, launched: map[string]launch{
		"job": {runId: "new", at: now},
	}}

	_, ok := reader.starting("job", []*mgmtv1alpha1.JobRun{{Id: "old"}, {Id: "other"}})
	require.True(t, ok)
	_, ok = reader.starting("job", []*mgmtv1alpha1.JobRun{{Id: "old"}, {Id: "new"}})
	require.False(t, ok)
	require.Empty(t, reader.launched)
	_, ok = reader.starting("other", nil)
	require.False(t, ok)
}

// A driver's error may quote where and how it connects. The API writes its own words under
// some codes; the reason a worker failed, and a call cut short, may carry a driver's instead.
func Test_preflightError(t *testing.T) {
	t.Parallel()
	const driver = "failed to connect to `user=shop database=bench`: dial tcp db.internal:5432: i/o timeout"
	// fromAPI is an error as the client reads it off the wire.
	fromAPI := func(code connect.Code, message string) error {
		return connect.NewWireError(code, errors.New(message))
	}

	for name, tc := range map[string]struct {
		err  error
		kept bool
	}{
		"no worker":                {fromAPI(connect.CodeFailedPrecondition, "no worker serves this account"), true},
		"job not found":            {fromAPI(connect.CodeNotFound, "unable to find job"), true},
		"permission":               {fromAPI(connect.CodePermissionDenied, "missing connection:view_sensitive"), true},
		"the worker failed":        {fromAPI(connect.CodeUnavailable, "the pre-flight check could not end: " + driver), false},
		"cut short at the API":     {fromAPI(connect.CodeDeadlineExceeded, "unable to get job: " + driver), false},
		"cut short here":           {connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded), false},
		"the API out of reach":     {connect.NewError(connect.CodeUnavailable, errors.New("dial tcp api.internal:8080")), false},
		"an error the API did not": {fromAPI(connect.CodeUnknown, driver), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := preflightError(tc.err)
			if tc.kept {
				require.Equal(t, tc.err, got)
				return
			}
			require.NotContains(t, got.Error(), "db.internal")
			require.NotContains(t, got.Error(), "api.internal")
			require.NotContains(t, got.Error(), "user=")
		})
	}
}
