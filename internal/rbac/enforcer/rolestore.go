package enforcer

import (
	"context"
	"log/slog"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/fishtre-compagnie/husonym/internal/rbac/sqladapter"
)

const (
	ruleKind       = "p"
	assignmentKind = "g"
	// assignmentSize is what a role assignment is made of: a person, a role, an account.
	assignmentSize = 3
)

// Rows is the table of the rules, as the enforcer asks it: it writes rows to it, and reads the
// rows of the kinds it names.
type Rows interface {
	persist.ContextBatchAdapter
	LoadFilteredPolicyCtx(ctx context.Context, m model.Model, filter any) error
}

// roleStore is the store the enforcer loads from: the role assignments of the table, and the
// rules that are the same in every account, which are not stored. Rule rows the table may hold
// are not read.
type roleStore struct {
	Rows
	fixedRules [][]string
	logger     *slog.Logger
}

func (s *roleStore) LoadPolicyCtx(ctx context.Context, m model.Model) error {
	// The rows are read aside, so that one that is no assignment is left out instead of
	// failing the load, which would cost every member their role. Read aside, a row needs a
	// first value only: its size is checked below. A row with no value at all still fails.
	read := m.Copy()
	aside, err := read.GetAssertion(assignmentKind, assignmentKind)
	if err != nil {
		return err
	}
	aside.Tokens = aside.Tokens[:1]
	if err := s.LoadFilteredPolicyCtx(ctx, read, &sqladapter.Filter{PType: []string{assignmentKind}}); err != nil {
		return err
	}
	assignments, err := read.GetPolicy(assignmentKind, assignmentKind)
	if err != nil {
		return err
	}
	for _, assignment := range assignments {
		if len(assignment) != assignmentSize {
			s.logger.WarnContext(ctx, "a role assignment is not a person, a role and an account: ignored", "row", assignment)
			continue
		}
		if err := m.AddPolicy(assignmentKind, assignmentKind, assignment); err != nil {
			return err
		}
	}
	for _, rule := range s.fixedRules {
		if err := m.AddPolicy(ruleKind, ruleKind, rule); err != nil {
			return err
		}
	}
	return nil
}
