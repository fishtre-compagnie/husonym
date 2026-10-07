package v1alpha1_jobservice

import (
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata/userdatatest"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/workflowservice/v1"
)

const aRunLogsAccountId = "d5ef8fc7-4b2e-4f1f-8c9c-2a2a2a2a2a2a"

// runLogsService is a job service whose logs are configured for Loki without the labels query it
// needs to serve them: a call that gets past every check before the logs are read fails on that,
// and nothing is fetched. The user is let through every check of access, under the license.
func runLogsService(t *testing.T, eelicense license.EEInterface) *Service {
	t.Helper()
	enforcer := userdata.NewMockEntityEnforcer(t)
	enforcer.On("EnforceJob", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).Return(userdatatest.NewUser(t, eelicense, enforcer), nil)

	temporal := clientmanager.NewMockInterface(t)
	temporal.On("DescribeWorklowExecution", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&workflowservice.DescribeWorkflowExecutionResponse{}, nil)

	logType := LokiRunLogType
	return New(
		&Config{RunLogConfig: &RunLogConfig{
			IsEnabled:        true,
			RunLogType:       &logType,
			LokiRunLogConfig: &LokiRunLogConfig{},
		}},
		nil, temporal, nil, nil, nil, users, nil, nil,
	)
}

func getRunLogs(t *testing.T, svc *Service) error {
	t.Helper()
	_, err := svc.GetJobRunLogs(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobRunLogsRequest{
		AccountId: aRunLogsAccountId, JobRunId: "run",
	}))
	return err
}

// The logs of a run are the run_logs feature, asked once the run is known and read. The stream and
// the unary call both go through streamLogs, which the unary call here stands for.
func Test_JobRunLogs_NeedTheRunLogsFeature(t *testing.T) {
	withoutRunLogs := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureRbac))

	err := getRunLogs(t, runLogsService(t, withoutRunLogs))

	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	require.ErrorContains(t, err, "this license does not include run_logs")
}

// Under a license that names no feature list, the call goes past the check: it is the labels query
// the test leaves out that stops it.
func Test_JobRunLogs_AreServedUnderTheDefaultLicense(t *testing.T) {
	err := getRunLogs(t, runLogsService(t, testutil.NewFakeEELicense(testutil.WithIsValid())))

	require.ErrorContains(t, err, "must provide a labels query")
}

// A license that has lapsed, or an instance without one, still allows consulting: the logs of a
// run are refused only by a license in force that does not include them. As above, the call goes
// past the check and is stopped by the labels query the test leaves out.
func Test_JobRunLogs_AreServedWhenNoLicenseIsInForce(t *testing.T) {
	for name, eelicense := range map[string]*testutil.FakeEELicense{
		"a license not in force that named no feature list": testutil.NewFakeEELicense(),
		"a license not in force that did not list run_logs": testutil.NewFakeEELicense(testutil.WithFeatures(license.FeatureRbac)),
	} {
		t.Run(name, func(t *testing.T) {
			err := getRunLogs(t, runLogsService(t, eelicense))

			require.ErrorContains(t, err, "must provide a labels query")
		})
	}
}

// Logs that are not configured are answered as they were, whatever the license includes: the
// answer comes before the user is asked for, so no license is read.
func Test_JobRunLogs_NotConfiguredIsAnsweredFirst(t *testing.T) {
	svc := New(&Config{}, nil, clientmanager.NewMockInterface(t), nil, nil, nil, userdata.NewMockInterface(t), nil, nil)

	err := getRunLogs(t, svc)

	require.Error(t, err)
	require.ErrorContains(t, err, "not enabled")
}

// Closing run_logs closes the logs and nothing else about a run: the run is still read.
func Test_JobRun_IsReadWithoutTheRunLogsFeature(t *testing.T) {
	withoutRunLogs := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureRbac))
	svc := runLogsService(t, withoutRunLogs)

	resp, err := svc.GetJobRun(t.Context(), connect.NewRequest(&mgmtv1alpha1.GetJobRunRequest{
		AccountId: aRunLogsAccountId, JobRunId: "run",
	}))

	require.NoError(t, err)
	require.NotNil(t, resp.Msg.GetJobRun())
}
