package rbac

import (
	"context"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// accountsOfDatabase reads the accounts and their members from the API database.
type accountsOfDatabase struct {
	queries db_queries.Querier
	db      db_queries.DBTX
}

// NewAccounts gives the accounts of the API database, and the people who are members of each.
func NewAccounts(queries db_queries.Querier, db db_queries.DBTX) Accounts {
	return &accountsOfDatabase{queries: queries, db: db}
}

func (a *accountsOfDatabase) Accounts(ctx context.Context) ([]Account, error) {
	ids, err := a.queries.GetAccountIds(ctx, a.db)
	if err != nil {
		return nil, err
	}
	accounts := make([]Account, 0, len(ids))
	for _, id := range ids {
		accounts = append(accounts, NewAccount(husonymdb.UUIDString(id)))
	}
	return accounts, nil
}

func (a *accountsOfDatabase) HumanMembers(ctx context.Context, account Account) ([]User, error) {
	accountId, err := husonymdb.ToUuid(account.id)
	if err != nil {
		return nil, err
	}
	ids, err := a.queries.GetAccountUsers(ctx, a.db, accountId)
	if err != nil {
		return nil, err
	}
	members := make([]User, 0, len(ids))
	for _, id := range ids {
		members = append(members, NewPgUser(id))
	}
	return members, nil
}
