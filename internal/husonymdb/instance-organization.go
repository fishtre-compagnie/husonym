package husonymdb

import (
	"context"
	"errors"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// instanceOrganizationName is the name of the organization a new instance creates.
const instanceOrganizationName = "organization"

// EntryOutcome says what an entry did.
type EntryOutcome int

const (
	// EntryPersonal: no organization applies, the caller falls back to the personal account.
	EntryPersonal EntryOutcome = iota
	// EntryCreated: the organization was created, the user is its first member.
	EntryCreated
	// EntryJoined: the user was added to the organization.
	EntryJoined
	// EntryMember: the user was already a member.
	EntryMember
)

// InstanceEntry is what an entry did, and where.
type InstanceEntry struct {
	Outcome EntryOutcome
	// AccountId is the organization. It is the zero value for EntryPersonal.
	AccountId pgtype.UUID
}

// RoleSetter gives a user the admin role in an account, or the viewer role. Roles are not
// stored by the transactions of this package: it writes them wherever they are.
type RoleSetter func(ctx context.Context, userId, accountId pgtype.UUID, admin bool) error

// RoleReader tells whether the user holds any role in the account. A member it says holds none
// is given the viewer role, whatever they held: it has to answer from the roles as they are
// stored, not from a copy that may be late.
type RoleReader func(ctx context.Context, userId, accountId pgtype.UUID) (bool, error)

// ErrInstanceOrganizationSet is returned when an organization is designated on an instance
// that already retains one.
var ErrInstanceOrganizationSet = errors.New("this instance already has its organization")

// ErrInstanceOrganizationMissing is returned when the account the instance retains as its
// organization no longer exists.
var ErrInstanceOrganizationMissing = errors.New("the organization of this instance no longer exists")

// GetInstanceOrganization gives the account the instance retains as its organization, and
// whether it retains one.
func (d *HusonymDb) GetInstanceOrganization(ctx context.Context) (pgtype.UUID, bool, error) {
	organization, err := d.Q.GetInstanceOrganization(ctx, d.Db)
	if err != nil {
		return pgtype.UUID{}, false, err
	}
	return organization, organization.Valid, nil
}

// EnterInstance brings a user into the organization of the instance: it creates the
// organization on an instance that has no account at all, and adds the user to the one retained
// otherwise. An instance that has accounts and retains none is left as it is.
//
// The row of the instance is held first, so two first entries at once do not both find no
// account: the second waits, then joins what the first created. That is why the transaction is
// read committed, as in SetPersonalAccount.
//
// A role is not part of the transaction, and is always written before the membership it goes
// with. A member without a role is made an admin when the API starts, if nobody else holds one
// in the account; a role without a membership grants nothing, since membership is checked
// before any role. So when one of the two writes is lost, it has to be the membership.
func (d *HusonymDb) EnterInstance(
	ctx context.Context,
	userId pgtype.UUID,
	setRole RoleSetter,
	hasRole RoleReader,
) (*InstanceEntry, error) {
	var entry *InstanceEntry
	if err := d.WithTx(ctx, &pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(dbtx BaseDBTX) error {
		if _, err := d.Q.LockInstance(ctx, dbtx); err != nil {
			return fmt.Errorf("unable to hold the instance: %w", err)
		}
		organization, err := d.Q.GetInstanceOrganization(ctx, dbtx)
		if err != nil {
			return err
		}
		if organization.Valid {
			entry, err = d.enterOrganization(ctx, dbtx, userId, organization, setRole, hasRole)
			return err
		}

		accounts, err := d.Q.CountAccounts(ctx, dbtx)
		if err != nil {
			return err
		}
		if accounts > 0 {
			entry = &InstanceEntry{Outcome: EntryPersonal}
			return nil
		}
		entry, err = d.createOrganization(ctx, dbtx, userId, setRole)
		return err
	}); err != nil {
		return nil, err
	}
	return entry, nil
}

// enterOrganization adds the user to the organization retained, unless they are in it already.
func (d *HusonymDb) enterOrganization(
	ctx context.Context,
	dbtx BaseDBTX,
	userId, organization pgtype.UUID,
	setRole RoleSetter,
	hasRole RoleReader,
) (*InstanceEntry, error) {
	if _, err := d.Q.GetAccount(ctx, dbtx, organization); err != nil {
		if IsNoRows(err) {
			return nil, fmt.Errorf("%w: account %s", ErrInstanceOrganizationMissing, UUIDString(organization))
		}
		return nil, err
	}
	members, err := d.Q.IsUserInAccount(ctx, dbtx, db_queries.IsUserInAccountParams{
		AccountId: organization,
		UserId:    userId,
	})
	if err != nil {
		return nil, err
	}
	if members > 0 {
		held, err := hasRole(ctx, userId, organization)
		if err != nil {
			return nil, fmt.Errorf("unable to tell whether the member holds a role: %w", err)
		}
		if !held {
			if err := setRole(ctx, userId, organization, false); err != nil {
				return nil, fmt.Errorf("unable to give a role to a member that held none: %w", err)
			}
		}
		return &InstanceEntry{Outcome: EntryMember, AccountId: organization}, nil
	}

	if err := setRole(ctx, userId, organization, false); err != nil {
		return nil, fmt.Errorf("unable to give its role to the new member: %w", err)
	}
	if err := d.Q.CreateAccountUserAssociation(ctx, dbtx, db_queries.CreateAccountUserAssociationParams{
		AccountID: organization,
		UserID:    userId,
	}); err != nil {
		return nil, fmt.Errorf("unable to add the user to the organization: %w", err)
	}
	return &InstanceEntry{Outcome: EntryJoined, AccountId: organization}, nil
}

// createOrganization creates the organization of an instance that has no account, with the user
// as its first member and admin, and has the instance retain it.
//
// The role needs the id of the account, so it is written once the account is created and before
// anything else is. Should what follows fail, the account is rolled back and the role is left
// for an account that does not exist.
func (d *HusonymDb) createOrganization(
	ctx context.Context,
	dbtx BaseDBTX,
	userId pgtype.UUID,
	setRole RoleSetter,
) (*InstanceEntry, error) {
	account, err := d.Q.CreateTeamAccount(ctx, dbtx, instanceOrganizationName)
	if err != nil {
		return nil, fmt.Errorf("unable to create the organization: %w", err)
	}
	if err := setRole(ctx, userId, account.ID, true); err != nil {
		return nil, fmt.Errorf("unable to give its role to the first member: %w", err)
	}
	if err := d.Q.CreateAccountUserAssociation(ctx, dbtx, db_queries.CreateAccountUserAssociationParams{
		AccountID: account.ID,
		UserID:    userId,
	}); err != nil {
		return nil, fmt.Errorf("unable to add the user to the organization: %w", err)
	}
	if err := retainOrganization(ctx, d.Q, dbtx, account.ID); err != nil {
		return nil, err
	}
	return &InstanceEntry{Outcome: EntryCreated, AccountId: account.ID}, nil
}

// retainOrganization has the instance retain the account, which it does only when it retains
// none.
func retainOrganization(ctx context.Context, q db_queries.Querier, dbtx BaseDBTX, accountId pgtype.UUID) error {
	retained, err := q.SetInstanceOrganization(ctx, dbtx, accountId)
	if err != nil {
		return fmt.Errorf("unable to retain the organization: %w", err)
	}
	if retained == 0 {
		return ErrInstanceOrganizationSet
	}
	return nil
}

// DesignateInstanceOrganization has the instance retain an account the user is in as its
// organization, once: it is refused with ErrInstanceOrganizationSet when one is retained.
//
// A personal account becomes a team account under the name given, and keeps its id, so what
// belongs to it stays where it is. Unlike ConvertPersonalToTeamAccount, no personal account is
// made beside it. A team or enterprise account is retained as it is, and the name is not read.
//
// The row of the instance is held first: of two designations at once, the second waits and is
// refused.
func (d *HusonymDb) DesignateInstanceOrganization(
	ctx context.Context,
	userId, accountId pgtype.UUID,
	name string,
) (*db_queries.HusonymApiAccount, error) {
	var designated *db_queries.HusonymApiAccount
	if err := d.WithTx(ctx, &pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(dbtx BaseDBTX) error {
		if _, err := d.Q.LockInstance(ctx, dbtx); err != nil {
			return fmt.Errorf("unable to hold the instance: %w", err)
		}
		organization, err := d.Q.GetInstanceOrganization(ctx, dbtx)
		if err != nil {
			return err
		}
		if organization.Valid {
			return ErrInstanceOrganizationSet
		}

		members, err := d.Q.IsUserInAccount(ctx, dbtx, db_queries.IsUserInAccountParams{
			AccountId: accountId,
			UserId:    userId,
		})
		if err != nil {
			return err
		}
		if members == 0 {
			return husonymerrors.NewNotFound("user is not in the provided account")
		}
		account, err := d.Q.GetAccount(ctx, dbtx, accountId)
		if err != nil {
			return err
		}

		if account.AccountType == int16(AccountType_Personal) {
			if name == "" {
				return husonymerrors.NewBadRequest("a personal account needs a name to become the organization")
			}
			accounts, err := d.Q.GetAccountsByUser(ctx, dbtx, userId)
			if err != nil {
				return err
			}
			if err := verifyAccountNameUnique(accounts, name); err != nil {
				return err
			}
			account, err = d.Q.ConvertPersonalAccountToTeam(ctx, dbtx, db_queries.ConvertPersonalAccountToTeamParams{
				TeamName:  name,
				AccountId: account.ID,
			})
			if err != nil {
				return err
			}
		}

		if err := retainOrganization(ctx, d.Q, dbtx, account.ID); err != nil {
			return err
		}
		designated = &account
		return nil
	}); err != nil {
		return nil, err
	}
	return designated, nil
}
