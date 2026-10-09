package runerror

import (
	"context"
	"errors"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	failurepb "go.temporal.io/api/failure/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"
)

// carriedDetails is what crosses the boundary of an activity with an error: the number of
// its category, and nothing of the error itself. The name of its member is written in the
// histories of the runs.
type carriedDetails struct {
	RunErrorCategory int32
}

// Carry gives the error of an activity the category Classify reads in it, as the details of
// the application error Temporal records, for the workflow to read with Carried.
//
// The error it returns is recorded by Temporal as err would have been, to the details: same
// message, same type, same retryability and delay, same causes. It is read the same by the
// worker too, which looks into the error of an activity to tell a cancellation and to retry
// a local activity. Carry holds itself to both and returns err as it is otherwise, as it does
// when it has nothing to tell: nil, a category that is "other" or "canceled", an error
// Temporal does not record as an application failure, one that already has details.
func Carry(err error) error {
	if err == nil {
		return nil
	}
	category := Classify(err)
	if category == mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER ||
		category == mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED {
		return err
	}

	// What Temporal makes of err is asked of Temporal: the message and the type of the
	// failure are its own, and are given back to it as they are.
	failures := temporal.GetDefaultFailureConverter()
	recorded := failures.ErrorToFailure(err)
	info := recorded.GetApplicationFailureInfo()
	if info == nil || len(info.GetDetails().GetPayloads()) > 0 {
		return err
	}

	carried := temporal.NewApplicationErrorWithOptions(recorded.GetMessage(), info.GetType(), temporal.ApplicationErrorOptions{
		NonRetryable:   info.GetNonRetryable(),
		NextRetryDelay: info.GetNextRetryDelay().AsDuration(),
		Category:       temporal.ApplicationErrorCategory(info.GetCategory()),
		Cause:          errors.Unwrap(err),
		Details:        []any{carriedDetails{RunErrorCategory: int32(category)}},
	})
	if !recordedAlike(failures, recorded, carried) || !readAlikeByTheWorker(err, carried, info.GetType()) {
		return err
	}
	return carried
}

// recordedAlike tells whether Temporal records carried as the failure it recorded of the
// original error, the details apart.
func recordedAlike(failures converter.FailureConverter, recorded *failurepb.Failure, carried error) bool {
	failure := failures.ErrorToFailure(carried)
	info := failure.GetApplicationFailureInfo()
	if info == nil {
		return false
	}
	info.Details = recorded.GetApplicationFailureInfo().GetDetails()
	return proto.Equal(recorded, failure)
}

// readAlikeByTheWorker tells whether the worker of Temporal, which looks into the error an
// activity returns before recording it, finds in carried what it found in the original
// error: a cancellation, which it may answer as one; and what decides whether a local
// activity is retried, which is an error of Temporal that ends the retries, or else the
// first application error and its type. originalType is the type Temporal names the original
// error by when it holds no application error.
func readAlikeByTheWorker(original, carried error, originalType string) bool {
	if errors.Is(original, context.Canceled) != errors.Is(carried, context.Canceled) {
		return false
	}
	if !sameFound[*temporal.CanceledError](original, carried) ||
		!sameFound[*temporal.TerminatedError](original, carried) ||
		!sameFound[*temporal.TimeoutError](original, carried) {
		return false
	}

	var first, firstCarried *temporal.ApplicationError
	if !errors.As(carried, &firstCarried) {
		return false
	}
	if errors.As(original, &first) {
		return first.NonRetryable() == firstCarried.NonRetryable() && first.Type() == firstCarried.Type()
	}
	return !firstCarried.NonRetryable() && firstCarried.Type() == originalType
}

// sameFound tells whether the first error of type T is the same one in both errors, or is
// in neither.
func sameFound[T comparable](original, carried error) bool {
	var found, foundCarried T
	return errors.As(original, &found) == errors.As(carried, &foundCarried) && found == foundCarried
}

// Carried gives the category an error carries out of an activity: that of the first
// application error, anywhere in err, whose details tell one. It tells false, and
// "unspecified", when there is none.
func Carried(err error) (mgmtv1alpha1.RunErrorCategory, bool) {
	category := mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED
	found := anyOf(err, func(err error) bool {
		// Not errors.As: anyOf visits every error of the tree, and each is asked in turn.
		application, ok := err.(*temporal.ApplicationError)
		if !ok || application == nil || !application.HasDetails() {
			return false
		}
		details, ok := detailsOf(application)
		if !ok || details.RunErrorCategory == 0 {
			return false
		}
		category = mgmtv1alpha1.RunErrorCategory(details.RunErrorCategory)
		return true
	})
	return category, found
}

// detailsOf reads the details of an application error as a carried category, and tells
// false when they are something else. Details that were not recorded yet are held as Go
// values, and Temporal panics when asked to read one as another type.
func detailsOf(application *temporal.ApplicationError) (details carriedDetails, ok bool) {
	defer func() {
		if recover() != nil {
			details, ok = carriedDetails{}, false
		}
	}()
	if application.Details(&details) != nil {
		return carriedDetails{}, false
	}
	return details, true
}

// anyOf tells whether an error of the tree of err, visited depth first as errors.Is does,
// satisfies is. It stops at the first one.
func anyOf(err error, is func(error) bool) bool {
	if err == nil {
		return false
	}
	if is(err) {
		return true
	}
	switch wrapper := err.(type) {
	case interface{ Unwrap() error }:
		return anyOf(wrapper.Unwrap(), is)
	case interface{ Unwrap() []error }:
		for _, wrapped := range wrapper.Unwrap() {
			if anyOf(wrapped, is) {
				return true
			}
		}
	}
	return false
}
