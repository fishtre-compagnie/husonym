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
	// view is what lets the caller see the owner. A caller who holds neither it nor all of
	// what the request is asked is answered as if the object were absent.
	view rbac.Action
	// actions are asked of every request, in this order. They are the permissions the
	// contract declares, view among them where it declares it; a procedure that only reads
	// asks view alone and lists nothing here.
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

// rules is what each hook procedure of the contract asks: the permissions the contract
// declares for it. One procedure asks more than it declares: SetJobHookEnabled asks for
// execute when the request enables.
//
// Execute is asked by CreateJobHook, UpdateJobHook and an enabling SetJobHookEnabled, next to
// create or edit.
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
		actions: []rbac.Action{rbac.JobAction_View, rbac.JobAction_Edit, rbac.JobAction_Execute},
		license: licenseAlways,
	},
	mgmtv1alpha1connect.JobServiceSetJobHookEnabledProcedure: {
		view:    rbac.JobAction_View,
		actions: []rbac.Action{rbac.JobAction_View, rbac.JobAction_Edit},
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
		actions: []rbac.Action{rbac.AccountAction_View, rbac.AccountAction_Edit},
		license: licenseAlways,
	},
	mgmtv1alpha1connect.AccountHookServiceSetAccountHookEnabledProcedure: {
		view:    rbac.AccountAction_View,
		actions: []rbac.Action{rbac.AccountAction_View, rbac.AccountAction_Edit},
		license: licenseToArm,
	},
	mgmtv1alpha1connect.AccountHookServiceDeleteAccountHookProcedure: {
		view:    rbac.AccountAction_View,
		actions: []rbac.Action{rbac.AccountAction_Edit},
		license: licenseNever,
	},
}
