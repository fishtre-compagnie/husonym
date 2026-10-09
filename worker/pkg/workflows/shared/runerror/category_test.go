package runerror

import (
	"context"
	"errors"
	"fmt"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
)

func Test_CategoryOf(t *testing.T) {
	carriedTimeout := Carry(fmt.Errorf("reading: %w", context.DeadlineExceeded))
	require.NotNil(t, carriedTimeout)
	_, isCarried := Carried(carriedTimeout)
	require.True(t, isCarried)

	cases := []struct {
		name     string
		err      error
		expected mgmtv1alpha1.RunErrorCategory
	}{
		{"a carried category under a wrapper", fmt.Errorf("table users: %w", carriedTimeout), timeout},
		{
			"a carried category as an activity failure gives it",
			seenByAWorkflow(Carry(fmt.Errorf("x: %w", &pgconn.PgError{Code: "23505"}))),
			constraintViolated,
		},
		{
			"a carried category before the license",
			License(Carry(fmt.Errorf("x: %w", &pgconn.PgError{Code: "42501"}))),
			insufficientPrivileges,
		},
		{"a refusal of the license", fmt.Errorf("halting: %w", License(errors.New("x"))), license},
		{
			"an activity that timed out",
			fmt.Errorf("x: %w", temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)),
			timeout,
		},
		{
			"an activity whose heartbeat timed out",
			fmt.Errorf("x: %w", temporal.NewHeartbeatTimeoutError()),
			timeout,
		},
		{"a cancellation", fmt.Errorf("x: %w", temporal.NewCanceledError()), canceled},
		{
			"a timeout before a cancellation",
			errors.Join(temporal.NewCanceledError(), temporal.NewTimeoutError(enumspb.TIMEOUT_TYPE_START_TO_CLOSE, nil)),
			timeout,
		},
		{"an application error with a type of its own", temporal.NewApplicationError("m", "PreflightBlocking"), other},
		{"an application error with details of its own", temporal.NewApplicationError("m", "T", "a detail"), other},
		// What is typed in the worker does not reach a workflow: only Classify reads it.
		{"a database error", &pgconn.PgError{Code: "23505"}, other},
		{"a plain error", errors.New("x"), other},
		{"nothing", nil, other},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, CategoryOf(tc.err))
			assert.Equal(t, tc.expected, CategoryOf(tc.err), "the same answer when asked again")
		})
	}
}

// seenByAWorkflow gives the error a workflow sees of an activity that failed with err: the
// failure Temporal records, read back, under the error of the activity.
func seenByAWorkflow(err error) error {
	converter := temporal.GetDefaultFailureConverter()
	return fmt.Errorf("activity failed: %w", converter.FailureToError(converter.ErrorToFailure(err)))
}
