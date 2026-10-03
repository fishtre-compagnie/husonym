package hooks

import (
	"slices"

	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
)

// licenseNeed says when an operation needs the account to hold a license.
type licenseNeed int

const (
	// licenseNever: reading, turning off and deleting never need a license.
	licenseNever licenseNeed = iota
	// licenseAlways: creating and changing always do.
	licenseAlways
	// licenseToArm: turning on does, turning off does not.
	licenseToArm
)

// rule is what a procedure asks of its caller.
type rule struct {
	// view is what lets the caller see the owner. Without it the object is answered as
	// absent.
	view rbac.Action
	// actions are asked of every request, in this order.
	actions []rbac.Action
	// arming is asked in addition of a request that turns a hook on.
	arming  []rbac.Action
	license licenseNeed
}

func (r rule) asked(arming bool) []rbac.Action {
	if arming {
		return slices.Concat(r.actions, r.arming)
	}
	return r.actions
}

func (r rule) needsLicense(arming bool) bool {
	return r.license == licenseAlways || (r.license == licenseToArm && arming)
}

var (
	seesJob     = rule{view: rbac.JobAction_View, license: licenseNever}
	seesAccount = rule{view: rbac.AccountAction_View, license: licenseNever}
)

// rules is what each hook procedure of the contract asks.
//
// The SQL of a job hook runs, at the next run, on a connection of the job, its source
// included: writing or turning on a job hook therefore takes executing the job, on top of
// creating or editing it.
var rules = map[string]rule{
	mgmtv1alpha1connect.JobServiceGetJobHooksProcedure:               seesJob,
	mgmtv1alpha1connect.JobServiceGetJobHookProcedure:                seesJob,
	mgmtv1alpha1connect.JobServiceIsJobHookNameAvailableProcedure:    seesJob,
	mgmtv1alpha1connect.JobServiceGetActiveJobHooksByTimingProcedure: seesJob,
	mgmtv1alpha1connect.JobServiceCreateJobHookProcedure: {
		view:    rbac.JobAction_View,
		actions: []rbac.Action{rbac.JobAction_Create, rbac.JobAction_Execute},
		license: licenseAlways,
	},
	mgmtv1alpha1connect.JobServiceUpdateJobHookProcedure: {
		view:    rbac.JobAction_View,
		actions: []rbac.Action{rbac.JobAction_Edit, rbac.JobAction_Execute},
		license: licenseAlways,
	},
	mgmtv1alpha1connect.JobServiceSetJobHookEnabledProcedure: {
		view:    rbac.JobAction_View,
		actions: []rbac.Action{rbac.JobAction_Edit},
		arming:  []rbac.Action{rbac.JobAction_Execute},
		license: licenseToArm,
	},
	mgmtv1alpha1connect.JobServiceDeleteJobHookProcedure: {
		view:    rbac.JobAction_View,
		actions: []rbac.Action{rbac.JobAction_Delete},
		license: licenseNever,
	},

	mgmtv1alpha1connect.AccountHookServiceGetAccountHooksProcedure:              seesAccount,
	mgmtv1alpha1connect.AccountHookServiceGetAccountHookProcedure:               seesAccount,
	mgmtv1alpha1connect.AccountHookServiceIsAccountHookNameAvailableProcedure:   seesAccount,
	mgmtv1alpha1connect.AccountHookServiceGetActiveAccountHooksByEventProcedure: seesAccount,
	mgmtv1alpha1connect.AccountHookServiceCreateAccountHookProcedure: {
		view:    rbac.AccountAction_View,
		actions: []rbac.Action{rbac.AccountAction_Edit},
		license: licenseAlways,
	},
	mgmtv1alpha1connect.AccountHookServiceUpdateAccountHookProcedure: {
		view:    rbac.AccountAction_View,
		actions: []rbac.Action{rbac.AccountAction_Edit},
		license: licenseAlways,
	},
	mgmtv1alpha1connect.AccountHookServiceSetAccountHookEnabledProcedure: {
		view:    rbac.AccountAction_View,
		actions: []rbac.Action{rbac.AccountAction_Edit},
		license: licenseToArm,
	},
	mgmtv1alpha1connect.AccountHookServiceDeleteAccountHookProcedure: {
		view:    rbac.AccountAction_View,
		actions: []rbac.Action{rbac.AccountAction_Edit},
		license: licenseNever,
	},
}
