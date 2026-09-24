package jobs

import (
	"context"
	"reflect"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// A method that answers with a run must have its failures emptied at its call site, and a
// method that makes a job run, now or on a schedule, must ask the person first. Before adding
// one, make sure its call site does.
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

// A trigger the scheduler skipped never shows: past the timeout, it no longer holds the job.
func Test_Reader_StartingIsGivenUp(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	reader := &Reader{now: func() time.Time { return now }, launched: map[string]launch{
		"job": {before: map[string]bool{"old": true}, at: now},
	}}
	old := []*mgmtv1alpha1.JobRun{{Id: "old"}}

	require.True(t, reader.Starting("job", old))
	now = now.Add(launchTimeout + time.Second)
	require.False(t, reader.Starting("job", old))
	require.Empty(t, reader.launched)
}

// The run started here is the first one the job did not have before.
func Test_Reader_StartingEndsWhenTheRunShows(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	reader := &Reader{now: func() time.Time { return now }, launched: map[string]launch{
		"job": {before: map[string]bool{"old": true}, at: now},
	}}

	require.False(t, reader.Starting("job", []*mgmtv1alpha1.JobRun{{Id: "old"}, {Id: "new"}}))
	require.False(t, reader.Starting("other", nil))
}
