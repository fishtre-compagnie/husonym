package husonymdb

import (
	"context"
	"errors"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/google/uuid"
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

// InstanceRoles is what an entry needs of the role store. Roles are not stored by the
// transactions of this package: they are written wherever they are, outside of them.
type InstanceRoles interface {
	// GrantAdmin gives the administrator role.
	GrantAdmin(ctx context.Context, userId, accountId pgtype.UUID) error
	// GrantViewerIfNone gives the viewer role only where the stored roles hold none for this
	// user in this account. It never replaces a role.
	GrantViewerIfNone(ctx context.Context, userId, accountId pgtype.UUID) error
}

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
// organization on an instance that is new for people, and adds the user to the one retained
// otherwise. An instance where people have accounts and that retains none is left as it is.
//
// An instance is new for people when no account has a person among its members, a person being a
// user an identity provider vouches for. The account of the anonymous user, which is there before
// anybody signs in on an instance that was seeded or first ran without authentication, does not
// make it an instance that has accounts; nor does the account of the user of an API key. The
// user who enters has an association already and no account yet: they do not count either.
//
// An entry is made on every page load, so it holds nothing on the instance once an
// organization is retained: the organization never changes then, and two entries of one user at
// once write the same membership. Only the entry that may create the organization holds the row
// of the instance, so that two first entries at once do not both find no such account: the second
// waits, looks again, then joins what the first created. That is why that transaction is read
// committed, as in SetPersonalAccount.
//
// A role is not part of any transaction here, and is always written before the membership it
// goes with. When the API starts, the members of an account where nobody holds a role are all
// made admins; a role without a membership grants nothing, since membership is checked before
// any role. So when one of the two writes is lost, it has to be the membership.
//
// No role is written while the instance is held either: the role store may need a connection of
// the pool this transaction took its own from, and every entry waiting for the instance holds
// one too.
func (d *HusonymDb) EnterInstance(
	ctx context.Context,
	userId pgtype.UUID,
	roles InstanceRoles,
) (*InstanceEntry, error) {
	organization, err := d.Q.GetInstanceOrganization(ctx, d.Db)
	if err != nil {
		return nil, err
	}
	if organization.Valid {
		return d.enterOrganization(ctx, userId, organization, roles)
	}
	accounts, err := d.Q.CountAccountsWithPersonMember(ctx, d.Db)
	if err != nil {
		return nil, err
	}
	if accounts > 0 {
		// The two reads above are not one: the first entry of all may have created the
		// organization between them, and its account is then what was counted. An organization
		// is retained in the transaction that creates its account, so it shows now if so.
		organization, err = d.Q.GetInstanceOrganization(ctx, d.Db)
		if err != nil {
			return nil, err
		}
		if organization.Valid {
			return d.enterOrganization(ctx, userId, organization, roles)
		}
		return &InstanceEntry{Outcome: EntryPersonal}, nil
	}

	// The admin role needs the id of the account, and is written before the instance is held:
	// the id is therefore chosen here, and the account created under it. When this entry ends up
	// creating nothing -- another one did meanwhile, or what follows fails -- the role stays,
	// for an account that never exists. It grants nothing.
	newId, err := ToUuid(uuid.NewString())
	if err != nil {
		return nil, err
	}
	if err := roles.GrantAdmin(ctx, userId, newId); err != nil {
		return nil, fmt.Errorf("unable to give its role to the first member: %w", err)
	}

	var entry *InstanceEntry
	if err := d.WithTx(ctx, &pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, func(dbtx BaseDBTX) error {
		if _, err := d.Q.LockInstance(ctx, dbtx); err != nil {
			return fmt.Errorf("unable to hold the instance: %w", err)
		}
		// What was read before the wait may no longer hold.
		organization, err = d.Q.GetInstanceOrganization(ctx, dbtx)
		if err != nil {
			return err
		}
		if organization.Valid {
			return nil
		}
		accounts, err := d.Q.CountAccountsWithPersonMember(ctx, dbtx)
		if err != nil {
			return err
		}
		if accounts > 0 {
			entry = &InstanceEntry{Outcome: EntryPersonal}
			return nil
		}
		entry, err = createOrganization(ctx, d.Q, dbtx, userId, newId)
		return err
	}); err != nil {
		return nil, err
	}
	if entry == nil {
		// Another entry created the organization meanwhile: the instance is released, and this
		// one enters it as any newcomer does.
		return d.enterOrganization(ctx, userId, organization, roles)
	}
	return entry, nil
}

// enterOrganization adds the user to the organization retained, unless they are in it already.
// It holds nothing: the membership is one statement, which a second one at once leaves as it is.
//
// The viewer role is asked for either way, and given only to who holds none: a member keeps the
// role they hold, and a member whose role was lost gets one back.
func (d *HusonymDb) enterOrganization(
	ctx context.Context,
	userId, organization pgtype.UUID,
	roles InstanceRoles,
) (*InstanceEntry, error) {
	if _, err := d.Q.GetAccount(ctx, d.Db, organization); err != nil {
		if IsNoRows(err) {
			return nil, fmt.Errorf("%w: account %s", ErrInstanceOrganizationMissing, UUIDString(organization))
		}
		return nil, err
	}
	members, err := d.Q.IsUserInAccount(ctx, d.Db, db_queries.IsUserInAccountParams{
		AccountId: organization,
		UserId:    userId,
	})
	if err != nil {
		return nil, err
	}
	if err := roles.GrantViewerIfNone(ctx, userId, organization); err != nil {
		return nil, fmt.Errorf("unable to give a role in the organization: %w", err)
	}
	if members > 0 {
		return &InstanceEntry{Outcome: EntryMember, AccountId: organization}, nil
	}
	if err := d.Q.CreateAccountUserAssociation(ctx, d.Db, db_queries.CreateAccountUserAssociationParams{
		AccountID: organization,
		UserID:    userId,
	}); err != nil {
		return nil, fmt.Errorf("unable to add the user to the organization: %w", err)
	}
	return &InstanceEntry{Outcome: EntryJoined, AccountId: organization}, nil
}

// createOrganization creates, under the id given, the organization of an instance that is new
// for people, with the user as its first member, and has the instance retain it. The user was given
// the admin role for that id beforehand.
func createOrganization(
	ctx context.Context,
	q db_queries.Querier,
	dbtx BaseDBTX,
	userId, accountId pgtype.UUID,
) (*InstanceEntry, error) {
	account, err := q.CreateTeamAccountWithId(ctx, dbtx, db_queries.CreateTeamAccountWithIdParams{
		ID:          accountId,
		AccountSlug: instanceOrganizationName,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create the organization: %w", err)
	}
	if err := q.CreateAccountUserAssociation(ctx, dbtx, db_queries.CreateAccountUserAssociationParams{
		AccountID: account.ID,
		UserID:    userId,
	}); err != nil {
		return nil, fmt.Errorf("unable to add the user to the organization: %w", err)
	}
	if err := retainOrganization(ctx, q, dbtx, account.ID); err != nil {
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
