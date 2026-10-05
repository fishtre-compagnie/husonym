package accounthooks

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

type GetAccountHooksByEventRequest struct {
	AccountId string
	EventName mgmtv1alpha1.AccountHookEvent
}

type GetAccountHooksByEventResponse struct {
	HookIds []string
}

// GetAccountHooksByEvent returns the ids of the active hooks of the account for the
// event, in the order of the API: oldest first. Only the ids leave the activity, so that
// the secret of a hook never enters a workflow history.
func (a *Activities) GetAccountHooksByEvent(
	ctx context.Context,
	req *GetAccountHooksByEventRequest,
) (*GetAccountHooksByEventResponse, error) {
	info := activity.GetInfo(ctx)
	logger := log.With(
		activity.GetLogger(ctx),
		"WorkflowID", info.WorkflowExecution.ID,
		"RunID", info.WorkflowExecution.RunID,
		"AccountId", req.AccountId,
		"EventName", req.EventName.String(),
	)
	ctx, cancel := context.WithTimeout(ctx, a.lookupTimeout)
	defer cancel()

	logger.Debug("asking the API for the hooks of the event")
	resp, err := a.hooks.GetActiveAccountHooksByEvent(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetActiveAccountHooksByEventRequest{
			AccountId: req.AccountId,
			Event:     req.EventName,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("the hooks of the event cannot be listed: %w", err)
	}
	hooks := resp.Msg.GetHooks()
	logger.Debug("the API listed the hooks of the event", "count", len(hooks))

	hookIds := make([]string, 0, len(hooks))
	for _, hook := range hooks {
		hookIds = append(hookIds, hook.GetId())
	}
	return &GetAccountHooksByEventResponse{HookIds: hookIds}, nil
}
