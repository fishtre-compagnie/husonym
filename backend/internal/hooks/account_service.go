package hooks

import (
	"context"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
)

// AccountService is the logic of account hooks. Its methods are the procedures of the account
// hook service of the contract that are not retired.
type AccountService struct {
	db   *husonymdb.HusonymDb
	gate gate
	// workerOnly tells the worker from the other callers: the worker reads the secret a
	// webhook is signed with.
	workerOnly userdata.WorkerOnly
}

// NewAccountService builds the logic of account hooks on the database of the API, on what
// tells who the caller is, and on the deployment's rule for what only the worker calls.
func NewAccountService(
	db *husonymdb.HusonymDb,
	users userdata.Interface,
	workerOnly userdata.WorkerOnly,
) *AccountService {
	return &AccountService{db: db, gate: gate{users: users}, workerOnly: workerOnly}
}

// account gives what an account id names. Whether the account exists is the access layer's
// to say: nobody belongs to an account that does not.
func account(id string) (target, pgtype.UUID, error) {
	accountID, err := husonymdb.ToUuid(id)
	if err != nil {
		return target{}, pgtype.UUID{}, invalidID("account")
	}
	return target{accountID: husonymdb.UUIDString(accountID)}, accountID, nil
}

// hook finds what an account hook id names, and the hook when there is one.
func (s *AccountService) hook(ctx context.Context, id string, absent error) (target, *db_queries.HusonymApiAccountHook, error) {
	hookID, err := husonymdb.ToUuid(id)
	if err != nil {
		return target{}, nil, invalidID("account hook")
	}
	row, err := s.db.Q.GetAccountHookById(ctx, s.db.Db, hookID)
	switch {
	case husonymdb.IsNoRows(err):
		return target{absent: absent}, nil, nil
	case err != nil:
		return target{}, nil, fmt.Errorf("unable to find the account hook: %w", err)
	}
	return target{accountID: husonymdb.UUIDString(row.AccountID), absent: absent}, &row, nil
}
