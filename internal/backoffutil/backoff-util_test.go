package backoffutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/stretchr/testify/require"
)

var errOperation = errors.New("operation failed")

func fastRetryOpts(maxTries uint) func() []backoff.RetryOption {
	return func() []backoff.RetryOption {
		return []backoff.RetryOption{
			backoff.WithBackOff(&backoff.ConstantBackOff{Interval: time.Millisecond}),
			backoff.WithMaxTries(maxTries),
		}
	}
}

func Test_Retry(t *testing.T) {
	t.Parallel()

	t.Run("returns the operation error when it is not retryable", func(t *testing.T) {
		t.Parallel()
		calls := 0
		_, err := Retry(t.Context(), func() (int, error) {
			calls++
			return 0, errOperation
		}, fastRetryOpts(5), func(error) bool { return false })
		require.Same(t, errOperation, err)
		require.Equal(t, 1, calls)
	})

	t.Run("returns the last operation error when the tries run out", func(t *testing.T) {
		t.Parallel()
		calls := 0
		_, err := Retry(t.Context(), func() (int, error) {
			calls++
			return 0, errOperation
		}, fastRetryOpts(3), func(error) bool { return true })
		require.Same(t, errOperation, err)
		require.Equal(t, 3, calls)
	})

	t.Run("keeps both the context cause and the last error", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		_, err := Retry(ctx, func() (int, error) {
			cancel()
			return 0, errOperation
		}, fastRetryOpts(5), func(error) bool { return true })
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, err, errOperation)
	})

	t.Run("returns the result on success", func(t *testing.T) {
		t.Parallel()
		calls := 0
		res, err := Retry(t.Context(), func() (int, error) {
			calls++
			if calls < 2 {
				return 0, errOperation
			}
			return 42, nil
		}, fastRetryOpts(5), func(error) bool { return true })
		require.NoError(t, err)
		require.Equal(t, 42, res)
	})
}
