package runerror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const testActivityName = "an-activity"

// runsTheActivity is a workflow that runs the activity of the test, as a local activity or
// not, and ends on what it gave.
func runsTheActivity(attempts int32, local bool) func(ctx workflow.Context) (string, error) {
	return func(ctx workflow.Context) (string, error) {
		retries := &temporal.RetryPolicy{
			MaximumAttempts:    attempts,
			InitialInterval:    time.Millisecond,
			BackoffCoefficient: 1,
		}
		var result string
		if local {
			ctx = workflow.WithLocalActivityOptions(ctx, workflow.LocalActivityOptions{
				StartToCloseTimeout: time.Minute,
				RetryPolicy:         retries,
			})
			err := workflow.ExecuteLocalActivity(ctx, testActivityName).Get(ctx, &result)
			return result, err
		}
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: time.Minute,
			RetryPolicy:         retries,
		})
		err := workflow.ExecuteActivity(ctx, testActivityName).Get(ctx, &result)
		return result, err
	}
}

// quietSuite is a test suite of Temporal that logs nothing: these tests fail activities on
// purpose.
func quietSuite() *testsuite.WorkflowTestSuite {
	suite := &testsuite.WorkflowTestSuite{}
	suite.SetLogger(log.NewStructuredLogger(slog.New(slog.DiscardHandler)))
	return suite
}

// testEnvironment is a test environment of Temporal, under the interceptor or not.
func testEnvironment(intercepted bool) *testsuite.TestWorkflowEnvironment {
	env := quietSuite().NewTestWorkflowEnvironment()
	if intercepted {
		env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{NewInterceptor()}})
	}
	return env
}

// outcome is what a workflow saw of the activity it ran.
type outcome struct {
	result   string
	err      error
	attempts int
}

// seenByWorkflow runs, in a test environment of Temporal, a workflow over a registered
// activity that does what fn does, and gives what the workflow ended on.
func seenByWorkflow(t *testing.T, fn func() (string, error), intercepted bool, attempts int32) outcome {
	t.Helper()
	env := testEnvironment(intercepted)
	var calls atomic.Int32
	env.RegisterActivityWithOptions(func(context.Context) (string, error) {
		calls.Add(1)
		return fn()
	}, activity.RegisterOptions{Name: testActivityName})

	env.ExecuteWorkflow(runsTheActivity(attempts, false))
	require.True(t, env.IsWorkflowCompleted())

	seen := outcome{err: env.GetWorkflowError(), attempts: int(calls.Load())}
	if seen.err == nil {
		require.NoError(t, env.GetWorkflowResult(&seen.result))
	}
	return seen
}

// failureSeenByWorkflow gives the error a workflow sees of an activity that fails with
// activityErr at its one attempt.
func failureSeenByWorkflow(t *testing.T, activityErr error, intercepted bool) error {
	t.Helper()
	seen := seenByWorkflow(t, func() (string, error) { return "", activityErr }, intercepted, 1)
	require.Error(t, seen.err)
	require.Equal(t, 1, seen.attempts)
	return seen.err
}

// What the rest of this file stands on: the test environment of Temporal runs the
// interceptors of the worker options around a registered activity.
func Test_TheTestEnvironment_RunsTheInterceptorsOfTheWorker(t *testing.T) {
	probe := &probeInterceptor{}
	env := quietSuite().NewTestWorkflowEnvironment()
	env.SetWorkerOptions(worker.Options{Interceptors: []interceptor.WorkerInterceptor{probe}})
	env.RegisterActivityWithOptions(func(context.Context) (string, error) {
		return "done", nil
	}, activity.RegisterOptions{Name: testActivityName})

	env.ExecuteWorkflow(runsTheActivity(1, false))

	require.NoError(t, env.GetWorkflowError())
	assert.Equal(t, int32(1), probe.activities.Load())
}

type probeInterceptor struct {
	interceptor.WorkerInterceptorBase
	activities atomic.Int32
}

func (p *probeInterceptor) InterceptActivity(
	ctx context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	return &probeActivity{ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next}, probe: p}
}

type probeActivity struct {
	interceptor.ActivityInboundInterceptorBase
	probe *probeInterceptor
}

func (p *probeActivity) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (any, error) {
	p.probe.activities.Add(1)
	return p.Next.ExecuteActivity(ctx, in)
}

// What the tests of the workflows stand on: the details of the error a mocked activity
// answers reach the workflow, as those of a real activity do.
func Test_TheTestEnvironment_GivesTheWorkflowTheDetailsOfAMock(t *testing.T) {
	answer := Carry(fmt.Errorf("x: %w", &pgconn.PgError{Code: "42501"}))

	for _, intercepted := range []bool{false, true} {
		t.Run(fmt.Sprintf("intercepted=%t", intercepted), func(t *testing.T) {
			env := testEnvironment(intercepted)
			env.RegisterActivityWithOptions(func(context.Context) (string, error) {
				return "", errors.New("the activity itself is not run")
			}, activity.RegisterOptions{Name: testActivityName})
			env.OnActivity(testActivityName, mock.Anything).Return("", answer)

			env.ExecuteWorkflow(runsTheActivity(1, false))

			seen := env.GetWorkflowError()
			require.Error(t, seen)
			category, ok := Carried(seen)
			assert.True(t, ok)
			assert.Equal(t, insufficientPrivileges, category)
			assert.Equal(t, insufficientPrivileges, CategoryOf(seen))
		})
	}
}

func Test_Interceptor_ChangesNothingButTheDetails(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected mgmtv1alpha1.RunErrorCategory
	}{
		{
			"a wrapped database error",
			fmt.Errorf("syncing: %w", fmt.Errorf("writing users: %w", &pgconn.PgError{Code: "42501"})),
			insufficientPrivileges,
		},
		{
			"an application error that is not retried",
			temporal.NewNonRetryableApplicationError("stopped", "PreflightBlocking", &mysql.MySQLError{Number: 1142}),
			insufficientPrivileges,
		},
		{"a deadline", context.DeadlineExceeded, timeout},
		{"a refusal of the license", License(errors.New("no license")), license},
		{"a typed error of the worker", &typedError{cause: &mysql.MySQLError{Number: 1062}}, constraintViolated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			without := failureSeenByWorkflow(t, tc.err, false)
			with := failureSeenByWorkflow(t, tc.err, true)

			var before, after *temporal.ApplicationError
			require.ErrorAs(t, without, &before)
			require.ErrorAs(t, with, &after)
			assert.Equal(t, before.Type(), after.Type())
			assert.Equal(t, before.Message(), after.Message())
			assert.Equal(t, before.NonRetryable(), after.NonRetryable())
			assert.Equal(t, before.NextRetryDelay(), after.NextRetryDelay())
			assert.Equal(t, before.Category(), after.Category())
			assert.Equal(t, before.Error(), after.Error())
			assert.Equal(t, without.Error(), with.Error())
			assert.Equal(t, causes(without), causes(with))

			var activityBefore, activityAfter *temporal.ActivityError
			require.ErrorAs(t, without, &activityBefore)
			require.ErrorAs(t, with, &activityAfter)
			assert.Equal(t, activityBefore.RetryState(), activityAfter.RetryState())

			category, ok := Carried(with)
			assert.True(t, ok)
			assert.Equal(t, tc.expected, category)
			assert.Equal(t, tc.expected, CategoryOf(with))

			category, ok = Carried(without)
			assert.False(t, ok)
			assert.Equal(t, unspecified, category)
		})
	}
}

// causes gives the chain of an error as a workflow can read it: the Go type and the text
// of each error under it.
func causes(err error) []string {
	chain := []string{}
	for ; err != nil; err = errors.Unwrap(err) {
		chain = append(chain, fmt.Sprintf("%T: %s", err, err.Error()))
	}
	return chain
}

func Test_Interceptor_LeavesAnErrorItDoesNotKnow(t *testing.T) {
	for _, activityErr := range []error{
		errors.New("x"),
		&pgconn.PgError{Code: "42601", Message: "syntax error"},
		temporal.NewApplicationError("stopped", "Stopped", "a detail"),
	} {
		without := failureSeenByWorkflow(t, activityErr, false)
		with := failureSeenByWorkflow(t, activityErr, true)

		assert.Equal(t, without.Error(), with.Error())
		assert.Equal(t, causes(without), causes(with))
		_, ok := Carried(with)
		assert.False(t, ok)
		assert.Equal(t, other, CategoryOf(with))
	}
}

// An activity is tried again as often with the interceptor as without, and a local activity
// too, which the worker itself decides to retry from the error it holds.
func Test_Interceptor_RetriesWhatWasRetriedAndNothingElse(t *testing.T) {
	database := &pgconn.PgError{Code: "23505"}
	cases := []struct {
		name     string
		err      error
		attempts int
	}{
		{"a database error", fmt.Errorf("x: %w", database), 3},
		{"a deadline", context.DeadlineExceeded, 3},
		{"an application error", temporal.NewApplicationErrorWithCause("stopped", "Stopped", database), 3},
		{"an application error that is not retried", temporal.NewNonRetryableApplicationError("stopped", "T", database), 1},
		{
			"an application error that is not retried, under a wrapper",
			fmt.Errorf("x: %w", temporal.NewNonRetryableApplicationError("stopped", "T", database)),
			0, // retried by a worker, not as a local activity: only asked to be the same
		},
		{"an error it does not know", errors.New("x"), 3},
	}
	for _, tc := range cases {
		for _, local := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/local=%t", tc.name, local), func(t *testing.T) {
				attempts := map[bool]int{}
				failures := map[bool]error{}
				for _, intercepted := range []bool{false, true} {
					env := testEnvironment(intercepted)
					var calls atomic.Int32
					env.RegisterActivityWithOptions(func(context.Context) (string, error) {
						calls.Add(1)
						return "", tc.err
					}, activity.RegisterOptions{Name: testActivityName})

					env.ExecuteWorkflow(runsTheActivity(3, local))

					require.True(t, env.IsWorkflowCompleted())
					failures[intercepted] = env.GetWorkflowError()
					require.Error(t, failures[intercepted])
					attempts[intercepted] = int(calls.Load())
				}
				assert.Equal(t, attempts[false], attempts[true], "attempts without and with the interceptor")
				if tc.attempts > 0 {
					assert.Equal(t, tc.attempts, attempts[true])
				}
				assert.Equal(t, failures[false].Error(), failures[true].Error())
			})
		}
	}
}

func Test_Interceptor_LeavesACancellation(t *testing.T) {
	for _, activityErr := range []error{
		context.Canceled,
		temporal.NewCanceledError(),
		fmt.Errorf("x: %w", errors.Join(context.Canceled, &pgconn.PgError{Code: "57014"})),
	} {
		without := failureSeenByWorkflow(t, activityErr, false)
		with := failureSeenByWorkflow(t, activityErr, true)

		assert.Equal(t, without.Error(), with.Error())
		assert.Equal(t, causes(without), causes(with))
		_, ok := Carried(with)
		assert.False(t, ok)
	}
}

// panicking is an error that cannot be inspected: asking for what it wraps panics.
type panicking struct{}

func (*panicking) Error() string { return "an error that cannot be inspected" }
func (*panicking) Unwrap() error { panic("unwrapped") }

// panickingOnce panics the first time it is asked what it wraps, which is when the
// interceptor classifies it, and answers afterwards.
type panickingOnce struct{ asked atomic.Bool }

func (*panickingOnce) Error() string { return "an error that cannot be inspected at first" }
func (p *panickingOnce) Is(error) bool {
	if p.asked.CompareAndSwap(false, true) {
		panic("compared")
	}
	return false
}

func Test_Interceptor_LeavesTheErrorWhenClassifyingPanics(t *testing.T) {
	t.Run("the interceptor gives back the very error of the activity", func(t *testing.T) {
		for _, activityErr := range []error{&panicking{}, &panickingOnce{}} {
			inbound := NewInterceptor().InterceptActivity(t.Context(), &answering{err: activityErr})

			var result any
			var err error
			require.NotPanics(t, func() {
				result, err = inbound.ExecuteActivity(t.Context(), &interceptor.ExecuteActivityInput{})
			})
			assert.Same(t, activityErr, err)
			assert.Equal(t, "the result", result)
		}
	})

	t.Run("the activity fails with its own error", func(t *testing.T) {
		without := failureSeenByWorkflow(t, &panickingOnce{}, false)
		with := failureSeenByWorkflow(t, &panickingOnce{}, true)

		var before, after *temporal.ApplicationError
		require.ErrorAs(t, without, &before)
		require.ErrorAs(t, with, &after)
		assert.Equal(t, "panickingOnce", after.Type())
		assert.Equal(t, "an error that cannot be inspected at first", after.Message())
		assert.Equal(t, before.Error(), after.Error())
		assert.Equal(t, without.Error(), with.Error())
	})

	t.Run("an error Temporal itself cannot inspect ends the same way", func(t *testing.T) {
		without := failureSeenByWorkflow(t, &panicking{}, false)
		with := failureSeenByWorkflow(t, &panicking{}, true)
		assert.Equal(t, without.Error(), with.Error())
	})
}

// answering is the end of a chain of interceptors: an activity that answers what it holds.
type answering struct {
	interceptor.ActivityInboundInterceptorBase
	err error
}

func (a *answering) ExecuteActivity(context.Context, *interceptor.ExecuteActivityInput) (any, error) {
	return "the result", a.err
}

// A panic of the activity is not one of the interceptor: it is left to Temporal, which
// fails the activity with it.
func Test_Interceptor_LetsAPanicOfTheActivityThrough(t *testing.T) {
	inbound := NewInterceptor().InterceptActivity(t.Context(), &panickingActivity{})
	assert.PanicsWithValue(t, "the activity panicked", func() {
		_, _ = inbound.ExecuteActivity(t.Context(), &interceptor.ExecuteActivityInput{})
	})

	fails := func() (string, error) { panic("the activity panicked") }
	without := seenByWorkflow(t, fails, false, 1)
	with := seenByWorkflow(t, fails, true, 1)
	require.Error(t, without.err)
	require.Error(t, with.err)
	var before, after *temporal.PanicError
	require.ErrorAs(t, without.err, &before)
	require.ErrorAs(t, with.err, &after)
	assert.Equal(t, before.Error(), after.Error())
}

type panickingActivity struct {
	interceptor.ActivityInboundInterceptorBase
}

func (*panickingActivity) ExecuteActivity(context.Context, *interceptor.ExecuteActivityInput) (any, error) {
	panic("the activity panicked")
}

func Test_Interceptor_DoesNotTouchASuccess(t *testing.T) {
	succeeds := func() (string, error) { return "done", nil }
	for _, intercepted := range []bool{false, true} {
		seen := seenByWorkflow(t, succeeds, intercepted, 1)
		require.NoError(t, seen.err)
		assert.Equal(t, "done", seen.result)
		assert.Equal(t, 1, seen.attempts)
	}

	t.Run("a nil error stays nil", func(t *testing.T) {
		inbound := NewInterceptor().InterceptActivity(t.Context(), &answering{})
		result, err := inbound.ExecuteActivity(t.Context(), &interceptor.ExecuteActivityInput{})
		require.NoError(t, err)
		assert.True(t, err == nil)
		assert.Equal(t, "the result", result)
	})
}

// The interceptor is around the activities only: a workflow is run as it was, which is what
// keeps the recorded histories replaying.
func Test_Interceptor_IsOnlyAroundActivities(t *testing.T) {
	next := &interceptor.WorkflowInboundInterceptorBase{}

	// The pass-through of Temporal, and nothing of this package.
	inbound, ok := NewInterceptor().InterceptWorkflow(nil, next).(*interceptor.WorkflowInboundInterceptorBase)
	require.True(t, ok)
	assert.Same(t, next, inbound.Next)
}
