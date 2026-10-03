package hooks

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
)

// gate checks the caller of an operation. It alone knows who the caller is: an operation
// reaches the caller, whose id every write records, only by being admitted.
type gate struct {
	users userdata.Interface
}

// target is what a request acts on, found before any check.
type target struct {
	// accountID is the account that owns it; empty when nothing was found.
	accountID string
	// jobID is the job it is or belongs to, for a job hook.
	jobID string
	// absent is the answer when nothing was found, and when the caller may not see what
	// was. It is nil when the request names the account itself: the access layer then
	// answers, the same for an account that does not exist.
	absent error
}

// intent is what a request asks beyond the rule of its procedure.
type intent struct {
	// arming says the request turns a hook on.
	arming bool
	// refuse is what the object or the request refuses whoever the caller is: a retired
	// kind. It is nil when there is nothing of the sort.
	refuse func() error
}

// admission is a caller the checks let through, and the account the operation is in.
type admission struct {
	caller    *userdata.User
	accountID string
}

// admit checks the caller against the rule of the procedure, in the order of the package:
// seeing the owner, each permission, what the object refuses, the license.
func (g gate) admit(ctx context.Context, procedure string, t target, in intent) (*admission, error) {
	r, ok := rules[procedure]
	if !ok {
		return nil, fmt.Errorf("no rule says what %s asks of its caller", procedure)
	}
	caller, err := g.users.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if t.accountID == "" {
		return nil, t.absent
	}
	if err := enforce(ctx, caller, t, r.view); err != nil {
		if t.absent != nil && connect.CodeOf(err) == connect.CodePermissionDenied {
			return nil, t.absent
		}
		return nil, err
	}
	for _, action := range r.asked(in.arming) {
		if err := enforce(ctx, caller, t, action); err != nil {
			return nil, err
		}
	}
	if in.refuse != nil {
		if err := in.refuse(); err != nil {
			return nil, err
		}
	}
	if r.needsLicense(in.arming) {
		if err := caller.EnforceLicense(ctx, t.accountID); err != nil {
			return nil, err
		}
	}
	return &admission{caller: caller, accountID: t.accountID}, nil
}

// enforce asks the access layer whether the caller may do the action on the target.
func enforce(ctx context.Context, caller *userdata.User, t target, action rbac.Action) error {
	switch action := action.(type) {
	case rbac.JobAction:
		return caller.EnforceJob(ctx, userdata.NewDomainEntity(t.accountID, t.jobID), action)
	case rbac.AccountAction:
		return caller.EnforceAccount(ctx, userdata.NewIdentifier(t.accountID), action)
	default:
		return fmt.Errorf("a hook asks nothing about a %s", action.Kind())
	}
}
