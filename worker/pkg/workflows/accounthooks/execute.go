package accounthooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/runevents"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
)

type ExecuteAccountHookRequest struct {
	HookId string
	Event  *runevents.Event
}

type ExecuteAccountHookResponse struct{}

// ExecuteAccountHook reads the hook and delivers the event to it. The hook is read anew
// because it may have changed since the lookup: one that is gone, disabled, retired, of no
// known kind, or that does not listen to the event is skipped, and the activity succeeds.
func (a *Activities) ExecuteAccountHook(
	ctx context.Context,
	req *ExecuteAccountHookRequest,
) (*ExecuteAccountHookResponse, error) {
	if req == nil || req.Event == nil {
		return nil, errMissingEvent()
	}
	info := activity.GetInfo(ctx)
	logger := log.With(
		activity.GetLogger(ctx),
		"WorkflowID", info.WorkflowExecution.ID,
		"RunID", info.WorkflowExecution.RunID,
		"HookId", req.HookId,
		"Event", req.Event.Kind().String(),
		"Attempt", info.Attempt,
	)

	logger.Debug("retrieving hook")
	hook, err := a.readHook(ctx, req.HookId)
	if connect.CodeOf(err) == connect.CodeNotFound {
		logger.Info("the account hook no longer exists, skipping")
		return &ExecuteAccountHookResponse{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve hook: %w", err)
	}

	if !hook.GetEnabled() {
		logger.Info("the account hook is disabled, skipping")
		return &ExecuteAccountHookResponse{}, nil
	}
	if hook.GetAccountId() != req.Event.AccountID() {
		return nil, temporal.NewNonRetryableApplicationError(
			"the hook does not belong to the account of the event",
			errorTypeHookOfAnotherAccount,
			nil,
		)
	}
	if !listensTo(hook, req.Event.Kind()) {
		logger.Info("the account hook does not listen to the event, skipping")
		return &ExecuteAccountHookResponse{}, nil
	}

	switch config := hook.GetConfig().GetConfig().(type) {
	case *mgmtv1alpha1.AccountHookConfig_Webhook:
		if config.Webhook == nil {
			return nil, temporal.NewNonRetryableApplicationError(
				"webhook config was nil for account hook configuration",
				errorTypeWebhookConfigMissing,
				nil,
			)
		}
		id := deliveryID(info.WorkflowExecution.ID, req.HookId)
		if err := a.deliver(ctx, id, req.Event, config.Webhook, logger); err != nil {
			return nil, err
		}
	case *mgmtv1alpha1.AccountHookConfig_Slack:
		logger.Warn("slack account hooks are no longer supported, skipping: replace this hook with a webhook")
	default:
		logger.Warn(fmt.Sprintf("hook config type %T is not supported, skipping", config))
	}
	return &ExecuteAccountHookResponse{}, nil
}

func (a *Activities) readHook(ctx context.Context, hookId string) (*mgmtv1alpha1.AccountHook, error) {
	ctx, cancel := context.WithTimeout(ctx, a.readTimeout)
	defer cancel()
	resp, err := a.hooks.GetAccountHook(ctx, connect.NewRequest(&mgmtv1alpha1.GetAccountHookRequest{Id: hookId}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetHook(), nil
}

// listensTo tells whether the hook is told about events of the kind. A hook stored with
// the unspecified event listens to every kind.
func listensTo(hook *mgmtv1alpha1.AccountHook, kind mgmtv1alpha1.AccountHookEvent) bool {
	events := hook.GetEvents()
	return slices.Contains(events, kind) ||
		slices.Contains(events, mgmtv1alpha1.AccountHookEvent_ACCOUNT_HOOK_EVENT_UNSPECIFIED)
}

func (a *Activities) deliver(
	ctx context.Context,
	id string,
	event *runevents.Event,
	config *mgmtv1alpha1.AccountHookConfig_WebHook,
	logger log.Logger,
) error {
	// The API hides the secret from a caller it does not know as the worker. A webhook
	// signed with the mask is one that no receiver can verify.
	if config.GetSecret() == pg_models.SensitiveValue {
		return temporal.NewNonRetryableApplicationError(
			"the API returned a masked secret: the worker is not identified by its API key",
			errorTypeWebhookSecretMasked,
			nil,
		)
	}

	logger.Debug("executing webhook")
	err := a.sender.Send(ctx, webhook.Delivery{
		ID:            id,
		URL:           config.GetUrl(),
		Secret:        config.GetSecret(),
		SkipTLSVerify: config.GetDisableSslVerification(),
		Event:         event,
	})
	if err != nil {
		// The error names the host of the receiver and the status of its answer. The URL
		// and the secret are never logged.
		logger.Warn("webhook was not delivered", "error", err)
		return deliveryError(err)
	}
	logger.Debug("webhook delivered")
	return nil
}

// deliveryID names the delivery of one event to one hook. The workflow that processes an
// event has an id of its own, so that the id of a delivery is the same for every attempt,
// and differs from one event to another and from one hook to another: a receiver can tell
// a webhook it has already processed.
func deliveryID(workflowID, hookId string) string {
	sum := sha256.Sum256([]byte(workflowID + "\x00" + hookId))
	return hex.EncodeToString(sum[:])[:32]
}
