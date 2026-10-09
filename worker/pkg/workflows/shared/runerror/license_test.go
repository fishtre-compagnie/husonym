package runerror

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"
)

// A refusal of the license is recorded by Temporal as the error it marks would have been: the
// same message, type, retryability and causes. The details are all the mark adds.
func Test_License_IsRecordedAsTheErrorItMarks(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"a plain error", errors.New("the worker has not received the license")},
		{"a wrapped error", fmt.Errorf("halting: %w", errors.New("no license"))},
		{"a typed error of the worker", &typedError{cause: &mysql.MySQLError{Number: 1062}}},
		{"a database error", &pgconn.PgError{Code: "23505"}},
		{"an application error that is not retried", temporal.NewNonRetryableApplicationError("refused", "Refused", nil)},
	}
	converter := temporal.GetDefaultFailureConverter()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			marked := License(tc.err)

			before := converter.ErrorToFailure(tc.err)
			after := converter.ErrorToFailure(marked)
			payloads := after.GetApplicationFailureInfo().GetDetails().GetPayloads()
			require.Len(t, payloads, 1)
			assert.Equal(t, `{"RunErrorCategory":10}`, string(payloads[0].GetData()))
			after.GetApplicationFailureInfo().Details = before.GetApplicationFailureInfo().GetDetails()
			assert.True(t, proto.Equal(before, after), "recorded before: %v\nrecorded after: %v", before, after)

			assert.Equal(t, converter.FailureToError(before).Error(), converter.FailureToError(converter.ErrorToFailure(marked)).Error())
			assert.Equal(t, readByTheWorker(tc.err), readByTheWorker(marked))
			assert.True(t, errors.Unwrap(tc.err) == errors.Unwrap(marked), "the same cause, not a copy of it")
		})
	}
}

// The mark is read where the error is, under a wrapper, and once Temporal recorded it: in the
// worker by Classify, in a workflow by CategoryOf.
func Test_License_IsReadWhereverTheErrorIs(t *testing.T) {
	marked := License(errors.New("no license"))
	converter := temporal.GetDefaultFailureConverter()
	roundTrip := func(err error) error {
		return converter.FailureToError(converter.ErrorToFailure(err))
	}

	for name, err := range map[string]error{
		"itself":                             marked,
		"under a wrapper":                    fmt.Errorf("x: %w", marked),
		"under a join":                       errors.Join(errors.New("other"), marked),
		"after a round trip":                 roundTrip(marked),
		"after a round trip under a wrapper": roundTrip(fmt.Errorf("x: %w", marked)),
		"as the failure of an activity":      seenByAWorkflow(marked),
	} {
		t.Run(name, func(t *testing.T) {
			category, ok := Carried(err)
			assert.True(t, ok)
			assert.Equal(t, license, category)
			assert.Equal(t, license, Classify(err))
			assert.Equal(t, license, CategoryOf(err))
		})
	}

	t.Run("the marked value is the one to compare with", func(t *testing.T) {
		assert.ErrorIs(t, fmt.Errorf("x: %w", marked), marked)
	})

	// A refusal is a plain error with a text of its own: the worker logs it as it did.
	t.Run("a plain error reads the same once marked", func(t *testing.T) {
		assert.Equal(t, "no license", marked.Error())
		assert.Equal(t, "x: no license", fmt.Errorf("x: %w", marked).Error())
	})

	t.Run("another category is not the license", func(t *testing.T) {
		carried := Carry(fmt.Errorf("x: %w", &pgconn.PgError{Code: "42501"}))
		assert.Equal(t, insufficientPrivileges, CategoryOf(carried))
		assert.NotEqual(t, license, Classify(carried))
	})
}

// The interceptor has nothing to add to a marked error: it is the very error that leaves the
// activity.
func Test_License_IsLeftByCarry(t *testing.T) {
	marked := License(errors.New("no license"))
	assert.Same(t, marked, Carry(marked))
}

// What Temporal does not record as an application failure cannot bear the mark without
// becoming another failure: it is left as it is, and so is an error that already tells
// something of its own.
func Test_License_LeavesWhatItCannotMarkUnseen(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"a cancellation", context.Canceled},
		{"a cancellation of Temporal", temporal.NewCanceledError()},
		{"a timeout of Temporal", temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)},
		{"an application error with details", temporal.NewApplicationError("m", "T", "a detail")},
		{"an error that cannot be asked for its text", &mute{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
				assert.Same(t, tc.err, License(tc.err))
			})
		})
	}
	assert.NoError(t, License(nil))
}

// A workflow sees the same failure of an activity that ends on a refusal, under the
// interceptor and without it, the category included: the mark is the error's own.
func Test_License_ReachesTheWorkflowTheSameWithAndWithoutTheInterceptor(t *testing.T) {
	bare := errors.New("no license")
	marked := License(bare)

	unmarked := failureSeenByWorkflow(t, bare, false)
	without := failureSeenByWorkflow(t, marked, false)
	with := failureSeenByWorkflow(t, marked, true)

	for _, seen := range []error{without, with} {
		var before, after *temporal.ApplicationError
		require.ErrorAs(t, unmarked, &before)
		require.ErrorAs(t, seen, &after)
		assert.Equal(t, before.Type(), after.Type())
		assert.Equal(t, before.Message(), after.Message())
		assert.Equal(t, before.NonRetryable(), after.NonRetryable())
		assert.Equal(t, unmarked.Error(), seen.Error())
		assert.Equal(t, causes(unmarked), causes(seen))

		var activityBefore, activityAfter *temporal.ActivityError
		require.ErrorAs(t, unmarked, &activityBefore)
		require.ErrorAs(t, seen, &activityAfter)
		assert.Equal(t, activityBefore.RetryState(), activityAfter.RetryState())

		assert.Equal(t, license, CategoryOf(seen))
	}
	assert.Equal(t, other, CategoryOf(unmarked))
}

// Tell is what Carry and License stand on: the category is the one given, and nothing is told
// of an error with a category that tells nothing.
func Test_Tell(t *testing.T) {
	err := temporal.NewNonRetryableApplicationError("stopped", "PreflightBlocking", nil)

	told := Tell(err, objectMissing)
	category, ok := Carried(told)
	assert.True(t, ok)
	assert.Equal(t, objectMissing, category)

	var application *temporal.ApplicationError
	require.ErrorAs(t, told, &application)
	assert.Equal(t, "PreflightBlocking", application.Type())
	assert.True(t, application.NonRetryable())
	assert.Equal(t, err.Error(), told.Error())

	assert.Same(t, err, Tell(err, other))
	assert.Same(t, err, Tell(err, unspecified))
	assert.Same(t, err, Tell(err, canceled))
	assert.NoError(t, Tell(nil, objectMissing))
}
