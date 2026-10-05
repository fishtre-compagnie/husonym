package accounthooks

import (
	"context"
	"time"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"
)

// HookReader is what the activities read from the API.
// mgmtv1alpha1connect.AccountHookServiceClient is one: the worker reads hooks and never
// writes them.
type HookReader interface {
	GetActiveAccountHooksByEvent(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetActiveAccountHooksByEventRequest],
	) (*connect.Response[mgmtv1alpha1.GetActiveAccountHooksByEventResponse], error)
	GetAccountHook(
		context.Context,
		*connect.Request[mgmtv1alpha1.GetAccountHookRequest],
	) (*connect.Response[mgmtv1alpha1.GetAccountHookResponse], error)
}

// Activities are the two activities of the workflow.
type Activities struct {
	hooks  HookReader
	sender *webhook.Sender

	// How long each call to the API may take. The activities send no heartbeat: these
	// bounds, and the timeout of a webhook request, are what ends them.
	lookupTimeout time.Duration
	readTimeout   time.Duration
}

func NewActivities(hooks HookReader, sender *webhook.Sender) *Activities {
	return &Activities{
		hooks:         hooks,
		sender:        sender,
		lookupTimeout: 30 * time.Second,
		readTimeout:   20 * time.Second,
	}
}
