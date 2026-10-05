package accounthooks

import "github.com/fishtre-compagnie/husonym/worker/pkg/workflows/accounthooks/webhook"

// Registry is what Register needs of a worker or of a test environment.
type Registry interface {
	RegisterWorkflow(w any)
	RegisterActivity(a any)
}

// Register registers the workflow and its two activities under the names of their
// functions.
func Register(r Registry, hooks HookReader, sender *webhook.Sender) {
	activities := NewActivities(hooks, sender)
	r.RegisterWorkflow(ProcessAccountHook)
	r.RegisterActivity(activities.GetAccountHooksByEvent)
	r.RegisterActivity(activities.ExecuteAccountHook)
}
