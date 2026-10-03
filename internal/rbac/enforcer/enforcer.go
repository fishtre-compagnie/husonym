// Package enforcer holds the rules of the access control in memory, answers the checks from
// them, and keeps them in step with the table they are stored in.
package enforcer

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/fishtre-compagnie/husonym/internal/rbac/sqladapter"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

const (
	// tableName is the table the role assignments are stored in.
	tableName = "husonym_api.casbin_rule"
	// reloadPeriod is how often the role assignments are read again from the table, which is
	// how an instance of the API learns of the changes another one made.
	reloadPeriod = 10 * time.Second

	// modelDocument says how a request (person, account, object, action) is decided: it is
	// allowed when a rule grants the action on the object to a role the person holds in that
	// account. A rule names its account, or every account with "*".
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
m = g(r.sub, p.sub, r.dom) && keyMatch(r.dom, p.dom) && keyMatch(r.obj, p.obj) && keyMatch(r.act, p.act)
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
	// reloading is held alone by a reload, and shared by the changes.
	reloading sync.RWMutex
	logger    *slog.Logger
}

// New builds the enforcer on the rows of the rule table and on the rules that are the same in
// every account, loads the role assignments, and reads them again every ten seconds until ctx
// ends. Each read and write of the rows is given a time limit, and ends with ctx.
func New(ctx context.Context, rows Rows, fixedRules [][]string, logger *slog.Logger) (*Enforcer, error) {
	e, err := newEnforcer(&boundedStore{
		store:        &roleStore{Rows: rows, fixedRules: fixedRules, logger: logger},
		ctx:          ctx,
		readTimeout:  storeReadTimeout,
		writeTimeout: storeWriteTimeout,
	})
	if err != nil {
		return nil, err
	}
	e.logger = logger
	go e.reloadEvery(ctx, reloadPeriod)
	return e, nil
}

// OpenRows gives the rows of the rule table of the API database. It fails if the database does
// not answer.
func OpenRows(ctx context.Context, pool *pgxpool.Pool) (Rows, error) {
	return openRows(ctx, stdlib.OpenDBFromPool(pool))
}

func openRows(ctx context.Context, db *sql.DB) (Rows, error) {
	adapter, err := sqladapter.NewAdapterWithContext(ctx, db, "pgx", tableName)
	if err != nil {
		return nil, fmt.Errorf("unable to reach the table of the access rules: %w", err)
	}
	return adapter, nil
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
