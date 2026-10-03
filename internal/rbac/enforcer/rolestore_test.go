package enforcer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/fishtre-compagnie/husonym/internal/rbac/sqladapter"
	"github.com/stretchr/testify/require"
)

// tableOfRows is a table of rules that is only read: it gives the rows of the kinds it is asked
// for, and tells what it was asked.
type tableOfRows struct {
	persist.ContextBatchAdapter
	rows  [][]string
	asked []any
}

func (s *tableOfRows) ReplaceAssignmentCtx(context.Context, string, string, string) error {
	return errors.New("the table is only read")
}

func (s *tableOfRows) LoadFilteredPolicyCtx(_ context.Context, m model.Model, filter any) error {
	s.asked = append(s.asked, filter)
	kinds := filter.(*sqladapter.Filter).PType
	for _, row := range s.rows {
		if !slices.Contains(kinds, row[0]) {
			continue
		}
		if err := persist.LoadPolicyArray(row, m); err != nil {
			return err
		}
	}
	return nil
}

func loadedThrough(t *testing.T, table *tableOfRows, fixedRules [][]string) model.Model {
	t.Helper()
	m, err := model.NewModelFromString(modelDocument)
	require.NoError(t, err)
	store := &roleStore{Rows: table, fixedRules: fixedRules, logger: slog.New(slog.DiscardHandler)}
	require.NoError(t, store.LoadPolicyCtx(context.Background(), m))
	return m
}

// Of the table, the role assignments alone are read; the rules are the fixed ones, whatever
// rules the table holds.
func Test_roleStore_LoadsTheAssignmentsAndTheFixedRulesOnly(t *testing.T) {
	table := &tableOfRows{rows: [][]string{
		{"p", "account_admin", "accounts/a", "*", "*"},
		{"p", "job_viewer", "accounts/a", "jobs/*", "delete"},
		{"g", "users/1", "account_admin", "accounts/a"},
		{"g", "users/2", "job_viewer", "accounts/b"},
	}}
	fixedRules := [][]string{{"job_viewer", "*", "jobs/*", "view"}}

	m := loadedThrough(t, table, fixedRules)

	require.Equal(t, []any{&sqladapter.Filter{PType: []string{"g"}}}, table.asked)
	rules, err := m.GetPolicy("p", "p")
	require.NoError(t, err)
	require.Equal(t, fixedRules, rules)
	assignments, err := m.GetPolicy("g", "g")
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"users/1", "account_admin", "accounts/a"},
		{"users/2", "job_viewer", "accounts/b"},
	}, assignments)
}

// A row that is not a person, a role and an account is left out, and the others are read: one
// such row does not cost every member their role.
func Test_roleStore_SkipsARowThatIsNoAssignment(t *testing.T) {
	table := &tableOfRows{rows: [][]string{
		{"g"},
		{"g", "users/1", "account_admin"},
		{"g", "users/2", "job_viewer", "accounts/b"},
		{"g", "users/3", "job_viewer", "accounts/b", "extra"},
	}}

	m := loadedThrough(t, table, nil)

	assignments, err := m.GetPolicy("g", "g")
	require.NoError(t, err)
	require.Equal(t, [][]string{{"users/2", "job_viewer", "accounts/b"}}, assignments)

	enforcer, err := newEnforcer(&boundedStore{
		store: &roleStore{Rows: table, logger: slog.New(slog.DiscardHandler)}, ctx: context.Background(),
		readTimeout: storeReadTimeout, writeTimeout: storeWriteTimeout,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"job_viewer"}, enforcer.GetRolesForUserInDomain("users/2", "accounts/b"))
}

// A role is held in the account its assignment names, and that account is never a pattern: an
// assignment naming every account gives the role nowhere.
func Test_Enforcer_ARoleAssignedToEveryAccountIsHeldNowhere(t *testing.T) {
	enforcer, err := newEnforcer(&slowStore{})
	require.NoError(t, err)
	_, err = enforcer.AddPolicy([]string{"account_admin", "*", "*", "*"})
	require.NoError(t, err)
	for user, account := range map[string]string{"users/1": "*", "users/2": "accounts/*"} {
		_, err = enforcer.AddRoleForUserInDomain(user, "account_admin", account)
		require.NoError(t, err)

		allowed, err := enforcer.Enforce(user, "accounts/a", "jobs/*", "view")
		require.NoError(t, err)
		require.False(t, allowed, "the role assigned in %s is held in accounts/a", account)
		require.Empty(t, enforcer.GetRolesForUserInDomain(user, "accounts/a"))
	}
}

// accountsLoaded gives an enforcer that holds a rule of every account and one member in each
// of a number of accounts.
func accountsLoaded(tb testing.TB, accounts int) *Enforcer {
	tb.Helper()
	store := &slowStore{rules: [][]string{{"p", "job_viewer", "*", "jobs/*", "view"}}}
	for i := range accounts {
		store.rules = append(store.rules, []string{"g", fmt.Sprintf("users/%d", i), "job_viewer", fmt.Sprintf("accounts/%d", i)})
	}
	enforcer, err := newEnforcer(store)
	require.NoError(tb, err)
	return enforcer
}

// What a check costs does not depend on how many accounts there are: in an account where the
// person holds no role, the answer comes without looking at the other accounts.
func Benchmark_Enforce_InAnAccountWithoutRoles(b *testing.B) {
	for _, accounts := range []int{1000, 4000, 8000} {
		enforcer := accountsLoaded(b, accounts)
		b.Run(fmt.Sprintf("%d accounts", accounts), func(b *testing.B) {
			for b.Loop() {
				if allowed, err := enforcer.Enforce("users/0", "accounts/none", "jobs/*", "view"); err != nil || allowed {
					b.Fatal(allowed, err)
				}
			}
		})
	}
}

// What reading the roles again costs grows with their number, and no faster.
func Benchmark_LoadPolicy(b *testing.B) {
	for _, accounts := range []int{1000, 4000, 8000} {
		enforcer := accountsLoaded(b, accounts)
		b.Run(fmt.Sprintf("%d accounts", accounts), func(b *testing.B) {
			for b.Loop() {
				if err := enforcer.LoadPolicy(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A rule written for every account is held by whoever has its role in the account asked about,
// and by nobody else.
func Test_Enforcer_AppliesARuleOfEveryAccountInEachAccount(t *testing.T) {
	enforcer, err := newEnforcer(&slowStore{})
	require.NoError(t, err)
	_, err = enforcer.AddPolicy([]string{"job_viewer", "*", "jobs/*", "view"})
	require.NoError(t, err)
	_, err = enforcer.AddRoleForUserInDomain("users/1", "job_viewer", "accounts/a")
	require.NoError(t, err)

	allowed := func(account, action string) bool {
		ok, err := enforcer.Enforce("users/1", account, "jobs/*", action)
		require.NoError(t, err)
		return ok
	}
	require.True(t, allowed("accounts/a", "view"))
	require.False(t, allowed("accounts/a", "view_sensitive"))
	require.False(t, allowed("accounts/b", "view"))
}
