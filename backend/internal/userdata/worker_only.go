package userdata

import husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"

// WorkerOnly guards what only the worker calls: the context of a run, which holds what the
// run executes; the key an account derives its pseudonyms with; the mappings a run reconciles
// with what it read. With authentication on, the worker has a key of its own, and that key
// alone may. Without authentication, every caller may do anything and nothing is told apart.
type WorkerOnly struct {
	IsAuthEnabled bool
}

// Allow says whether a caller may call what only the worker calls.
func (w WorkerOnly) Allow(user *User) error {
	switch {
	case user.IsWorkerApiKey():
		return nil
	case w.IsAuthEnabled:
		return husonymerrors.NewForbidden("only the worker calls this, with its key")
	default:
		return nil
	}
}
