package jobhooks_by_timing_activity

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/proto"
)

const licenseNotReceivedMessage = "the worker has not received the instance's license yet: job hooks were not run"

// The refusal tells its category, and is recorded by Temporal as the plain error it was: the
// same message, no type, retried as before, no cause.
func Test_errLicenseNotReceived_IsRecordedAsThePlainErrorItWas(t *testing.T) {
	converter := temporal.GetDefaultFailureConverter()
	before := converter.ErrorToFailure(errors.New(licenseNotReceivedMessage))
	after := converter.ErrorToFailure(errLicenseNotReceived)

	payloads := after.GetApplicationFailureInfo().GetDetails().GetPayloads()
	require.Len(t, payloads, 1)
	require.Equal(t, `{"RunErrorCategory":10}`, string(payloads[0].GetData()))
	after.GetApplicationFailureInfo().Details = nil
	require.True(t, proto.Equal(before, after), "recorded before: %v\nrecorded after: %v", before, after)

	require.Equal(t, licenseNotReceivedMessage, errLicenseNotReceived.Error())
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE, runerror.Classify(errLicenseNotReceived))
}

// A run whose hooks the license refuses fails on an error its workflow reads as a refusal of
// the license.
func Test_Activity_ARefusalOfTheLicenseTellsItsCategory(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	testSuite.SetLogger(log.NewStructuredLogger(testutil.GetConcurrentTestLogger(t)))
	env := testSuite.NewTestActivityEnvironment()

	jobclient := mgmtv1alpha1connect.NewMockJobServiceClient(t)
	jobclient.EXPECT().
		GetActiveJobHooksByTiming(mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.GetActiveJobHooksByTimingResponse{
			Hooks: []*mgmtv1alpha1.JobHook{{Id: "hook-1", Name: "a-hook", Enabled: true}},
		}), nil)
	activity := New(
		jobclient,
		mgmtv1alpha1connect.NewMockConnectionServiceClient(t),
		sqlmanager.NewMockSqlManagerClient(t),
		&fakeELicense{isValid: false},
	)
	env.RegisterActivity(activity)

	_, err := env.ExecuteActivity(activity.RunJobHooksByTiming, &RunJobHooksByTimingRequest{
		JobId: "job-1", Timing: mgmtv1alpha1.GetActiveJobHooksByTimingRequest_TIMING_PRESYNC,
	})

	require.Error(t, err)
	var application *temporal.ApplicationError
	require.ErrorAs(t, err, &application)
	require.Equal(t, licenseNotReceivedMessage, application.Message())
	require.Empty(t, application.Type())
	require.False(t, application.NonRetryable())
	require.Equal(t, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE, runerror.CategoryOf(err))
}
