package enforcer

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/stretchr/testify/require"
)

// slowStore is a store of rules whose reads take the time the test gives them, as the reads
// of a database do.
type slowStore struct {
	mu    sync.Mutex
	rules [][]string
	// failing fails the reads.
	failing error

	// reading is told a read that has its rules, which read then lets end.
	reading chan struct{}
	read    chan struct{}
}

var _ persist.BatchAdapter = (*slowStore)(nil)

func (s *slowStore) LoadPolicy(m model.Model) error {
	s.mu.Lock()
	rules, failing, reading := slices.Clone(s.rules), s.failing, s.reading
	s.mu.Unlock()
	if failing != nil {
		return failing
	}
	if reading != nil {
		reading <- struct{}{}
		<-s.read
	}
	for _, rule := range rules {
		if err := persist.LoadPolicyArray(rule, m); err != nil {
			return err
		}
	}
	return nil
}

func (s *slowStore) SavePolicy(model.Model) error { return nil }

func (s *slowStore) AddPolicy(_, ptype string, rule []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, append([]string{ptype}, rule...))
	return nil
}

func (s *slowStore) RemovePolicy(_, ptype string, rule []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := append([]string{ptype}, rule...)
	s.rules = slices.DeleteFunc(s.rules, func(stored []string) bool { return slices.Equal(stored, removed) })
	return nil
}

func (s *slowStore) AddPolicies(sec, ptype string, rules [][]string) error {
	for _, rule := range rules {
		if err := s.AddPolicy(sec, ptype, rule); err != nil {
			return err
		}
	}
	return nil
}

func (s *slowStore) RemovePolicies(sec, ptype string, rules [][]string) error {
	for _, rule := range rules {
		if err := s.RemovePolicy(sec, ptype, rule); err != nil {
			return err
		}
	}
	return nil
}

func (s *slowStore) RemoveFilteredPolicy(_, ptype string, fieldIndex int, fieldValues ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = slices.DeleteFunc(s.rules, func(stored []string) bool {
		if stored[0] != ptype {
			return false
		}
		for i, value := range fieldValues {
			if value != "" && stored[1+fieldIndex+i] != value {
				return false
			}
		}
		return true
	})
	return nil
}

// slow makes the reads of the store wait for the test from now on.
func (s *slowStore) slow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reading, s.read = make(chan struct{}), make(chan struct{})
}

// overtakenReload reloads the rules, and makes a change of them once the reload has read the
// store, before it has put what it read in place.
func overtakenReload(t *testing.T, store *slowStore, enforcer *Enforcer, change func() error) {
	t.Helper()
	store.slow()
	reloaded := make(chan error, 1)
	go func() { reloaded <- enforcer.LoadPolicy() }()
	<-store.reading

	changed, asked := make(chan error, 1), make(chan struct{})
	go func() {
		close(asked)
		changed <- change()
	}()
	// The change waits for the reload, or is made beside it: either way the reload ends after
	// the change was asked for.
	<-asked
	time.Sleep(50 * time.Millisecond)
	close(store.read)
	require.NoError(t, <-reloaded)
	require.NoError(t, <-changed)
}

var adminOfAccount = []string{"account_admin", "account/1", "*", "*"}

// A rule added while the rules are read again from the store is kept: the reload, which read
// the store before the rule was added, does not take it away from those the enforcer knows.
func Test_Enforcer_KeepsARuleAddedWhileItReloads(t *testing.T) {
	store := &slowStore{}
	enforcer, err := newEnforcer(store)
	require.NoError(t, err)

	// An account is created.
	overtakenReload(t, store, enforcer, func() error {
		_, err := enforcer.AddPolicy(adminOfAccount)
		return err
	})

	known, err := enforcer.HasPolicy(adminOfAccount)
	require.NoError(t, err)
	require.True(t, known, "the rule is in the store, and the enforcer no longer knows it")
}

// The same of a role: given while the rules are read again, it is kept; taken away, it is not
// given back.
func Test_Enforcer_KeepsTheRolesChangedWhileItReloads(t *testing.T) {
	store := &slowStore{}
	enforcer, err := newEnforcer(store)
	require.NoError(t, err)
	_, err = enforcer.AddPolicy(adminOfAccount)
	require.NoError(t, err)
	allowed := func() bool {
		ok, err := enforcer.Enforce("user/1", "account/1", "job/1", "delete")
		require.NoError(t, err)
		return ok
	}
	require.False(t, allowed())

	overtakenReload(t, store, enforcer, func() error {
		_, err := enforcer.AddRoleForUserInDomain("user/1", "account_admin", "account/1")
		return err
	})
	require.True(t, allowed(), "the role was given, and the user is refused")
	require.Equal(t, []string{"account_admin"}, enforcer.GetRolesForUserInDomain("user/1", "account/1"))

	overtakenReload(t, store, enforcer, func() error {
		_, err := enforcer.DeleteRolesForUserInDomain("user/1", "account/1")
		return err
	})
	require.False(t, allowed(), "the role was taken away, and the user is allowed again")

	_, err = enforcer.AddNamedGroupingPolicies("g", [][]string{{"user/1", "account_admin", "account/1"}})
	require.NoError(t, err)
	overtakenReload(t, store, enforcer, func() error {
		_, err := enforcer.DeleteRoleForUserInDomain("user/1", "account_admin", "account/1")
		return err
	})
	require.False(t, allowed(), "the role was taken away, and the user is allowed again")

	overtakenReload(t, store, enforcer, func() error {
		_, err := enforcer.AddNamedGroupingPolicies("g", [][]string{{"user/1", "account_admin", "account/1"}})
		return err
	})
	require.True(t, allowed(), "the role was given, and the user is refused")
}

// The rules another instance of the API writes to the store are known once read again, which
// they are without end until the context ends.
func Test_Enforcer_ReloadsUntilItsContextEnds(t *testing.T) {
	store := &slowStore{}
	enforcer, err := newEnforcer(store)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		enforcer.reloadEvery(ctx, 10*time.Millisecond)
	}()

	require.NoError(t, store.AddPolicy("p", "p", adminOfAccount))
	require.Eventually(t, func() bool {
		known, err := enforcer.HasPolicy(adminOfAccount)
		return err == nil && known
	}, 5*time.Second, 10*time.Millisecond, "the rule written to the store was not read again")

	cancel()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the reloads outlive their context")
	}
}

// A reload that fails leaves the rules known in place.
func Test_Enforcer_KeepsItsRulesWhenAReloadFails(t *testing.T) {
	store := &slowStore{}
	enforcer, err := newEnforcer(store)
	require.NoError(t, err)
	_, err = enforcer.AddPolicy(adminOfAccount)
	require.NoError(t, err)

	down := errors.New("the database is down")
	store.mu.Lock()
	store.failing = down
	store.mu.Unlock()

	require.ErrorIs(t, enforcer.LoadPolicy(), down)
	known, err := enforcer.HasPolicy(adminOfAccount)
	require.NoError(t, err)
	require.True(t, known)
}
