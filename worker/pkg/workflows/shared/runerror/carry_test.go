package runerror

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"
)

func Test_Carry_CarriesTheNumberAndNothingElse(t *testing.T) {
	err := fmt.Errorf("writing users: %w", &pgconn.PgError{Code: "23505", Message: "secret-value"})

	converter := temporal.GetDefaultFailureConverter()
	failure := converter.ErrorToFailure(Carry(err))

	payloads := failure.GetApplicationFailureInfo().GetDetails().GetPayloads()
	require.Len(t, payloads, 1)
	assert.Equal(t, `{"RunErrorCategory":4}`, string(payloads[0].GetData()))

	category, ok := Carried(converter.FailureToError(failure))
	assert.True(t, ok)
	assert.Equal(t, constraintViolated, category)

	t.Run("before it is recorded too", func(t *testing.T) {
		category, ok := Carried(Carry(err))
		assert.True(t, ok)
		assert.Equal(t, constraintViolated, category)
	})
}

// What Temporal records of an error, and what a worker reads of it to retry or to cancel, is
// the same with the category as without: the details are all that Carry adds.
func Test_Carry_ChangesNothingButTheDetails(t *testing.T) {
	database := &pgconn.PgError{Code: "42501", Message: "permission denied"}
	cases := []struct {
		name string
		err  error
	}{
		{"a database error", database},
		{"a wrapped database error", fmt.Errorf("syncing: %w", fmt.Errorf("writing users: %w", database))},
		{"a deadline", context.DeadlineExceeded},
		{"a refusal of the license", License(errors.New("no license"))},
		{"a typed error of the worker", &typedError{cause: &mysql.MySQLError{Number: 1062}}},
		{"a join, of which Temporal records no cause", errors.Join(errors.New("first"), database)},
		{
			"a refusal of the license over a timeout of Temporal",
			License(temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, database)),
		},
		{
			"a database error of an activity Temporal canceled, under a wrapper",
			fmt.Errorf("x: %w", errors.Join(database, temporal.NewCanceledError())),
		},
		{
			"a database error over a timeout of Temporal",
			fmt.Errorf("x: %w", errors.Join(database, temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_HEARTBEAT, nil))),
		},
		{"an application error", temporal.NewApplicationErrorWithCause("stopped", "Stopped", database)},
		{"an application error without a type", temporal.NewApplicationErrorWithCause("stopped", "", database)},
		{
			"an application error that is not retried",
			temporal.NewNonRetryableApplicationError("stopped", "PreflightBlocking", &mysql.MySQLError{Number: 1142}),
		},
		{
			"an application error that says when to retry",
			temporal.NewApplicationErrorWithOptions("busy", "Busy", temporal.ApplicationErrorOptions{
				NextRetryDelay: 90 * time.Second,
				Cause:          database,
			}),
		},
		{
			"an application error nobody should be alerted of",
			temporal.NewApplicationErrorWithOptions("busy", "Busy", temporal.ApplicationErrorOptions{
				Category: temporal.ApplicationErrorCategoryBenign,
				Cause:    database,
			}),
		},
		{
			"an application error over a chain of causes",
			temporal.NewApplicationErrorWithCause("stopped", "Stopped",
				fmt.Errorf("first: %w", fmt.Errorf("second: %w", database))),
		},
	}
	converter := temporal.GetDefaultFailureConverter()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			carried := Carry(tc.err)
			_, ok := Carried(carried)
			require.True(t, ok, "the category is carried")

			before := converter.ErrorToFailure(tc.err)
			after := converter.ErrorToFailure(carried)
			require.Len(t, after.GetApplicationFailureInfo().GetDetails().GetPayloads(), 1)
			after.GetApplicationFailureInfo().Details = before.GetApplicationFailureInfo().GetDetails()
			assert.True(t, proto.Equal(before, after), "recorded before: %v\nrecorded after: %v", before, after)

			seenBefore, seenAfter := converter.FailureToError(before), converter.FailureToError(after)
			assert.Equal(t, seenBefore.Error(), seenAfter.Error())

			assert.Equal(t, readByTheWorker(tc.err), readByTheWorker(carried))
			assert.True(t, errors.Unwrap(tc.err) == errors.Unwrap(carried), "the same cause, not a copy of it")
		})
	}
}

// typedError is an error of the worker with a type of its own, as Temporal names it.
type typedError struct{ cause error }

func (e *typedError) Error() string { return "typed" }
func (e *typedError) Unwrap() error { return e.cause }

// workerReading is what the worker of Temporal reads of the error of an activity, beyond
// what it records: whether it is a cancellation, whether a local activity is retried, and
// when.
type workerReading struct {
	contextCanceled bool
	canceled        bool
	terminated      bool
	timedOut        bool
	nonRetryable    bool
	errorType       string
	nextRetryDelay  time.Duration
	benign          bool
}

func readByTheWorker(err error) workerReading {
	var (
		canceledErr   *temporal.CanceledError
		terminatedErr *temporal.TerminatedError
		timeoutErr    *temporal.TimeoutError
		application   *temporal.ApplicationError
	)
	reading := workerReading{
		contextCanceled: errors.Is(err, context.Canceled),
		canceled:        errors.As(err, &canceledErr),
		terminated:      errors.As(err, &terminatedErr),
		timedOut:        errors.As(err, &timeoutErr),
	}
	if errors.As(err, &application) {
		reading.nonRetryable = application.NonRetryable()
		reading.errorType = application.Type()
	} else {
		info := temporal.GetDefaultFailureConverter().ErrorToFailure(err).GetApplicationFailureInfo()
		reading.errorType = info.GetType()
	}
	if top, ok := err.(*temporal.ApplicationError); ok {
		reading.nextRetryDelay = top.NextRetryDelay()
		reading.benign = top.Category() == temporal.ApplicationErrorCategoryBenign
	}
	return reading
}

func Test_Carry_LeavesWhatItHasNothingToSayOf(t *testing.T) {
	database := &pgconn.PgError{Code: "23505"}
	timedOut := temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, database)
	cases := []struct {
		name string
		err  error
	}{
		{"a plain error", errors.New("x")},
		{"a database error whose code is not listed", &pgconn.PgError{Code: "42601"}},
		{"a cancellation", context.Canceled},
		{"a cancellation of Temporal", temporal.NewCanceledError()},
		{"a database error of a canceled activity", fmt.Errorf("x: %w", errors.Join(context.Canceled, database))},
		{"an application error with details", temporal.NewApplicationErrorWithCause("m", "T", database, "a detail")},
		// What Temporal does not record as an application failure keeps its kind.
		{"a timeout of Temporal", timedOut},
		// What cannot be rebuilt without changing what the worker reads of it.
		{"a cancellation of Temporal in a join at the top", errors.Join(database, temporal.NewCanceledError())},
		{
			"an application error that is not retried, under a wrapper",
			fmt.Errorf("x: %w", temporal.NewNonRetryableApplicationError("stopped", "T", database)),
		},
		{
			"an application error with a type, under a wrapper",
			fmt.Errorf("x: %w", temporal.NewApplicationErrorWithCause("stopped", "T", database)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Same(t, tc.err, Carry(tc.err))
		})
	}
	assert.NoError(t, Carry(nil))

	t.Run("an error that came back from a failure with details", func(t *testing.T) {
		converter := temporal.GetDefaultFailureConverter()
		seen := converter.FailureToError(converter.ErrorToFailure(Carry(fmt.Errorf("x: %w", database))))
		assert.Same(t, seen, Carry(seen))
	})
}

func Test_Carried_FindsTheCategoryUnderEveryWrapper(t *testing.T) {
	carried := Carry(fmt.Errorf("x: %w", &pgconn.PgError{Code: "23505"}))
	converter := temporal.GetDefaultFailureConverter()
	roundTrip := func(err error) error {
		return converter.FailureToError(converter.ErrorToFailure(err))
	}

	found := map[string]error{
		"itself":                                  carried,
		"under a wrapper":                         fmt.Errorf("table users: %w", carried),
		"under a join":                            errors.Join(errors.New("other"), carried),
		"under a wrapper of a join":               fmt.Errorf("x: %w", errors.Join(errors.New("other"), fmt.Errorf("y: %w", carried))),
		"after a round trip":                      roundTrip(carried),
		"after a round trip under a wrapper":      roundTrip(fmt.Errorf("table users: %w", carried)),
		"after a round trip as an activity error": roundTrip(temporal.NewApplicationErrorWithCause("activity", "T", carried)),
		"behind an application error with other details": temporal.NewApplicationErrorWithCause(
			"m", "T", carried, "a detail"),
	}
	for name, err := range found {
		t.Run(name, func(t *testing.T) {
			category, ok := Carried(err)
			assert.True(t, ok)
			assert.Equal(t, constraintViolated, category)
		})
	}

	notFound := map[string]error{
		"nothing":                         nil,
		"a plain error":                   errors.New("x"),
		"a database error":                &pgconn.PgError{Code: "23505"},
		"details that are a text":         temporal.NewApplicationError("m", "T", "a detail"),
		"details that are a text, back":   roundTrip(temporal.NewApplicationError("m", "T", "RunErrorCategory")),
		"details that are a number":       roundTrip(temporal.NewApplicationError("m", "T", 4)),
		"details of something else":       roundTrip(temporal.NewApplicationError("m", "T", map[string]string{"Reason": "x"})),
		"details that tell no category":   roundTrip(temporal.NewApplicationError("m", "T", carriedDetails{})),
		"an application error without":    temporal.NewApplicationError("m", "T"),
		"a failure without details, back": roundTrip(errors.New("x")),
	}
	for name, err := range notFound {
		t.Run(name, func(t *testing.T) {
			category, ok := Carried(err)
			assert.False(t, ok)
			assert.Equal(t, unspecified, category)
		})
	}

	t.Run("the first one found is the one told", func(t *testing.T) {
		later := Carry(fmt.Errorf("x: %w", context.DeadlineExceeded))
		category, ok := Carried(errors.Join(carried, later))
		assert.True(t, ok)
		assert.Equal(t, constraintViolated, category)
	})
}

// The failure a carried error is recorded as holds no text the error did not already have.
func Test_Carry_AddsNoTextToTheFailure(t *testing.T) {
	err := fmt.Errorf("writing users: %w", &pgconn.PgError{Code: "23505", Message: "secret-value"})
	converter := temporal.GetDefaultFailureConverter()

	var texts func(failure *failurepb.Failure) []string
	texts = func(failure *failurepb.Failure) []string {
		if failure == nil {
			return nil
		}
		return append([]string{failure.GetMessage(), failure.GetStackTrace()}, texts(failure.GetCause())...)
	}
	assert.Equal(t, texts(converter.ErrorToFailure(err)), texts(converter.ErrorToFailure(Carry(err))))
}
