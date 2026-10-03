package husonymerrors

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/pkg/presidio"
)

// FromPresidio tells a client of the API what a failed call to Presidio means for it: try
// again later when Presidio did not answer, change the request when Presidio refused what it
// carries, and an internal error otherwise. An error that is not of Presidio is returned as it
// is.
//
// What kept Presidio from answering is not told: it can quote where Presidio is reached. The
// caller logs it.
func FromPresidio(err error) error {
	var refused *presidio.RefusedError
	switch {
	case errors.Is(err, presidio.ErrNoAnswer):
		// The caller gave up: Presidio is not what failed.
		if errors.Is(err, context.Canceled) {
			return connect.NewError(connect.CodeCanceled, context.Canceled)
		}
		return connect.NewError(connect.CodeUnavailable, presidio.ErrNoAnswer)
	case errors.As(err, &refused):
		if refused.StatusCode == http.StatusBadRequest || refused.StatusCode == http.StatusUnprocessableEntity {
			return NewBadRequest(refused.Message)
		}
		return NewInternalError(err.Error())
	case errors.Is(err, presidio.ErrInvalidResponse):
		return NewInternalError(err.Error())
	default:
		return err
	}
}
