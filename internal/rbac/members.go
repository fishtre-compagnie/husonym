package rbac

import (
	"context"
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

// SetRole gives the role first, then takes the others away: should it fail midway, the member
// holds the role held before, with or without the new one, and never none.
func (s *Service) SetRole(_ context.Context, user User, account Account, role mgmtv1alpha1.AccountRole) error {
	word, ok := roleWord(role)
	if !ok {
		return husonymerrors.NewBadRequest(fmt.Sprintf("%d is not a role a member can be given", role))
	}
	if _, err := s.enforcer.AddRoleForUserInDomain(user.stored(), word, account.stored()); err != nil {
		return fmt.Errorf("unable to give the role %s: %w", word, err)
	}
	for _, other := range roles {
		if other.word == word {
			continue
		}
		if _, err := s.enforcer.DeleteRoleForUserInDomain(user.stored(), other.word, account.stored()); err != nil {
			return fmt.Errorf("the role %s was given, but the role %s could not be taken away: %w", word, other.word, err)
		}
	}
	return nil
}

func (s *Service) RemoveMember(_ context.Context, user User, account Account) error {
	if _, err := s.enforcer.DeleteRolesForUserInDomain(user.stored(), account.stored()); err != nil {
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
	if _, err := s.enforcer.AddNamedGroupingPolicies("g", admins); err != nil {
		return 0, fmt.Errorf("unable to give the role %s to the members of the accounts without roles: %w", roleAdmin, err)
	}
	return len(admins), nil
}
