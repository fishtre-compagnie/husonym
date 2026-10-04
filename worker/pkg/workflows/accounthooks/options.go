package accounthooks

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// Both activities last seconds and bound themselves (lookupTimeout, readTimeout, the
// timeout of a webhook request): the minute below is a margin above that, and they send
// no heartbeat.

func lookupOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
		Summary:             "Lists the hooks of the account that listen to the event",
	}
}

// A delivery that failed for a reason that may heal is attempted again 5, 15, 45 and 135
// seconds later: long enough for a receiver that restarts. A failure that cannot heal is
// not retried, which the error of the activity says itself.
func executeOptions() workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 3,
			MaximumAttempts:    5,
		},
		Summary: "Delivers the event to one hook of the account",
	}
}
