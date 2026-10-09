// Package rbac decides what a member of an account may do there, from the role the member
// holds: on the account itself, on its connections, on its jobs.
//
// What each role may do is the same in every account and is written in this package. What is
// stored is who holds which role in which account. It decides for people only, and whether or
// not the instance has a license.
package rbac

import (
	"context"
	"fmt"
	"log/slog"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/rbac/enforcer"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Checker says whether a person may do an action in an account.
type Checker interface {
	// Allowed says whether user may do action in account. Its error is a failure to decide,
	// never a refusal.
	Allowed(ctx context.Context, user User, account Account, action Action) (bool, error)
	// Enforce returns nothing when user may do action in account, and a permission error when
	// not. A failure to decide is the error Allowed gives, which is not a permission error.
	Enforce(ctx context.Context, user User, account Account, action Action) error
}

// Interface is the access control: the checks, and the roles of the members.
type Interface interface {
	Checker
	// SetRole leaves user with that role in account, and no other. A role that is none is
	// refused as an invalid argument, and changes nothing. ErrRoleNotReadBack tells a role
	// that is stored and not yet held on this instance.
	SetRole(ctx context.Context, user User, account Account, role mgmtv1alpha1.AccountRole) error
	// GrantViewerIfNone gives user the viewer role in account when no role is stored for them
	// there, and leaves the role they hold otherwise. ErrRoleNotReadBack means what it means
	// for SetRole.
	GrantViewerIfNone(ctx context.Context, user User, account Account) error
	// RemoveMember takes away every role user holds in account.
	RemoveMember(ctx context.Context, user User, account Account) error
	// SetRoleKeepingAnAdmin is SetRole, refused with ErrLastAdmin, nothing changed, when it
	// would leave account with no administrator.
	SetRoleKeepingAnAdmin(ctx context.Context, user User, account Account, role mgmtv1alpha1.AccountRole) error
	// RemoveMemberKeepingAnAdmin is RemoveMember, refused with ErrLastAdmin, nothing changed,
	// when it would leave account with no administrator. ErrRoleNotReadBack means what it
	// means for SetRole.
	RemoveMemberKeepingAnAdmin(ctx context.Context, user User, account Account) error
	// Roles gives the role of each of the users that has one in account.
	Roles(users []User, account Account) map[User]mgmtv1alpha1.AccountRole
}

// ErrRoleNotReadBack is what SetRole returns when the role is stored and the roles could not be
// read again from the database afterwards. The change is made: the member holds the role on
// this instance once the roles are read again, which they are every ten seconds.
var ErrRoleNotReadBack = enforcer.ErrNotReadBack

// Service is the access control of the API, on the role assignments its database stores.
type Service struct {
	enforcer *enforcer.Enforcer
	logger   *slog.Logger
}

var _ Interface = (*Service)(nil)

// New builds the access control on the API database, loads the role assignments, and keeps
// them in step with the database until ctx ends. It fails if the database does not answer.
func New(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) (*Service, error) {
	rows, err := enforcer.OpenRows(ctx, pool)
	if err != nil {
		return nil, err
	}
	return newService(ctx, rows, logger)
}

func newService(ctx context.Context, rows enforcer.Rows, logger *slog.Logger) (*Service, error) {
	e, err := enforcer.New(ctx, rows, fixedRules(), logger)
	if err != nil {
		return nil, err
	}
	return &Service{enforcer: e, logger: logger}, nil
}

func (s *Service) Allowed(_ context.Context, user User, account Account, action Action) (bool, error) {
	allowed, err := s.enforcer.Enforce(user.stored(), account.stored(), action.object(account), action.String())
	if err != nil {
		return false, fmt.Errorf("unable to decide whether the user may %s %s: %w", action, action.Kind(), err)
	}
	return allowed, nil
}

func (s *Service) Enforce(ctx context.Context, user User, account Account, action Action) error {
	allowed, err := s.Allowed(ctx, user, account, action)
	if err != nil {
		return err
	}
	if !allowed {
		return husonymerrors.NewForbidden(
			fmt.Sprintf("user does not have permission to %s %s", action, action.Kind()),
		)
	}
	return nil
}
