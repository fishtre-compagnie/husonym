package hooks

import (
	"errors"
	"fmt"

	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// errNothingToRemove is what a delete is told when there is no hook to remove, or none the
// caller may see: the delete then answers that it is done.
var errNothingToRemove = errors.New("no hook to remove")

func jobNotFound() error     { return husonymerrors.NewNotFound("unable to find job id") }
func jobHookNotFound() error { return husonymerrors.NewNotFound("unable to find job hook by id") }
func accountHookNotFound() error {
	return husonymerrors.NewNotFound("unable to find account hook by id")
}

// invalidID refuses an id that is not a uuid, naming what it was the id of.
func invalidID(what string) error {
	return husonymerrors.NewBadRequest(what + " id is not a valid uuid")
}

// writeFailed tells why a write failed. A name is one hook's within its owner, which the
// database enforces: its refusal is told as a name already taken.
func writeFailed(err error, doing, kind, name, owner string) error {
	if husonymdb.IsConflict(err) {
		return husonymerrors.NewAlreadyExists(
			fmt.Sprintf("%s hook named %q already exists for this %s", kind, name, owner),
		)
	}
	return fmt.Errorf("unable to %s: %w", doing, err)
}
