package syncactivityopts_activity

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/datasync/activities/shared"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func Test_New(t *testing.T) {
	a := New(mgmtv1alpha1connect.NewMockJobServiceClient(t))
	require.NotNil(t, a)
}

func Test_Activity(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestActivityEnvironment()

	accountId := uuid.NewString()
	syncActivityJobId := uuid.NewString()
	generatedRequestedJobId := uuid.NewString()
	aiGeneratedRequestedJobId := uuid.NewString()

	mux := http.NewServeMux()
	mux.Handle(mgmtv1alpha1connect.JobServiceGetJobProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.JobServiceGetJobProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetJobRequest]) (*connect.Response[mgmtv1alpha1.GetJobResponse], error) {
			if r.Msg.GetId() == syncActivityJobId {
				return connect.NewResponse(&mgmtv1alpha1.GetJobResponse{
					Job: &mgmtv1alpha1.Job{
						Id:        syncActivityJobId,
						AccountId: accountId,
						SyncOptions: &mgmtv1alpha1.ActivityOptions{
							ScheduleToCloseTimeout: shared.Ptr(int64(1)),
							StartToCloseTimeout:    shared.Ptr(int64(2)),
							RetryPolicy: &mgmtv1alpha1.RetryPolicy{
								MaximumAttempts: shared.Ptr(int32(3)),
							},
						},
						WorkflowOptions: &mgmtv1alpha1.WorkflowOptions{
							RunTimeout: shared.Ptr(int64(4)),
						},
					},
				}), nil
			}
			if r.Msg.GetId() == generatedRequestedJobId {
				return connect.NewResponse(&mgmtv1alpha1.GetJobResponse{
					Job: &mgmtv1alpha1.Job{
						Id:        syncActivityJobId,
						AccountId: accountId,
						Source: &mgmtv1alpha1.JobSource{
							Options: &mgmtv1alpha1.JobSourceOptions{
								Config: &mgmtv1alpha1.JobSourceOptions_Generate{
									Generate: &mgmtv1alpha1.GenerateSourceOptions{
										Schemas: []*mgmtv1alpha1.GenerateSourceSchemaOption{
											{
												Schema: "foo",
												Tables: []*mgmtv1alpha1.GenerateSourceTableOption{
													{
														Table:    "1",
														RowCount: 1,
													},
													{
														Table:    "2",
														RowCount: 2,
													},
												},
											},
											{
												Schema: "bar",
												Tables: []*mgmtv1alpha1.GenerateSourceTableOption{
													{
														Table:    "3",
														RowCount: 3,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}), nil
			}
			if r.Msg.GetId() == aiGeneratedRequestedJobId {
				return connect.NewResponse(&mgmtv1alpha1.GetJobResponse{
					Job: &mgmtv1alpha1.Job{
						Id:        syncActivityJobId,
						AccountId: accountId,
						Source: &mgmtv1alpha1.JobSource{
							Options: &mgmtv1alpha1.JobSourceOptions{
								Config: &mgmtv1alpha1.JobSourceOptions_AiGenerate{
									AiGenerate: &mgmtv1alpha1.AiGenerateSourceOptions{
										Schemas: []*mgmtv1alpha1.AiGenerateSourceSchemaOption{
											{
												Schema: "foo",
												Tables: []*mgmtv1alpha1.AiGenerateSourceTableOption{
													{
														Table:    "1",
														RowCount: 11,
													},
													{
														Table:    "2",
														RowCount: 22,
													},
												},
											},
											{
												Schema: "bar",
												Tables: []*mgmtv1alpha1.AiGenerateSourceTableOption{
													{
														Table:    "3",
														RowCount: 33,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}), nil
			}
			return nil, fmt.Errorf("invalid test job id")
		},
	))
	srv := startHTTPServer(t, mux)

	jobclient := mgmtv1alpha1connect.NewJobServiceClient(srv.Client(), srv.URL)
	activity := New(jobclient)
	env.RegisterActivity(activity.RetrieveActivityOptions)

	t.Run("sync activity options", func(t *testing.T) {
		val, err := env.ExecuteActivity(
			activity.RetrieveActivityOptions,
			&RetrieveActivityOptionsRequest{JobId: syncActivityJobId},
		)
		require.NoError(t, err)
		res := &RetrieveActivityOptionsResponse{}
		err = val.Get(res)
		require.NoError(t, err)
		require.Nil(t, res.RequestedRecordCount)
		require.Equal(t, accountId, res.AccountId)
		require.NotNil(t, res.SyncActivityOptions)
		require.NotNil(t, res.RunTimeout)
		require.Equal(t, time.Duration(4), *res.RunTimeout)
	})
	t.Run("generate requested row count", func(t *testing.T) {
		val, err := env.ExecuteActivity(
			activity.RetrieveActivityOptions,
			&RetrieveActivityOptionsRequest{JobId: generatedRequestedJobId},
		)
		require.NoError(t, err)
		res := &RetrieveActivityOptionsResponse{}
		err = val.Get(res)
		require.NoError(t, err)
		require.Equal(t, accountId, res.AccountId)
		require.NotNil(t, res.RequestedRecordCount)
		require.Equal(t, uint64(6), *res.RequestedRecordCount)
	})
	t.Run("aigenerate requested row count", func(t *testing.T) {
		val, err := env.ExecuteActivity(
			activity.RetrieveActivityOptions,
			&RetrieveActivityOptionsRequest{JobId: aiGeneratedRequestedJobId},
		)
		require.NoError(t, err)
		res := &RetrieveActivityOptionsResponse{}
		err = val.Get(res)
		require.NoError(t, err)
		require.Equal(t, accountId, res.AccountId)
		require.NotNil(t, res.RequestedRecordCount)
		require.Equal(t, uint64(66), *res.RequestedRecordCount)
	})
}

func Test_getSyncActivityOptionsFromJob(t *testing.T) {
	expected := &workflow.ActivityOptions{
		StartToCloseTimeout: 24 * time.Hour,
		HeartbeatTimeout:    1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumAttempts:    3,
		},
	}
	// What a job still carries of the options it once set is not read.
	tests := map[string]*mgmtv1alpha1.Job{
		"no sync options": {},
		"timeouts and attempts of the job": {
			SyncOptions: &mgmtv1alpha1.ActivityOptions{
				StartToCloseTimeout:    shared.Ptr(int64(2)),
				ScheduleToCloseTimeout: shared.Ptr(int64(3)),
				RetryPolicy:            &mgmtv1alpha1.RetryPolicy{MaximumAttempts: shared.Ptr(int32(1))},
			},
		},
	}
	for name, job := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, expected, getSyncActivityOptionsFromJob(job))
		})
	}
}

func Test_getRunTimeout(t *testing.T) {
	assert.Nil(t, getRunTimeout(&mgmtv1alpha1.Job{}))
	assert.Nil(t, getRunTimeout(&mgmtv1alpha1.Job{WorkflowOptions: &mgmtv1alpha1.WorkflowOptions{RunTimeout: shared.Ptr(int64(0))}}))
	got := getRunTimeout(&mgmtv1alpha1.Job{WorkflowOptions: &mgmtv1alpha1.WorkflowOptions{RunTimeout: shared.Ptr(int64(2 * time.Hour))}})
	require.NotNil(t, got)
	assert.Equal(t, 2*time.Hour, *got)
}

func startHTTPServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.Start()
	tb.Cleanup(srv.Close)
	return srv
}

func Test_zeroToNilPointer(t *testing.T) {
	val := uint64(1)

	resp := zeroToNilPointer(uint64(1))
	require.NotNil(t, resp)
	require.Equal(t, val, *resp)

	require.Nil(t, zeroToNilPointer(uint64(0)))
	require.Nil(t, zeroToNilPointer(int64(-1)))
}
