// Package licenserefusal counts the license refusals that leave the API, by gate and by day.
package licenserefusal

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	refusal "github.com/fishtre-compagnie/husonym/backend/internal/licenserefusal"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

type Interceptor struct {
	counter refusal.Counter
}

// NewInterceptor counts every *license.Refusal that a handler returns, wherever it is wrapped.
// It never changes what the handler answers: a count that fails is logged and nothing more.
func NewInterceptor(counter refusal.Counter) connect.Interceptor {
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
	var licenseRefusal *license.Refusal
	if errors.As(err, &licenseRefusal) {
		refusal.Count(ctx, i.counter, licenseRefusal.AccountId, licenseRefusal.Gates)
	}
}
