package runerror

import (
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
)

// License marks err as a refusal of the license, for an activity that refuses to return:
// Classify and CategoryOf read it as such, wherever it is in an error, on both sides of the
// boundary of the activity.
//
// The mark is the category told in the details of the error (Tell), and nothing else: what
// Temporal records of the marked error is what it would have recorded of err, the details
// apart. An error that cannot be marked at that price is returned as it is.
//
// The error returned is another value than err: a refusal kept as a variable, to be returned
// and compared with, is to be the marked value.
//
// A workflow that refuses by itself needs no mark: it tells the category with the step of its
// run (workflow_shared.RunFailure).
func License(err error) error {
	return Tell(err, mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE)
}

func isLicense(err error) bool {
	category, ok := Carried(err)
	return ok && category == mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE
}
