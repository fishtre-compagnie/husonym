package backoffutil

import (
	"context"
	"errors"

	"github.com/cenkalti/backoff/v7"
)

// Retry is a helper function that retries an operation with the provided retry options
// and a custom retryable error check function.
//
// It takes a context, a function to execute, a function to get retry options,
// and a function to check if an error is retryable.
//
// Errors are returned as described by lastError.
func Retry[T any](
	ctx context.Context,
	fn func() (T, error),
	getOpts func() []backoff.RetryOption,
	isRetryable func(error) bool,
) (T, error) {
	res, err := backoff.Retry(ctx, func() (T, error) {
		res, err := fn()
		if err != nil && !isRetryable(err) {
			return res, backoff.Permanent(err)
		}
		return res, err
	}, getOpts()...)
	return res, lastError(err)
}

// lastError returns the operation's own error when retrying stopped on it,
// because it was not retryable or the tries ran out, so callers see the error
// they would see without retries. When the context or the elapsed time budget
// stopped retrying, it keeps the *backoff.RetryError, which matches both that
// cause and the last operation error with errors.Is and errors.As.
func lastError(err error) error {
	retryErr := backoff.AsRetryError(err)
	if retryErr == nil {
		return err
	}
	if errors.Is(retryErr.Cause, backoff.ErrPermanent) ||
		errors.Is(retryErr.Cause, backoff.ErrExhausted) {
		return retryErr.LastErr
	}
	return err
}
