package hooks

import (
	"context"
	"errors"
	"fmt"

	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgconn"
)

// The constraints that keep a name to one hook of a job, and to one hook of an account.
const (
	jobHookNameConstraint     = "job_hooks_name_unique"
	accountHookNameConstraint = "account_hooks_name_unique"
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

// nameTaken says whether a write failed on the constraint that keeps a name to one hook. Any
// other unique violation is not a name already taken.
func nameTaken(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == husonymdb.PqUniqueViolationCode &&
		pgErr.ConstraintName == constraint
}

func jobHookNameTaken(name string) error {
	return husonymerrors.NewAlreadyExists(fmt.Sprintf("a job hook named %q already exists for this job", name))
}

func accountHookNameTaken(name string) error {
	return husonymerrors.NewAlreadyExists(
		fmt.Sprintf("an account hook named %q already exists for this account", name),
	)
}

// unreadableConfig is the answer for a hook whose stored configuration cannot be decoded. It
// names the hook and nothing of what is stored: what the decoder says may quote a stored
// value, a secret included, so it is neither returned nor logged.
func unreadableConfig(ctx context.Context, kind, hookID string) error {
	logger_interceptor.GetLoggerFromContextOrDefault(ctx).
		Error("the stored config of a hook cannot be decoded", "hookKind", kind, "hookId", hookID)
	return husonymerrors.NewInternalError(
		fmt.Sprintf("the stored config of %s hook %s cannot be read", kind, hookID),
	)
}
