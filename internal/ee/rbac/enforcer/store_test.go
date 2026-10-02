package enforcer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/stretchr/testify/require"
)

// silentStore is a store that never answers: each call is held until its context ends. It
// tells the calls it was asked.
type silentStore struct {
	asked []string
}

var _ persist.ContextBatchAdapter = (*silentStore)(nil)

func (s *silentStore) held(ctx context.Context, call string) error {
	s.asked = append(s.asked, call)
	<-ctx.Done()
	return ctx.Err()
}

func (s *silentStore) LoadPolicyCtx(ctx context.Context, _ model.Model) error {
	return s.held(ctx, "load")
}
func (s *silentStore) SavePolicyCtx(ctx context.Context, _ model.Model) error {
	return s.held(ctx, "save")
}
func (s *silentStore) AddPolicyCtx(ctx context.Context, _, _ string, _ []string) error {
	return s.held(ctx, "add")
}
func (s *silentStore) AddPoliciesCtx(ctx context.Context, _, _ string, _ [][]string) error {
	return s.held(ctx, "add many")
}
func (s *silentStore) RemovePolicyCtx(ctx context.Context, _, _ string, _ []string) error {
	return s.held(ctx, "remove")
}
func (s *silentStore) RemovePoliciesCtx(ctx context.Context, _, _ string, _ [][]string) error {
	return s.held(ctx, "remove many")
}
func (s *silentStore) RemoveFilteredPolicyCtx(ctx context.Context, _, _ string, _ int, _ ...string) error {
	return s.held(ctx, "remove filtered")
}

// Each read and each write of the store ends at its limit on a store that never answers.
func Test_boundedStore_EndsEachCallAtItsLimit(t *testing.T) {
	silent := &silentStore{}
	store := &boundedStore{
		store: silent, ctx: context.Background(), readTimeout: 20 * time.Millisecond, writeTimeout: 40 * time.Millisecond,
	}
	rule := []string{"account_admin", "account/1", "*", "*"}
	calls := []struct {
		name  string
		limit time.Duration
		call  func() error
	}{
		{"load", store.readTimeout, func() error { return store.LoadPolicy(nil) }},
		{"save", store.writeTimeout, func() error { return store.SavePolicy(nil) }},
		{"add", store.writeTimeout, func() error { return store.AddPolicy("p", "p", rule) }},
		{"add many", store.writeTimeout, func() error { return store.AddPolicies("p", "p", [][]string{rule}) }},
		{"remove", store.writeTimeout, func() error { return store.RemovePolicy("p", "p", rule) }},
		{"remove many", store.writeTimeout, func() error { return store.RemovePolicies("p", "p", [][]string{rule}) }},
		{"remove filtered", store.writeTimeout, func() error { return store.RemoveFilteredPolicy("g", "g", 0, "user/1") }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			answered := make(chan error, 1)
			start := time.Now()
			go func() { answered <- tc.call() }()
			select {
			case err := <-answered:
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.GreaterOrEqual(t, time.Since(start), tc.limit, "the call ended before its limit")
			case <-time.After(10 * time.Second):
				require.FailNow(t, "the call is still waiting for a store that never answers")
			}
			require.Equal(t, tc.name, silent.asked[len(silent.asked)-1], "the call reached another call of the store")
		})
	}
}

// A store that ends with the server ends its calls with it.
func Test_boundedStore_EndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &boundedStore{store: &silentStore{}, ctx: ctx, readTimeout: time.Hour, writeTimeout: time.Hour}

	require.ErrorIs(t, store.LoadPolicy(nil), context.Canceled)
}

// The enforcer of the API works on a store that is limited: a rule that cannot be written is
// not known, and the rules stay checked.
func Test_Enforcer_OnAStoreThatNeverAnswers(t *testing.T) {
	enforcer, err := newEnforcer(&slowStore{})
	require.NoError(t, err)
	_, err = enforcer.AddPolicy(adminOfAccount)
	require.NoError(t, err)

	silent := &boundedStore{
		store: &silentStore{}, ctx: context.Background(), readTimeout: 20 * time.Millisecond, writeTimeout: 20 * time.Millisecond,
	}
	enforcer.inner.SetAdapter(silent)

	_, err = enforcer.AddRoleForUserInDomain("user/1", "account_admin", "account/1")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	allowed, err := enforcer.Enforce("user/1", "account/1", "job/1", "delete")
	require.NoError(t, err)
	require.False(t, allowed, "a role the store did not take is known")
	require.True(t, errors.Is(enforcer.LoadPolicy(), context.DeadlineExceeded))
	known, err := enforcer.HasPolicy(adminOfAccount)
	require.NoError(t, err)
	require.True(t, known, "the rules known were lost with a reload that failed")
}
