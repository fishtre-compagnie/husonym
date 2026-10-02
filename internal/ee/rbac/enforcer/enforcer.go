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
	sqladapter "github.com/fishtre-compagnie/husonym/internal/ee/rbac/sqladapter"
)

// reloadInterval is how often the rules are read again from their store: it allows HA between
// husonym-api instances, and changes made to the rules in the store to be picked up.
const reloadInterval = 10 * time.Second

// Enforcer is the enforcer of the API: a casbin enforcer whose rules live in a store, and are
// read again from it now and then.
//
// casbin reads the rules again in two steps: it reads the store, then puts what it read in
// place of the rules it knows. A rule added between the two — an account that is created, a
// role that is given — is in the store, and no longer among the rules known: its user is
// refused until the next reload. A rule removed is, the same way, known again until then.
// Enforcer has a reload and a change of the rules wait for one another.
//
// It offers the calls the API makes, and no other: a change of the rules made around it would
// not wait for a reload.
type Enforcer struct {
	inner *casbin.SyncedEnforcer

	// changes is held by a reload, alone, from its read of the store until what it read is in
	// place, and by each change of the rules, with the other changes, while it is made.
	changes sync.RWMutex
}

// The default casbin enforcer with a SQL-enabled backend. Its rules are read again from the
// database until the context ends.
func NewActiveEnforcer(
	ctx context.Context,
	db *sql.DB,
	casbinTableName string,
) (*Enforcer, error) {
	adapter, err := newSqlAdapter(ctx, db, casbinTableName)
	if err != nil {
		return nil, err
	}
	enforcer, err := newEnforcer(adapter)
	if err != nil {
		return nil, err
	}
	go enforcer.reloadEvery(ctx, reloadInterval)
	return enforcer, nil
}

func newEnforcer(
	adapter persist.Adapter,
) (*Enforcer, error) {
	m, err := model.NewModelFromString(husonymRbacModel)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize casbin model from string: %w", err)
	}

	inner, err := casbin.NewSyncedEnforcer(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize casbin synced cached enforcer: %w", err)
	}
	inner.EnableAutoSave(
		true,
	) // seems to do this automatically but it doesn't hurt
	return &Enforcer{inner: inner}, nil
}

// reloadEvery reads the rules again from their store at each interval, until the context ends.
func (e *Enforcer) reloadEvery(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// The rules known stay in place: the next reload may fare better.
			if err := e.LoadPolicy(); err != nil && ctx.Err() == nil {
				slog.Default().Warn("unable to reload the rbac policies", "error", err)
			}
		}
	}
}

// LoadPolicy reads the rules again from their store. No rule changes meanwhile.
func (e *Enforcer) LoadPolicy() error {
	e.changes.Lock()
	defer e.changes.Unlock()
	return e.inner.LoadPolicy()
}

func (e *Enforcer) Enforce(rvals ...any) (bool, error) {
	return e.inner.Enforce(rvals...)
}

func (e *Enforcer) HasPolicy(params ...any) (bool, error) {
	return e.inner.HasPolicy(params...)
}

func (e *Enforcer) GetNamedGroupingPolicy(ptype string) ([][]string, error) {
	return e.inner.GetNamedGroupingPolicy(ptype)
}

func (e *Enforcer) GetRolesForUserInDomain(name, domain string) []string {
	return e.inner.GetRolesForUserInDomain(name, domain)
}

func (e *Enforcer) AddPolicy(params ...any) (bool, error) {
	e.changes.RLock()
	defer e.changes.RUnlock()
	return e.inner.AddPolicy(params...)
}

func (e *Enforcer) AddNamedGroupingPolicies(ptype string, rules [][]string) (bool, error) {
	e.changes.RLock()
	defer e.changes.RUnlock()
	return e.inner.AddNamedGroupingPolicies(ptype, rules)
}

func (e *Enforcer) AddRoleForUserInDomain(user, role, domain string) (bool, error) {
	e.changes.RLock()
	defer e.changes.RUnlock()
	return e.inner.AddRoleForUserInDomain(user, role, domain)
}

func (e *Enforcer) DeleteRoleForUserInDomain(user, role, domain string) (bool, error) {
	e.changes.RLock()
	defer e.changes.RUnlock()
	return e.inner.DeleteRoleForUserInDomain(user, role, domain)
}

func (e *Enforcer) DeleteRolesForUserInDomain(user, domain string) (bool, error) {
	e.changes.RLock()
	defer e.changes.RUnlock()
	return e.inner.DeleteRolesForUserInDomain(user, domain)
}

func newSqlAdapter(
	ctx context.Context,
	db *sql.DB,
	tableName string,
) (persist.Adapter, error) {
	adapter, err := sqladapter.NewAdapterWithContext(ctx, db, "postgres", tableName)
	if err != nil {
		return nil, fmt.Errorf("unable to create casbin sql adapter: %w", err)
	}
	return &boundedStore{
		store: adapter, ctx: ctx, readTimeout: storeReadTimeout, writeTimeout: storeWriteTimeout,
	}, nil
}
