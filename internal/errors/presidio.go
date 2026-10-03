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
//
// ctx is the context Presidio was called with. When it is the one that ended, Presidio is not
// what failed, and the client is not told to try again: a caller that gave up is canceled, and
// one whose time ran out exceeded its deadline. A Presidio that used up the time its client
// gives it, under a caller that still had some, did not answer.
func FromPresidio(ctx context.Context, err error) error {
	var refused *presidio.RefusedError
	switch {
	case errors.Is(err, presidio.ErrNoAnswer):
		switch ctx.Err() {
		case context.Canceled:
			return connect.NewError(connect.CodeCanceled, context.Canceled)
		case context.DeadlineExceeded:
			return connect.NewError(connect.CodeDeadlineExceeded, context.DeadlineExceeded)
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

// IsServiceFault says whether an error is the fault of the service or of what it depends on,
// and not of the request or of a caller that gave up: an error to log as one.
func IsServiceFault(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeInternal, connect.CodeUnavailable, connect.CodeUnknown, connect.CodeDataLoss:
		return true
	default:
		return false
	}
}
