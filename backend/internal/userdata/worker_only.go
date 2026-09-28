package userdata

import husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"

// WorkerOnly guards what only the worker calls: the context of a run, which holds what the
// run executes; the key an account derives its pseudonyms with; the mappings a run reconciles
// with what it read. A worker key always passes. Once the worker has a key of its own, it alone
// does; without one, with authentication on, an API key does, never the session of a person,
// since no page calls these. Without authentication, every caller may do anything and nothing
// is told apart.
type WorkerOnly struct {
	IsAuthEnabled  bool
	IsHusonymCloud bool
	// HasWorkerApiKeys: authentication is on and the worker has a key of its own.
	HasWorkerApiKeys bool
}

// Allow says whether a caller may call what only the worker calls.
func (w WorkerOnly) Allow(user *User) error {
	switch {
	case user.IsWorkerApiKey():
		return nil
	case w.IsHusonymCloud, w.HasWorkerApiKeys:
		return husonymerrors.NewForbidden("only the worker calls this, with its key")
	case w.IsAuthEnabled && !user.IsApiKey():
		return husonymerrors.NewForbidden("only the worker calls this, never from a session")
	default:
		return nil
	}
}
