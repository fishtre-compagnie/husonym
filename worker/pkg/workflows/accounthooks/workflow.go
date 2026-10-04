package accounthooks

import (
	"errors"
	"fmt"

	"github.com/fishtre-compagnie/husonym/internal/runevents"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ProcessAccountHookRequest is the input of the workflow. Its serialized form is recorded
// in histories.
type ProcessAccountHookRequest struct {
	Event *runevents.Event
}

// ProcessAccountHookResponse is the output of the workflow: nothing.
type ProcessAccountHookResponse struct{}

// ProcessAccountHook delivers one lifecycle event to the hooks of its account that listen
// to it. It fails when the event is missing, when the hooks cannot be listed, or when a
// hook failed: its failure is how an operator sees a webhook that was not delivered.
func ProcessAccountHook(
	ctx workflow.Context,
	req *ProcessAccountHookRequest,
) (*ProcessAccountHookResponse, error) {
	if req == nil || req.Event == nil {
		return nil, errMissingEvent()
	}

	var activities *Activities
	var lookup *GetAccountHooksByEventResponse
	err := workflow.ExecuteActivity(
		workflow.WithActivityOptions(ctx, lookupOptions()),
		activities.GetAccountHooksByEvent,
		&GetAccountHooksByEventRequest{
			AccountId: req.Event.AccountID(),
			EventName: req.Event.Kind(),
		},
	).Get(ctx, &lookup)
	if err != nil {
		return nil, err
	}

	// Every hook is started before the first one is awaited: none waits for another.
	executeCtx := workflow.WithActivityOptions(ctx, executeOptions())
	executions := make([]workflow.Future, 0, len(lookup.HookIds))
	for _, hookId := range lookup.HookIds {
		executions = append(executions, workflow.ExecuteActivity(
			executeCtx,
			activities.ExecuteAccountHook,
			&ExecuteAccountHookRequest{HookId: hookId, Event: req.Event},
		))
	}

	logger := workflow.GetLogger(ctx)
	var failures []error
	for i, execution := range executions {
		if err := execution.Get(ctx, nil); err != nil {
			logger.Error(
				"account hook failed",
				"accountId", req.Event.AccountID(),
				"hookId", lookup.HookIds[i],
				"error", err,
			)
			failures = append(failures, fmt.Errorf("error executing hook: %w", err))
		}
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("error executing hooks: %w", errors.Join(failures...))
	}
	return &ProcessAccountHookResponse{}, nil
}

func errMissingEvent() error {
	return temporal.NewNonRetryableApplicationError("event is required", errorTypeMissingEvent, nil)
}
