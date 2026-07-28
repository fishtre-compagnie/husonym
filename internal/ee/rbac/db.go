package rbac

import (
	"context"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

type RbacDb struct {
	q    db_queries.Querier
	dbtx db_queries.DBTX
}

var _ Db = (*RbacDb)(nil)

func NewRbacDb(q db_queries.Querier, dbtx db_queries.DBTX) *RbacDb {
	return &RbacDb{q: q, dbtx: dbtx}
}

func (r *RbacDb) GetAccountIds(ctx context.Context) ([]string, error) {
	resp, err := r.q.GetAccountIds(ctx, r.dbtx)
	if err != nil {
		return nil, err
	}
	return husonymdb.UUIDStrings(resp), nil
}

func (r *RbacDb) GetAccountUsers(ctx context.Context, accountId string) ([]string, error) {
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return nil, err
	}
	resp, err := r.q.GetAccountUsers(ctx, r.dbtx, accountUuid)
	if err != nil {
		return nil, err
	}
	return husonymdb.UUIDStrings(resp), nil
}
