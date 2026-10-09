package rbac

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/rbac/enforcer"
)

// SetRole replaces the role in the table all at once, whatever this instance believed the
// member held, then reads the roles again: once it returns nil, the table held that role for
// the member and no other, and this instance decides from it. Should the table refuse, the
// member holds what they held. Two changes for one member, on this instance or on two, are
// made one after the other: the member ends with the role of one of them, never with none.
func (s *Service) SetRole(_ context.Context, user User, account Account, role mgmtv1alpha1.AccountRole) error {
	word, ok := roleWord(role)
	if !ok {
		return husonymerrors.NewBadRequest(fmt.Sprintf("%d is not a role a member can be given", role))
	}
	if err := s.enforcer.SetRoleForUserInDomain(user.stored(), word, account.stored()); err != nil {
		return fmt.Errorf("unable to give the role %s: %w", word, err)
	}
	return nil
}

// GrantViewerIfNone decides from the table, not from what this instance believes the person
// holds, and in one step the table makes alone: a role another instance gave is never replaced,
// and of two asked at once one writes. Where a role is held it costs one read of the table and
// writes nothing; the roles are read again only when this instance holds none of what the table
// holds for that person, so that they are let in here at once.
func (s *Service) GrantViewerIfNone(_ context.Context, user User, account Account) error {
	if err := s.enforcer.SetRoleForUserInDomainIfNone(user.stored(), roleViewer, account.stored()); err != nil {
		return fmt.Errorf("unable to give the role %s to who holds none: %w", roleViewer, err)
	}
	return nil
}

func (s *Service) RemoveMember(_ context.Context, user User, account Account) error {
	if _, err := s.enforcer.DeleteRolesForUserInDomain(user.stored(), account.stored()); err != nil {
		return fmt.Errorf("unable to take the roles of the member away: %w", err)
	}
	return nil
}

// ErrLastAdmin is what SetRoleKeepingAnAdmin and RemoveMemberKeepingAnAdmin return when the
// change would leave the account with no administrator. Nothing has changed then.
var ErrLastAdmin = enforcer.ErrLastHolder

// SetRoleKeepingAnAdmin is SetRole, refused with ErrLastAdmin when the member is the only
// administrator the table holds for the account and the role is another. The table decides, in
// the transaction that changes the role: of two administrators who demote each other at once, on
// this instance or on two, one is refused.
func (s *Service) SetRoleKeepingAnAdmin(_ context.Context, user User, account Account, role mgmtv1alpha1.AccountRole) error {
	word, ok := roleWord(role)
	if !ok {
		return husonymerrors.NewBadRequest(fmt.Sprintf("%d is not a role a member can be given", role))
	}
	if err := s.enforcer.SetRoleForUserInDomainKeeping(user.stored(), word, account.stored(), roleAdmin); err != nil {
		return fmt.Errorf("unable to give the role %s: %w", word, err)
	}
	return nil
}

// RemoveMemberKeepingAnAdmin is RemoveMember, refused with ErrLastAdmin when the member is the
// only administrator the table holds for the account, and decided as SetRoleKeepingAnAdmin is.
// ErrRoleNotReadBack tells roles that are taken away in the table and still held on this
// instance until the roles are read again.
func (s *Service) RemoveMemberKeepingAnAdmin(_ context.Context, user User, account Account) error {
	if err := s.enforcer.DeleteRolesForUserInDomainKeeping(user.stored(), account.stored(), roleAdmin); err != nil {
		return fmt.Errorf("unable to take the roles of the member away: %w", err)
	}
	return nil
}

// Roles gives, of a member that holds several roles, the one that may do the most: it is what
// the member may do.
func (s *Service) Roles(users []User, account Account) map[User]mgmtv1alpha1.AccountRole {
	held := make(map[User]mgmtv1alpha1.AccountRole, len(users))
	for _, user := range users {
		words := s.enforcer.GetRolesForUserInDomain(user.stored(), account.stored())
		if len(words) > 1 {
			s.logger.Warn("a member holds several roles in an account", "user", user.id, "account", account.id, "roles", words)
		}
		if role := highestRole(words); role != mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED {
			held[user] = role
		}
	}
	return held
}

// Accounts tells the accounts there are, and the people who are members of each.
type Accounts interface {
	Accounts(ctx context.Context) ([]Account, error)
	// HumanMembers gives the members of an account that are people, not API keys.
	HumanMembers(ctx context.Context, account Account) ([]User, error)
}

// GrantAdminWhereNoRole makes admin every person that is a member of an account where nobody
// holds a role, and says how many were. An account where somebody holds a role is left as it
// is. It is how an account that has members and no roles — one older than the roles, one whose
// creation stopped before its creator was given theirs — gets an admin. Done again, it gives
// nothing more.
func (s *Service) GrantAdminWhereNoRole(ctx context.Context, accounts Accounts) (int, error) {
	all, err := accounts.Accounts(ctx)
	if err != nil {
		return 0, fmt.Errorf("unable to list the accounts: %w", err)
	}
	assignments, err := s.enforcer.GetGroupingPolicy()
	if err != nil {
		return 0, fmt.Errorf("unable to read the roles held: %w", err)
	}
	withRoles := make(map[string]bool, len(assignments))
	for _, assignment := range assignments {
		withRoles[assignment[2]] = true
	}

	var admins [][]string
	for _, account := range all {
		if withRoles[account.stored()] {
			continue
		}
		members, err := accounts.HumanMembers(ctx, account)
		if err != nil {
			return 0, fmt.Errorf("unable to list the members of the account %s: %w", account.id, err)
		}
		for _, member := range members {
			admins = append(admins, []string{member.stored(), roleAdmin, account.stored()})
		}
	}
	if len(admins) == 0 {
		return 0, nil
	}
	given, err := s.enforcer.AddNamedGroupingPolicies("g", admins)
	if err != nil {
		return 0, fmt.Errorf("unable to give the role %s to the members of the accounts without roles: %w", roleAdmin, err)
	}
	if !given {
		// One of them was given a role meanwhile: nothing was written, and the next start
		// looks again.
		return 0, nil
	}
	return len(admins), nil
}
