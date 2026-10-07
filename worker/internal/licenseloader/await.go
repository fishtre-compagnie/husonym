package licenseloader

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/license"
)

// firstAnswerTimeout bounds one attempt at the first answer, so that an API that accepts
// the connection and says nothing is asked again rather than waited for.
const firstAnswerTimeout = 10 * time.Second

// Refresher is the part of the license provider that asks its loader.
type Refresher interface {
	Refresh(ctx context.Context) error
}

// AwaitFirstAnswer refreshes the provider until its loader has answered once, asking again
// every retryEvery, and returns nil then. It returns the error of ctx when ctx ends first.
//
// "The instance holds no key" is an answer, and so is a key the worker refuses: the API
// said what it holds. An API that does not answer is not one: a worker that took work then
// would believe there is no license while the API, which holds one, starts runs. The
// provider logs each distinct failure once.
func AwaitFirstAnswer(
	ctx context.Context,
	provider Refresher,
	retryEvery time.Duration,
	logger *slog.Logger,
) error {
	waiting := false
	for {
		attemptCtx, stopAttempt := context.WithTimeout(ctx, firstAnswerTimeout)
		err := provider.Refresh(attemptCtx)
		stopAttempt()
		if !errors.Is(err, license.ErrKeyNotLoaded) {
			logger.Info("the API answered for the license of the instance")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !waiting {
			waiting = true
			logger.Warn(
				"the API has not answered for the license of the instance: the worker takes no work until it does",
				"retryEvery", retryEvery.String(),
			)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryEvery):
		}
	}
}
