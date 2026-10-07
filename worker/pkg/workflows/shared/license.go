package workflow_shared

import (
	"github.com/fishtre-compagnie/husonym/internal/license"
	"go.temporal.io/sdk/workflow"
)

const licenseReadChangeId = "license-read-recorded"

// LicenseIsValid tells a workflow whether the license is valid, and records the answer in
// the history of the run: the license follows the clock, and a replay that read it anew
// could take another branch than the run took. Runs started before the answer was recorded
// replay as they ran, reading the license directly.
//
// Every call records an answer of its own. A workflow reads it once, at its start, and
// hands the answer to what needs it: the run then keeps to its end the answer it started
// with.
func LicenseIsValid(ctx workflow.Context, lic license.EEInterface) bool {
	if workflow.GetVersion(ctx, licenseReadChangeId, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return lic.IsValid()
	}
	var valid bool
	_ = workflow.SideEffect(ctx, func(workflow.Context) any { return lic.IsValid() }).Get(&valid)
	return valid
}

const licenseFeatureReadChangeId = "license-feature-read-recorded"

// LicenseAllows tells a workflow whether the license includes the feature, and records the
// answer in the history of the run, as LicenseIsValid does and for the same reason: the
// license in force changes, and a replay that read it anew could take another branch than
// the run took.
//
// before is the answer the code acted on before it asked for the feature. Runs started then
// hold no answer in their history: they are given before, and the license is not consulted,
// so that they replay as they ran whatever the license has become.
//
// Every call records an answer of its own. A workflow reads each feature once, at its start,
// right after LicenseIsValid, and hands the answer to what needs it.
func LicenseAllows(ctx workflow.Context, lic license.EEInterface, f license.Feature, before bool) bool {
	if workflow.GetVersion(ctx, licenseFeatureReadChangeId, workflow.DefaultVersion, 1) == workflow.DefaultVersion {
		return before
	}
	var allowed bool
	_ = workflow.SideEffect(ctx, func(workflow.Context) any { return lic.HasFeature(f) }).Get(&allowed)
	return allowed
}
