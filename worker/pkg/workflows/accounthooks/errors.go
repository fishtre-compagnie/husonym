package accounthooks

import (
	"errors"
	"fmt"

	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
	"go.temporal.io/sdk/temporal"
)

// The types of the errors that are not retried. They name the cause in the history of
// the workflow.
const (
	errorTypeMissingEvent              = "MissingEvent"
	errorTypeHookOfAnotherAccount      = "HookOfAnotherAccount"
	errorTypeWebhookConfigMissing      = "WebhookConfigMissing"
	errorTypeWebhookSecretMasked       = "WebhookSecretMasked"
	errorTypeWebhookInvalidEvent       = "WebhookInvalidEvent"
	errorTypeWebhookInvalidURL         = "WebhookInvalidURL"
	errorTypeWebhookDestinationRefused = "WebhookDestinationRefused"
	errorTypeWebhookRedirected         = "WebhookRedirected"
	errorTypeWebhookRejected           = "WebhookRejected"
)

var permanentErrorTypes = map[webhook.Reason]string{
	webhook.ReasonEvent:       errorTypeWebhookInvalidEvent,
	webhook.ReasonInvalidURL:  errorTypeWebhookInvalidURL,
	webhook.ReasonDestination: errorTypeWebhookDestinationRefused,
	webhook.ReasonRedirected:  errorTypeWebhookRedirected,
	webhook.ReasonRejected:    errorTypeWebhookRejected,
}

// deliveryError is the error of the activity for a webhook that was not delivered. A
// failure that a new attempt cannot cure is not retried, and the error says so itself:
// it knows the cause, whatever retry policy the activity was scheduled with.
func deliveryError(err error) error {
	var failure *webhook.Error
	if errors.As(err, &failure) && failure.Permanent() {
		return temporal.NewNonRetryableApplicationError(
			"the webhook was not delivered",
			permanentErrorTypes[failure.Reason],
			failure,
		)
	}
	return fmt.Errorf("the webhook was not delivered: %w", err)
}
