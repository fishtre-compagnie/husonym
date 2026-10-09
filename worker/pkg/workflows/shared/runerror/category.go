package runerror

import (
	"errors"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"go.temporal.io/sdk/temporal"
)

// CategoryOf gives the category of the error a run ends on, as its workflow sees it. The
// first of these that is found anywhere in the error decides:
//
//  1. the category an activity carried out (Carried), the mark of a refusal of the license
//     included, which is one;
//  2. a timeout of Temporal;
//  3. a cancellation of Temporal.
//
// Anything else is "other", nil included. It reads the error and nothing else: a workflow
// may call it, and gets the same answer when it is replayed.
func CategoryOf(err error) mgmtv1alpha1.RunErrorCategory {
	if category, ok := Carried(err); ok {
		return category
	}
	var timeoutErr *temporal.TimeoutError
	if errors.As(err, &timeoutErr) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
	}
	var canceledErr *temporal.CanceledError
	if errors.As(err, &canceledErr) {
		return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED
	}
	return mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
}
