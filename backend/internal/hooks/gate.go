package hooks

import (
	"context"
	"fmt"
	"slices"

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
	asked := r.asked(in.arming)
	if t.accountID == "" {
		if t.absent == nil {
			return nil, fmt.Errorf("%s was given nothing to act on", procedure)
		}
		return nil, t.absent
	}
	viewer, err := holds(ctx, caller, t, r.view)
	if err != nil {
		return nil, err
	}
	seen := viewer
	if !seen {
		seen, err = holdsAll(ctx, caller, t, asked)
		if err != nil {
			return nil, err
		}
	}
	if !seen && t.absent != nil {
		return nil, t.absent
	}
	if !seen {
		// The request names the account itself: the access layer says what is missing,
		// and says the same of an account that does not exist.
		asked = slices.Concat(asked, []rbac.Action{r.view})
	}
	for _, action := range asked {
		if viewer && action == r.view {
			continue
		}
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

// holdsAll says whether the caller holds every action an operation asks, when it asks any.
//
// It is the second way to see a target, after viewing its owner: a caller of the owner's
// account who holds all that the operation asks may do it, so an API key scoped to what a
// procedure declares calls it without holding view. A caller outside the account holds
// nothing there, whatever the caller holds elsewhere.
func holdsAll(ctx context.Context, caller *userdata.User, t target, asked []rbac.Action) (bool, error) {
	if len(asked) == 0 {
		return false, nil
	}
	for _, action := range asked {
		held, err := holds(ctx, caller, t, action)
		if err != nil || !held {
			return false, err
		}
	}
	return true, nil
}

// holds says whether the caller may do the action on the target. A caller the access layer
// refuses — one outside the account — holds nothing; a failure to decide is an error.
func holds(ctx context.Context, caller *userdata.User, t target, action rbac.Action) (bool, error) {
	var held bool
	var err error
	switch action := action.(type) {
	case rbac.JobAction:
		held, err = caller.Job(ctx, userdata.NewDomainEntity(t.accountID, t.jobID), action)
	case rbac.AccountAction:
		held, err = caller.Account(ctx, userdata.NewIdentifier(t.accountID), action)
	default:
		return false, fmt.Errorf("a hook asks nothing about a %s", action.Kind())
	}
	if connect.CodeOf(err) == connect.CodePermissionDenied {
		return false, nil
	}
	return held, err
}

// enforce asks the access layer whether the caller may do the action on the target, and
// gives its refusal.
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
