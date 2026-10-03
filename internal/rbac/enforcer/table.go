package enforcer

import (
	"context"
	"fmt"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/rbac/sqladapter"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// tableName is the table the role assignments are stored in.
const tableName = "husonym_api.casbin_rule"

// table is the rule table of the API database.
type table struct {
	*sqladapter.Adapter
	pool    *pgxpool.Pool
	queries db_queries.Querier
}

// OpenRows gives the rows of the rule table of the API database. It fails if the database does
// not answer.
func OpenRows(ctx context.Context, pool *pgxpool.Pool) (Rows, error) {
	adapter, err := sqladapter.NewAdapterWithContext(ctx, stdlib.OpenDBFromPool(pool), "pgx", tableName)
	if err != nil {
		return nil, fmt.Errorf("unable to reach the table of the access rules: %w", err)
	}
	return &table{Adapter: adapter, pool: pool, queries: db_queries.New()}, nil
}

// ReplaceAssignmentCtx leaves the person that role in the account and no other, in one
// transaction: the role is never missing, to this instance or to another. Two replacements
// for one person in one account, wherever they are asked, are made one after the other.
func (t *table) ReplaceAssignmentCtx(ctx context.Context, user, role, account string) error {
	// Read committed, whatever the default of the server: the replacement has to see what the
	// change it waited for wrote, which a snapshot taken before the wait would not show.
	tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	// Rolling back a transaction that was committed does nothing.
	defer func() { _ = tx.Rollback(ctx) }()

	lock := db_queries.LockAccountRoleParams{Member: user, Account: account}
	if err := t.queries.LockAccountRole(ctx, tx, lock); err != nil {
		return err
	}
	replacement := db_queries.ReplaceAccountRoleParams{Member: user, Role: role, Account: account}
	if err := t.queries.ReplaceAccountRole(ctx, tx, replacement); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
