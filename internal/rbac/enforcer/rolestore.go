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

// Rows is the table of the rules, as the enforcer asks it: it writes rows to it, reads the
// rows of the kinds it names, and replaces the role of a person in an account.
type Rows interface {
	persist.ContextBatchAdapter
	LoadFilteredPolicyCtx(ctx context.Context, m model.Model, filter any) error
	// ReplaceAssignmentCtx leaves the person that role in the account and no other, all at
	// once: the table never holds the person without a role meanwhile.
	ReplaceAssignmentCtx(ctx context.Context, user, role, account string) error
}

// FirstAssignments is the table of the rules, asked to give a role to a person that holds none
// in an account. The table of the API database answers it.
type FirstAssignments interface {
	// HasAssignmentCtx tells whether the table holds a role for the person in the account.
	HasAssignmentCtx(ctx context.Context, user, account string) (bool, error)
	// AddAssignmentIfNoneCtx gives the person that role in the account when the table holds
	// none for them there, and says whether it did. It never replaces a role, and of two asked
	// at once only one writes.
	AddAssignmentIfNoneCtx(ctx context.Context, user, role, account string) (bool, error)
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
	// failing the load, which would cost every member their role. Read aside, a row may be of
	// any size, down to its kind alone: its size is checked below.
	read := m.Copy()
	aside, err := read.GetAssertion(assignmentKind, assignmentKind)
	if err != nil {
		return err
	}
	aside.Tokens = aside.Tokens[:0]
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
