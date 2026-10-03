package husonymerrors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
	"github.com/stretchr/testify/require"
)

func TestFromPresidio(t *testing.T) {
	wrapped := func(err error) error { return fmt.Errorf("unable to analyze input: %w", err) }

	t.Run("a Presidio that did not answer is unavailable, and where it is reached is not told", func(t *testing.T) {
		cause := errors.New(`Post "http://presidio.internal:3000/analyze": connection refused`)
		err := FromPresidio(wrapped(errors.Join(presidio.ErrNoAnswer, cause)))
		require.Equal(t, connect.CodeUnavailable, connect.CodeOf(err))
		require.NotContains(t, err.Error(), "presidio.internal")
		require.Contains(t, err.Error(), "presidio did not answer")
	})

	t.Run("a caller that gave up is canceled", func(t *testing.T) {
		err := FromPresidio(wrapped(errors.Join(presidio.ErrNoAnswer, context.Canceled)))
		require.Equal(t, connect.CodeCanceled, connect.CodeOf(err))
	})

	for _, status := range []int{http.StatusBadRequest, http.StatusUnprocessableEntity} {
		t.Run(fmt.Sprintf("a request refused with %d is an invalid argument, with the words of Presidio", status), func(t *testing.T) {
			err := FromPresidio(wrapped(&presidio.RefusedError{
				Operation: "anonymize", StatusCode: status, Message: "Invalid operator class 'nope'.",
			}))
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			require.Equal(t, "Invalid operator class 'nope'.", connectErr.Message())
		})
	}

	t.Run("any other refusal is internal, and says why", func(t *testing.T) {
		err := FromPresidio(wrapped(&presidio.RefusedError{
			Operation: "analyze", StatusCode: http.StatusInternalServerError, Message: "No language provided",
		}))
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
		require.Contains(t, err.Error(), "presidio analyze refused (status 500): No language provided")
	})

	t.Run("an answer that cannot be read is internal", func(t *testing.T) {
		err := FromPresidio(wrapped(fmt.Errorf("presidio anonymize: %w: the answer has no text", presidio.ErrInvalidResponse)))
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
		require.Contains(t, err.Error(), "the answer has no text")
	})

	t.Run("an error that is not of Presidio is returned as it is", func(t *testing.T) {
		other := NewBadRequest("nope")
		require.Same(t, other, FromPresidio(other))
		plain := errors.New("plain")
		require.Same(t, plain, FromPresidio(plain))
		require.NoError(t, FromPresidio(nil))
	})
}
