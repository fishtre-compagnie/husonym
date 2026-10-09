package preflight_activity

import (
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/preflight"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/shared/runerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"
)

const (
	insufficientPrivileges = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES
	objectMissing          = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING
	otherCategory          = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
)

func blockingFinding(kind preflight.Kind, message string) *preflight.Finding {
	return &preflight.Finding{Kind: kind, Level: preflight.Blocking, Message: message}
}

// Every kind of finding is named here with the category a run stopped on it is told with: a
// kind added to the list of the API has to be given one, or "other", here and in the table.
func Test_blockingCategories_EveryKindIsAccountedFor(t *testing.T) {
	expected := map[preflight.Kind]mgmtv1alpha1.RunErrorCategory{
		mgmtv1alpha1.PreflightFinding_KIND_UNSPECIFIED:                 otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_TABLE_EXISTS:                objectMissing,
		mgmtv1alpha1.PreflightFinding_KIND_READABLE:                    insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_SERVER_WRITABLE:             insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_WRITABLE:                    insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_TRUNCATE:                    insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_TRIGGERS:                    insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_TRIGGER_DEFINER:             insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_FOREIGN_KEY_SUSPENSION:      insufficientPrivileges,
		mgmtv1alpha1.PreflightFinding_KIND_ENGINE_UNSUPPORTED:          otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_GENERATED_COLUMN_WRITTEN:    otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_OUTPUT_TOO_LONG:             otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_CONSTANT_ON_UNIQUE:          otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM:          otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE:         otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_DESTINATION_TRIGGERS:        otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_REFERENCE_CLEARED_BY_SUBSET: otherCategory,
		mgmtv1alpha1.PreflightFinding_KIND_TRANSFORMER_DOES_NOT_FIT:    otherCategory,
	}
	for number := range mgmtv1alpha1.PreflightFinding_Kind_name {
		kind := preflight.Kind(number)
		category, known := expected[kind]
		require.True(t, known, "the kind %s is not accounted for", kind)
		assert.Equal(t, category, blockingCategory([]*preflight.Finding{blockingFinding(kind, "m")}), "kind %s", kind)
	}
	// A kind this worker does not know, as an API newer than it may name one.
	assert.Equal(t, otherCategory, blockingCategory([]*preflight.Finding{blockingFinding(preflight.Kind(9999), "m")}))
	assert.Equal(t, otherCategory, blockingCategory(nil))
}

// A run stopped on several findings is told with the category of the first of them, in the
// order of the report, that has one.
func Test_blockingCategory_TheFirstFindingThatHasOneDecides(t *testing.T) {
	engine := blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_ENGINE_UNSUPPORTED, "engine")
	absent := blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_TABLE_EXISTS, "absent")
	refused := blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, "refused")

	assert.Equal(t, objectMissing, blockingCategory([]*preflight.Finding{engine, absent, refused}))
	assert.Equal(t, insufficientPrivileges, blockingCategory([]*preflight.Finding{refused, absent}))
	assert.Equal(t, otherCategory, blockingCategory([]*preflight.Finding{engine}))
}

// The error a run stops on is what it was, the category apart: same message, same type, not
// retried, no cause. The category never comes from the sentence of a finding.
func Test_blockingError(t *testing.T) {
	converter := temporal.GetDefaultFailureConverter()
	asBefore := func(message string) error {
		return temporal.NewNonRetryableApplicationError(message, "PreflightBlocking", nil)
	}

	t.Run("a missing grant", func(t *testing.T) {
		err := blockingError([]*preflight.Finding{
			blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, "destination \"dst\" cannot write public.users"),
			blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_TABLE_EXISTS, "destination \"dst\" has no table public.orders"),
		})
		message := `pre-flight check stopped the run: destination "dst" cannot write public.users; ` +
			`destination "dst" has no table public.orders`

		var application *temporal.ApplicationError
		require.ErrorAs(t, err, &application)
		assert.Equal(t, message, application.Message())
		assert.Equal(t, "PreflightBlocking", application.Type())
		assert.True(t, application.NonRetryable())
		assert.NoError(t, application.Unwrap())
		assert.Equal(t, asBefore(message).Error(), err.Error())
		assert.Equal(t, insufficientPrivileges, runerror.CategoryOf(err))

		before, after := converter.ErrorToFailure(asBefore(message)), converter.ErrorToFailure(err)
		payloads := after.GetApplicationFailureInfo().GetDetails().GetPayloads()
		require.Len(t, payloads, 1)
		assert.Equal(t, `{"RunErrorCategory":5}`, string(payloads[0].GetData()))
		after.GetApplicationFailureInfo().Details = nil
		assert.True(t, proto.Equal(before, after), "recorded before: %v\nrecorded after: %v", before, after)
	})

	t.Run("a finding that has no category tells none", func(t *testing.T) {
		err := blockingError([]*preflight.Finding{
			blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_ENGINE_UNSUPPORTED, "permission denied: table missing"),
		})
		message := "pre-flight check stopped the run: permission denied: table missing"

		assert.Equal(t, otherCategory, runerror.CategoryOf(err))
		_, carried := runerror.Carried(err)
		assert.False(t, carried)
		assert.True(t, proto.Equal(converter.ErrorToFailure(asBefore(message)), converter.ErrorToFailure(err)))
	})
}

// Through the activity: a run whose plan holds a blocking finding fails on the error that
// tells its category, and the interceptor of the worker has nothing to add to it.
func Test_RunPreflight_ABlockingFindingTellsItsCategory(t *testing.T) {
	jobclient := holdsJob(t)
	jobclient.On("SetRunContext", mock.Anything, mock.Anything).
		Return(connect.NewResponse(&mgmtv1alpha1.SetRunContextResponse{}), nil).Once()
	a := &Activity{jobclient: jobclient, sqlconnmanager: &oneDatabase{}, sourceVersionTimeout: sourceVersionTimeout}

	_, err := activityEnvironment(t, a).ExecuteActivity(a.RunPreflight, &RunPreflightRequest{
		JobId: "job",
		Findings: []*preflight.Finding{
			blockingFinding(mgmtv1alpha1.PreflightFinding_KIND_TABLE_EXISTS, "destination \"dst\" has no table public.users"),
		},
	})

	require.Error(t, err)
	var application *temporal.ApplicationError
	require.ErrorAs(t, err, &application)
	assert.Equal(t, "PreflightBlocking", application.Type())
	assert.True(t, application.NonRetryable())
	assert.Equal(t, `pre-flight check stopped the run: destination "dst" has no table public.users`, application.Message())
	assert.Equal(t, objectMissing, runerror.CategoryOf(err))
	assert.Same(t, application, runerror.Carry(application))
}
