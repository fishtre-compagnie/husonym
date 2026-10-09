package runerror

import (
	"context"

	"go.temporal.io/sdk/interceptor"
)

// NewInterceptor gives the interceptor a worker runs its activities under: the error an
// activity returns leaves it with its category (Carry). Workflows are not intercepted.
func NewInterceptor() interceptor.WorkerInterceptor {
	return &workerInterceptor{}
}

type workerInterceptor struct {
	interceptor.WorkerInterceptorBase
}

func (*workerInterceptor) InterceptActivity(
	_ context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	inbound := &activityInterceptor{}
	inbound.Next = next
	return inbound
}

type activityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
}

// ExecuteActivity runs the activity and gives its error a category. The activity itself is
// outside of what is recovered below: its panic is its own, and Temporal's to handle.
func (a *activityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (any, error) {
	result, err := a.Next.ExecuteActivity(ctx, in)
	return result, carryOrLeave(err)
}

// carryOrLeave is Carry for an error that may not bear inspection: when classifying it
// panics, the activity ends on the error it returned.
func carryOrLeave(err error) (carried error) {
	defer func() {
		if recover() != nil {
			carried = err
		}
	}()
	return Carry(err)
}
