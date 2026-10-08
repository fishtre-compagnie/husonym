// Package enforcer holds the rules of the access control in memory, answers the checks from
// them, and keeps them in step with the table they are stored in.
package enforcer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
)

const (
	// reloadPeriod is how often the role assignments are read again from the table, which is
	// how an instance of the API learns of the changes another one made.
	reloadPeriod = 10 * time.Second

	// modelDocument says how a request (person, account, object, action) is decided: it is
	// allowed when a rule grants the action on the object to a role the person holds in that
	// account. A rule names its account, or every account with "*".
	//
	// The account of a role assignment is never a pattern: the account term is written with
	// an equality, not with keyMatch, which would make the engine match the accounts of the
	// assignments as patterns too, and walk every account at each check and each reload.
	modelDocument = `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && (p.dom == "*" || r.dom == p.dom) && keyMatch(r.obj, p.obj) && keyMatch(r.act, p.act)
`
)

// Enforcer answers the checks from the rules it holds in memory, writes each change of them
// through to their store, and reads them again from it.
//
// A reload reads the store, then puts what it read in place. A change made between the two
// would be in the store and no longer in memory: a reload and a change therefore never overlap.
// Checks wait for neither.
type Enforcer struct {
	inner *casbin.SyncedEnforcer
	// reloading is held alone by a reload and by the replacement of a role, and shared by the
	// other changes.
	reloading sync.RWMutex
	logger    *slog.Logger
	// replaceAssignment leaves a person one role in an account, in the table, within the time
	// a write is given.
	replaceAssignment func(user, role, account string) error
	// firstAssignments asks the table for the role of a person that holds none, within the time
	// a write is given. It is nil on a table that does not answer that.
	firstAssignments *boundedFirstAssignments
}

// boundedFirstAssignments asks the table what FirstAssignments names, each within a time limit.
type boundedFirstAssignments struct {
	has func(user, account string) (bool, error)
	add func(user, role, account string) (bool, error)
}

// New builds the enforcer on the rows of the rule table and on the rules that are the same in
// every account, loads the role assignments, and reads them again every ten seconds until ctx
// ends. Each read and write of the rows is given a time limit, and ends with ctx.
func New(ctx context.Context, rows Rows, fixedRules [][]string, logger *slog.Logger) (*Enforcer, error) {
	store := &boundedStore{
		store:        &roleStore{Rows: rows, fixedRules: fixedRules, logger: logger},
		ctx:          ctx,
		readTimeout:  storeReadTimeout,
		writeTimeout: storeWriteTimeout,
	}
	e, err := newEnforcer(store)
	if err != nil {
		return nil, err
	}
	e.logger = logger
	e.replaceAssignment = func(user, role, account string) error {
		return store.within(store.writeTimeout, func(ctx context.Context) error {
			return rows.ReplaceAssignmentCtx(ctx, user, role, account)
		})
	}
	if first, ok := rows.(FirstAssignments); ok {
		e.firstAssignments = &boundedFirstAssignments{
			has: func(user, account string) (held bool, err error) {
				err = store.within(store.writeTimeout, func(ctx context.Context) error {
					held, err = first.HasAssignmentCtx(ctx, user, account)
					return err
				})
				return held, err
			},
			add: func(user, role, account string) (given bool, err error) {
				err = store.within(store.writeTimeout, func(ctx context.Context) error {
					given, err = first.AddAssignmentIfNoneCtx(ctx, user, role, account)
					return err
				})
				return given, err
			},
		}
	}
	go e.reloadEvery(ctx, reloadPeriod)
	return e, nil
}

// newEnforcer builds the enforcer on a store, and loads what it holds.
func newEnforcer(store persist.BatchAdapter) (*Enforcer, error) {
	m, err := model.NewModelFromString(modelDocument)
	if err != nil {
		return nil, fmt.Errorf("unable to read the model of the access rules: %w", err)
	}
	inner, err := casbin.NewSyncedEnforcer(m, store)
	if err != nil {
		return nil, fmt.Errorf("unable to load the access rules: %w", err)
	}
	return &Enforcer{inner: inner, logger: slog.Default()}, nil
}

// change makes a change of the rules, which a reload does not overlap.
func (e *Enforcer) change(apply func() (bool, error)) (bool, error) {
	e.reloading.RLock()
	defer e.reloading.RUnlock()
	return apply()
}

// ErrNotReadBack tells that a role was stored and that the roles could not be read again from
// the table afterwards: the role is held on this instance once they are.
var ErrNotReadBack = errors.New("the role is stored, and the roles could not be read again")

// SetRoleForUserInDomain leaves a person that role in an account, and no other. The table is
// changed first, in one transaction, whatever this instance holds in memory; the roles are then
// read again from it, so that memory follows the table and never the reverse. Nothing else
// changes the roles meanwhile on this instance.
//
// When it returns nil the table held that role for the person, and this instance holds what the
// table held. When the table refuses the change, nothing has changed. When the change runs out
// of time or loses the database while it is committed, the table may hold either the role held
// before or the role asked for, never none; this instance keeps deciding from what it held, and
// sees which at the next reload.
func (e *Enforcer) SetRoleForUserInDomain(user, role, domain string) error {
	e.reloading.Lock()
	defer e.reloading.Unlock()
	if err := e.replaceAssignment(user, role, domain); err != nil {
		return err
	}
	if err := e.inner.LoadPolicy(); err != nil {
		return fmt.Errorf("%w: %w", ErrNotReadBack, err)
	}
	return nil
}

// ErrNoFirstAssignments tells that the table the enforcer was built on cannot give a role to who
// holds none.
var ErrNoFirstAssignments = errors.New("the table of the access rules does not give a role to who holds none")

// SetRoleForUserInDomainIfNone gives a person that role in an account when the table holds none
// for them there, and leaves the role they hold otherwise. The table decides, never what this
// instance holds in memory: a role another instance gave since the last reload is kept.
//
// It is made to be asked often. Where a role is held it is one read of the table: nothing is
// written, the roles are not read again, and neither a reload nor a change of the rules waits
// for it. Only when the read finds no role does it take its turn as SetRoleForUserInDomain
// does: the table is asked to write unless a role was given meanwhile, and when it did write
// the roles are read again from it, so that this instance holds the role once it returns nil.
// ErrNotReadBack means here what it means there.
//
// A role it finds stored and that this instance has not read yet is held here at the next
// reload, as any role another instance gave.
func (e *Enforcer) SetRoleForUserInDomainIfNone(user, role, domain string) error {
	if e.firstAssignments == nil {
		return ErrNoFirstAssignments
	}
	held, err := e.firstAssignments.has(user, domain)
	if err != nil {
		return err
	}
	if held {
		return nil
	}

	e.reloading.Lock()
	defer e.reloading.Unlock()
	given, err := e.firstAssignments.add(user, role, domain)
	if err != nil {
		return err
	}
	if !given {
		return nil
	}
	if err := e.inner.LoadPolicy(); err != nil {
		return fmt.Errorf("%w: %w", ErrNotReadBack, err)
	}
	return nil
}

// Enforce says whether sub may do act on obj in dom, from the rules held in memory.
func (e *Enforcer) Enforce(sub, dom, obj, act string) (bool, error) {
	return e.inner.Enforce(sub, dom, obj, act)
}

// HasPolicy says whether a rule is held in memory.
func (e *Enforcer) HasPolicy(rule []string) (bool, error) {
	return e.inner.HasPolicy(rule)
}

// GetRolesForUserInDomain gives the roles a person holds in an account, from memory.
func (e *Enforcer) GetRolesForUserInDomain(user, domain string) []string {
	return e.inner.GetRolesForUserInDomain(user, domain)
}

// GetGroupingPolicy gives every role assignment held in memory: person, role, account.
func (e *Enforcer) GetGroupingPolicy() ([][]string, error) {
	return e.inner.GetGroupingPolicy()
}

// AddPolicy adds a rule, to the store then to memory. It says whether the rule was new.
func (e *Enforcer) AddPolicy(rule []string) (bool, error) {
	return e.change(func() (bool, error) { return e.inner.AddPolicy(rule) })
}

// AddRoleForUserInDomain gives a person a role in an account, in the store then in memory. A
// role already held in memory is not written again.
func (e *Enforcer) AddRoleForUserInDomain(user, role, domain string) (bool, error) {
	return e.change(func() (bool, error) { return e.inner.AddRoleForUserInDomain(user, role, domain) })
}

// AddNamedGroupingPolicies gives several roles at once. If one of them is already held in
// memory, none is given.
func (e *Enforcer) AddNamedGroupingPolicies(ptype string, rules [][]string) (bool, error) {
	return e.change(func() (bool, error) { return e.inner.AddNamedGroupingPolicies(ptype, rules) })
}

// DeleteRoleForUserInDomain takes one role away from a person in an account. The store is
// asked whether or not the role is held in memory: a role another instance gave is taken away
// too.
func (e *Enforcer) DeleteRoleForUserInDomain(user, role, domain string) (bool, error) {
	return e.change(func() (bool, error) { return e.inner.DeleteRoleForUserInDomain(user, role, domain) })
}

// DeleteRolesForUserInDomain takes away every role of a person in an account, those held in
// memory and those the store alone knows.
func (e *Enforcer) DeleteRolesForUserInDomain(user, domain string) (bool, error) {
	return e.change(func() (bool, error) { return e.inner.RemoveFilteredGroupingPolicy(0, user, "", domain) })
}
