package enforcer

import (
	"context"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
)

const (
	// storeReadTimeout is how long the rules may take to be read again from their store.
	storeReadTimeout = 30 * time.Second
	// storeWriteTimeout is how long a change of the rules may take to reach their store.
	storeWriteTimeout = 10 * time.Second
)

// boundedStore is the store of the rules, each of whose reads and writes is waited for a
// limited time. The enforcer keeps its rules locked while it writes one to the store, and
// keeps their changes waiting while it reads them again: on a store that never answers, a
// write would hold every check of a permission, and a read every change of a role, without
// end.
//
// It offers what the enforcer of the API asks of a store, and no other: a call around it
// would not be limited.
type boundedStore struct {
	store persist.ContextBatchAdapter
	// ctx is the context the store is used in: it ends with the server.
	ctx          context.Context
	readTimeout  time.Duration
	writeTimeout time.Duration
}

var _ persist.BatchAdapter = (*boundedStore)(nil)

func (s *boundedStore) within(limit time.Duration, call func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(s.ctx, limit)
	defer cancel()
	return call(ctx)
}

func (s *boundedStore) LoadPolicy(m model.Model) error {
	return s.within(s.readTimeout, func(ctx context.Context) error { return s.store.LoadPolicyCtx(ctx, m) })
}

func (s *boundedStore) SavePolicy(m model.Model) error {
	return s.within(s.writeTimeout, func(ctx context.Context) error { return s.store.SavePolicyCtx(ctx, m) })
}

func (s *boundedStore) AddPolicy(sec, ptype string, rule []string) error {
	return s.within(s.writeTimeout, func(ctx context.Context) error {
		return s.store.AddPolicyCtx(ctx, sec, ptype, rule)
	})
}

func (s *boundedStore) AddPolicies(sec, ptype string, rules [][]string) error {
	return s.within(s.writeTimeout, func(ctx context.Context) error {
		return s.store.AddPoliciesCtx(ctx, sec, ptype, rules)
	})
}

func (s *boundedStore) RemovePolicy(sec, ptype string, rule []string) error {
	return s.within(s.writeTimeout, func(ctx context.Context) error {
		return s.store.RemovePolicyCtx(ctx, sec, ptype, rule)
	})
}

func (s *boundedStore) RemovePolicies(sec, ptype string, rules [][]string) error {
	return s.within(s.writeTimeout, func(ctx context.Context) error {
		return s.store.RemovePoliciesCtx(ctx, sec, ptype, rules)
	})
}

func (s *boundedStore) RemoveFilteredPolicy(sec, ptype string, fieldIndex int, fieldValues ...string) error {
	return s.within(s.writeTimeout, func(ctx context.Context) error {
		return s.store.RemoveFilteredPolicyCtx(ctx, sec, ptype, fieldIndex, fieldValues...)
	})
}
