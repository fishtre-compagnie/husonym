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
