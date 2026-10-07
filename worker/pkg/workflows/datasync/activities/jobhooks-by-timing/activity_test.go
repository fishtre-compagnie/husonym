package jobhooks_by_timing_activity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/testsuite"
)

type fakeELicense struct {
	isValid bool
}

func (f *fakeELicense) IsValid() bool {
	return f.isValid
}

func (f *fakeELicense) Limits() *license.Limits {
	return nil
}

func Test_New(t *testing.T) {
	a := New(
		mgmtv1alpha1connect.NewMockJobServiceClient(t),
		mgmtv1alpha1connect.NewMockConnectionServiceClient(t),
		sqlmanager.NewMockSqlManagerClient(t),
		&fakeELicense{isValid: true},
	)
	require.NotNil(t, a)
}

// A job without an active hook for the timing has nothing to run, whatever the license says.
func Test_Activity_NotLicensed_NoActiveHook(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestActivityEnvironment()

	jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
	jobclient.EXPECT().
		GetActiveJobHooksByTiming(mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.GetActiveJobHooksByTimingResponse{}), nil).
		Once()
	activity := New(
		jobclient,
		mgmtv1alpha1connect.NewMockConnectionServiceClient(t),
		sqlmanager.NewMockSqlManagerClient(t),
		&fakeELicense{isValid: false},
	)
	env.RegisterActivity(activity)

	val, err := env.ExecuteActivity(activity.RunJobHooksByTiming, &RunJobHooksByTimingRequest{
		JobId:  uuid.NewString(),
		Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
	})
	require.NoError(t, err)
	res := &RunJobHooksByTimingResponse{}
	err = val.Get(res)
	require.NoError(t, err)
	require.Equal(t, uint(0), res.ExecCount)
}

// The run tells the activity the license answer it started with, and that answer decides —
// not what the license says by the time the hooks are due. A request without it comes from
// a run started before the answer was passed along: the license is then read as before.
//
// The API does not start a run under a license that is not in force, so a run that reaches
// its hooks "not licensed" is one whose worker had not received the license: the hooks are
// not run, and the activity fails rather than let the run pass for whole.
func Test_Activity_FollowsTheLicenseAnswerOfTheRun(t *testing.T) {
	yes, no := true, false

	tests := []struct {
		name         string
		licensed     *bool
		licenseValid bool
		expectRun    bool
	}{
		{name: "the run was licensed and the license lapsed since", licensed: &yes, licenseValid: false, expectRun: true},
		{name: "the run was not licensed and the license is valid now", licensed: &no, licenseValid: true, expectRun: false},
		{name: "no answer from the run, a valid license", licensed: nil, licenseValid: true, expectRun: true},
		{name: "no answer from the run, no valid license", licensed: nil, licenseValid: false, expectRun: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSuite := &testsuite.WorkflowTestSuite{}
			testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
			env := testSuite.NewTestActivityEnvironment()

			jobId := uuid.NewString()
			connId := uuid.NewString()

			// Without an expectation, a mock fails the test on any call: the hooks are looked
			// up either way, and a hook that is not run opens no connection.
			jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
			connclient := mgmtv1alpha1connect.NewMockConnectionServiceClient(t)
			sqlMgrClient := sqlmanager.NewMockSqlManagerClient(t)
			jobclient.EXPECT().
				GetActiveJobHooksByTiming(mock.Anything, mock.Anything).
				Return(connect.NewResponse(&mgmtv1alpha1.GetActiveJobHooksByTimingResponse{
					Hooks: []*mgmtv1alpha1.JobHook{{
						Id:      uuid.NewString(),
						Name:    "restore-constraints",
						JobId:   jobId,
						Enabled: true,
						Config: &mgmtv1alpha1.JobHookConfig{
							Config: &mgmtv1alpha1.JobHookConfig_Sql{
								Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
									Query:        "alter table public.users enable trigger all",
									ConnectionId: connId,
									Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
										Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PostSync{},
									},
								},
							},
						},
					}},
				}), nil).
				Once()
			if tt.expectRun {
				connclient.EXPECT().
					GetConnection(mock.Anything, mock.Anything).
					Return(connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{
						Connection: &mgmtv1alpha1.Connection{Id: connId},
					}), nil).
					Once()
				sqlDb := sqlmanager.NewMockSqlDatabase(t)
				sqlMgrClient.EXPECT().
					NewSqlConnection(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(sqlmanager.NewPostgresSqlConnection(sqlDb), nil).
					Once()
				sqlDb.EXPECT().Exec(mock.Anything, mock.Anything).Return(nil).Once()
				sqlDb.EXPECT().Close().Return().Once()
			}

			activity := New(jobclient, connclient, sqlMgrClient, &fakeELicense{isValid: tt.licenseValid})
			env.RegisterActivity(activity)

			val, err := env.ExecuteActivity(activity.RunJobHooksByTiming, &RunJobHooksByTimingRequest{
				JobId:    jobId,
				Timing:   mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_POSTSYNC,
				Licensed: tt.licensed,
			})
			if !tt.expectRun {
				require.ErrorContains(t, err,
					"the worker has not received the instance's license yet: job hooks were not run")
				return
			}
			require.NoError(t, err)
			res := &RunJobHooksByTimingResponse{}
			require.NoError(t, val.Get(res))
			require.Equal(t, uint(1), res.ExecCount)
		})
	}
}

func Test_Activity_Success(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestActivityEnvironment()

	jobId := uuid.NewString()
	connId := uuid.NewString()

	mux := http.NewServeMux()
	mux.Handle(
		mgmtv1alpha1connect.JobServiceGetActiveJobHooksByTimingProcedure,
		connect.NewUnaryHandler(
			mgmtv1alpha1connect.JobServiceGetActiveJobHooksByTimingProcedure,
			func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetActiveJobHooksByTimingRequest]) (*connect.Response[mgmtv1alpha1.GetActiveJobHooksByTimingResponse], error) {
				if r.Msg.GetJobId() == jobId &&
					r.Msg.Timing == mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC {
					return connect.NewResponse(&mgmtv1alpha1.GetActiveJobHooksByTimingResponse{
						Hooks: []*mgmtv1alpha1.JobHook{
							{
								Id:       uuid.NewString(),
								Name:     "test-1",
								JobId:    jobId,
								Enabled:  true,
								Priority: 0,
								Config: &mgmtv1alpha1.JobHookConfig{
									Config: &mgmtv1alpha1.JobHookConfig_Sql{
										Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
											Query:        "truncate table public.users",
											ConnectionId: connId,
											Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
												Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
											},
										},
									},
								},
							},
							{
								Id:       uuid.NewString(),
								Name:     "test-2",
								JobId:    jobId,
								Enabled:  true,
								Priority: 0,
								Config: &mgmtv1alpha1.JobHookConfig{
									Config: &mgmtv1alpha1.JobHookConfig_Sql{
										Sql: &mgmtv1alpha1.JobHookConfig_JobSqlHook{
											Query:        "truncate table public.pets",
											ConnectionId: connId,
											Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing{
												Timing: &mgmtv1alpha1.JobHookConfig_JobSqlHook_Timing_PreSync{},
											},
										},
									},
								},
							},
						},
					}), nil
				}
				return nil, connect.NewError(connect.CodeNotFound, errors.New("invalid test input"))
			},
		),
	)
	mux.Handle(mgmtv1alpha1connect.ConnectionServiceGetConnectionProcedure, connect.NewUnaryHandler(
		mgmtv1alpha1connect.ConnectionServiceGetConnectionProcedure,
		func(ctx context.Context, r *connect.Request[mgmtv1alpha1.GetConnectionRequest]) (*connect.Response[mgmtv1alpha1.GetConnectionResponse], error) {
			if r.Msg.GetId() == connId {
				return connect.NewResponse(&mgmtv1alpha1.GetConnectionResponse{
					Connection: &mgmtv1alpha1.Connection{
						Id: connId,
						// leaving remaining impl out as it's not needed for this test due to mocking
					},
				}), nil
			}
			return nil, connect.NewError(connect.CodeNotFound, errors.New("invalid test input"))
		},
	))
	srv := startHTTPServer(t, mux)
	jobclient := mgmtv1alpha1connect.NewJobServiceClient(srv.Client(), srv.URL)
	connclient := mgmtv1alpha1connect.NewConnectionServiceClient(srv.Client(), srv.URL)
	mockSqlMgrClient := sqlmanager.NewMockSqlManagerClient(t)
	mockSqlDb := sqlmanager.NewMockSqlDatabase(t)

	mockSqlMgrClient.On("NewSqlConnection", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Once().
		Return(
			sqlmanager.NewPostgresSqlConnection(mockSqlDb), nil,
		)
	mockSqlDb.On("Exec", mock.Anything, mock.Anything).Twice().Return(nil)
	mockSqlDb.On("Close").Once().Return(nil)

	activity := New(jobclient, connclient, mockSqlMgrClient, &fakeELicense{isValid: true})
	env.RegisterActivity(activity)

	val, err := env.ExecuteActivity(activity.RunJobHooksByTiming, &RunJobHooksByTimingRequest{
		JobId:  jobId,
		Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
	})
	require.NoError(t, err)
	res := &RunJobHooksByTimingResponse{}
	err = val.Get(res)
	require.NoError(t, err)
	require.Equal(t, uint(2), res.ExecCount)
}

func startHTTPServer(tb testing.TB, h http.Handler) *httptest.Server {
	tb.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.EnableHTTP2 = true
	srv.Start()
	tb.Cleanup(srv.Close)
	return srv
}
