package husonymerrors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

func TestFromPresidio(t *testing.T) {
	wrapped := func(err error) error { return fmt.Errorf("unable to analyze input: %w", err) }

	t.Run("a Presidio that did not answer is unavailable, and where it is reached is not told", func(t *testing.T) {
		cause := errors.New(`Post "http://presidio.internal:3000/analyze": connection refused`)
		err := FromPresidio(context.Background(), wrapped(errors.Join(presidio.ErrNoAnswer, cause)))
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
		require.NotContains(t, err.Error(), "presidio.internal")
		require.Contains(t, err.Error(), "presidio did not answer")
	})

	t.Run("a caller that gave up is canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := FromPresidio(ctx, wrapped(errors.Join(presidio.ErrNoAnswer, context.Canceled)))
		require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))
		require.False(t, IsServiceFault(err))
	})

	t.Run("a caller whose time ran out during the call exceeded its deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		err := FromPresidio(ctx, wrapped(errors.Join(presidio.ErrNoAnswer, context.DeadlineExceeded)))
		require.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
		require.False(t, IsServiceFault(err))
	})

	t.Run("a Presidio that used up the time the client gives it did not answer", func(t *testing.T) {
		// The caller still has time: the deadline that expired is the client's own.
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		err := FromPresidio(ctx, wrapped(errors.Join(presidio.ErrNoAnswer, context.DeadlineExceeded)))
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
		require.True(t, IsServiceFault(err))
	})

	t.Run("what is the fault of the service or of Presidio is told from what is the caller's", func(t *testing.T) {
		require.True(t, IsServiceFault(NewInternalError("boom")))
		require.True(t, IsServiceFault(connect.NewError(connect.CodeUnavailable, presidio.ErrNoAnswer)))
		require.True(t, IsServiceFault(errors.New("an error without a code")))
		require.False(t, IsServiceFault(NewBadRequest("nope")))
		require.False(t, IsServiceFault(NewForbidden("nope")))
		require.False(t, IsServiceFault(NewNotFound("nope")))
	})

	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		t.Run(fmt.Sprintf("a request refused with %d is an invalid argument, with the words of Presidio", status), func(t *testing.T) {
			err := FromPresidio(context.Background(), wrapped(&presidio.RefusedError{
				Operation: "anonymize", StatusCode: status, Message: "Invalid operator class 'nope'.",
			}))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			require.Equal(t, "Invalid operator class 'nope'.", connectErr.Message())
		})
	}

	t.Run("any other refusal is internal, and says why", func(t *testing.T) {
		err := FromPresidio(context.Background(), wrapped(&presidio.RefusedError{
			Operation: "analyze", StatusCode: http.StatusInternalServerError, Message: "No language provided",
		}))
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
		require.Contains(t, err.Error(), "presidio analyze refused (status 500): No language provided")
	})

	t.Run("an answer that cannot be read is internal", func(t *testing.T) {
		err := FromPresidio(context.Background(), wrapped(fmt.Errorf("presidio anonymize: %w: the answer has no text", presidio.ErrInvalidResponse)))
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
		require.Contains(t, err.Error(), "the answer has no text")
	})

	t.Run("an error that is not of Presidio is returned as it is", func(t *testing.T) {
		other := NewBadRequest("nope")
		require.Same(t, other, FromPresidio(context.Background(), other))
		plain := errors.New("plain")
		require.Same(t, plain, FromPresidio(context.Background(), plain))
		require.NoError(t, FromPresidio(context.Background(), nil))
	})
}
