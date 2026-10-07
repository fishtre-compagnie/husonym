// Package licenserefusal counts the license refusals that leave the API, by gate and by day.
package licenserefusal

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// Counter keeps the count of the refusals. *usagestore.Store is one.
type Counter interface {
	CountRefusal(ctx context.Context, accountId string, gates []license.Gate, at time.Time) error
}

type Interceptor struct {
	counter Counter
}

// NewInterceptor counts every *license.Refusal that a handler returns, wherever it is wrapped.
// It never changes what the handler answers: a count that fails is logged and nothing more.
func NewInterceptor(counter Counter) connect.Interceptor {
	return &Interceptor{counter: counter}
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, request connect.AnyRequest) (connect.AnyResponse, error) {
		resp, err := next(ctx, request)
		i.count(ctx, err)
		return resp, err
	}
}

func (i *Interceptor) WrapStreamingClient(
	next connect.StreamingClientFunc,
) connect.StreamingClientFunc {
	return next
}

func (i *Interceptor) WrapStreamingHandler(
	next connect.StreamingHandlerFunc,
) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		err := next(ctx, conn)
		i.count(ctx, err)
		return err
	}
}

func (i *Interceptor) count(ctx context.Context, err error) {
	var refusal *license.Refusal
	if !errors.As(err, &refusal) {
		return
	}
	logger := logger_interceptor.GetLoggerFromContextOrDefault(ctx)
	if refusal.AccountId == "" || len(refusal.Gates) == 0 {
		logger.WarnContext(ctx, "a license refusal could not be counted: it names no account or no gate")
		return
	}
	// The count must outlive a caller that gave up on the call.
	if cerr := i.counter.CountRefusal(context.WithoutCancel(ctx), refusal.AccountId, refusal.Gates, time.Now()); cerr != nil {
		logger.WarnContext(ctx, "unable to count a license refusal", "error", cerr.Error())
	}
}
