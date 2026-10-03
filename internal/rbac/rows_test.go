package rbac

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/fishtre-compagnie/husonym/internal/rbac/enforcer"
	"github.com/fishtre-compagnie/husonym/internal/rbac/sqladapter"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// memoryRows is the table of the rules, held in memory: each row is its kind followed by its
// values, as the table stores them. It counts the writes it is asked, and can refuse its writes
// or its reads.
type memoryRows struct {
	mu     sync.Mutex
	rows   [][]string
	writes int
	// failingWrites fails every change of the rows, failingReads every read of them.
	failingWrites, failingReads error
}

var _ enforcer.Rows = (*memoryRows)(nil)

// fail makes the table refuse its writes, its reads, or neither.
func (s *memoryRows) fail(writes, reads error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failingWrites, s.failingReads = writes, reads
}

// ReplaceAssignmentCtx leaves the person one role in the account, all at once.
func (s *memoryRows) ReplaceAssignmentCtx(_ context.Context, user, role, account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.failingWrites != nil {
		return s.failingWrites
	}
	s.rows = slices.DeleteFunc(s.rows, func(stored []string) bool {
		return stored[0] == "g" && len(stored) == 4 && stored[1] == user && stored[3] == account
	})
	s.rows = append(s.rows, []string{"g", user, role, account})
	return nil
}

func (s *memoryRows) stored() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rows)
}

// write puts a row in the table without the service knowing, as another instance of the API
// does.
func (s *memoryRows) write(row ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, row)
}

// erase takes a row out of the table without the service knowing.
func (s *memoryRows) erase(row ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = slices.DeleteFunc(s.rows, func(stored []string) bool { return slices.Equal(stored, row) })
}

// storedFor gives the role assignments of a person, in every account.
func (s *memoryRows) storedFor(user User) [][]string {
	var rows [][]string
	for _, row := range s.stored() {
		if row[0] == "g" && row[1] == user.stored() {
			rows = append(rows, row)
		}
	}
	return rows
}

func (s *memoryRows) LoadPolicyCtx(context.Context, model.Model) error {
	return errors.New("the whole table is never read")
}

func (s *memoryRows) LoadFilteredPolicyCtx(_ context.Context, m model.Model, filter any) error {
	s.mu.Lock()
	failing := s.failingReads
	s.mu.Unlock()
	if failing != nil {
		return failing
	}
	kinds := filter.(*sqladapter.Filter).PType
	for _, row := range s.stored() {
		if !slices.Contains(kinds, row[0]) {
			continue
		}
		if err := persist.LoadPolicyArray(row, m); err != nil {
			return err
		}
	}
	return nil
}

func (s *memoryRows) SavePolicyCtx(context.Context, model.Model) error {
	return errors.New("the table is never written whole")
}

func (s *memoryRows) AddPolicyCtx(_ context.Context, _, ptype string, rule []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	row := append([]string{ptype}, rule...)
	if !slices.ContainsFunc(s.rows, func(stored []string) bool { return slices.Equal(stored, row) }) {
		s.rows = append(s.rows, row)
	}
	return nil
}

func (s *memoryRows) AddPoliciesCtx(ctx context.Context, sec, ptype string, rules [][]string) error {
	for _, rule := range rules {
		if err := s.AddPolicyCtx(ctx, sec, ptype, rule); err != nil {
			return err
		}
	}
	return nil
}

func (s *memoryRows) RemovePolicyCtx(_ context.Context, _, ptype string, rule []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.failingWrites != nil {
		return s.failingWrites
	}
	row := append([]string{ptype}, rule...)
	s.rows = slices.DeleteFunc(s.rows, func(stored []string) bool { return slices.Equal(stored, row) })
	return nil
}

func (s *memoryRows) RemovePoliciesCtx(ctx context.Context, sec, ptype string, rules [][]string) error {
	for _, rule := range rules {
		if err := s.RemovePolicyCtx(ctx, sec, ptype, rule); err != nil {
			return err
		}
	}
	return nil
}

func (s *memoryRows) RemoveFilteredPolicyCtx(_ context.Context, _, ptype string, fieldIndex int, fieldValues ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.failingWrites != nil {
		return s.failingWrites
	}
	s.rows = slices.DeleteFunc(s.rows, func(stored []string) bool {
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

// serviceOn builds the access control on a table, as the API does on that of its database.
func serviceOn(t *testing.T, rows *memoryRows) *Service {
	t.Helper()
	service, err := newService(t.Context(), rows, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	return service
}

func someone() User        { return NewUser(uuid.NewString()) }
func someAccount() Account { return NewAccount(uuid.NewString()) }
func assignment(user User, role string, account Account) []string {
	return []string{"g", user.stored(), role, account.stored()}
}
