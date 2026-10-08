package husonymdb

import (
	"context"
	"sync"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/stretchr/testify/require"
)

// beforeFirstCount is the queries of the database, with something done once, just before the
// accounts people have are first counted: what another entry does between the two reads an entry
// starts with.
type beforeFirstCount struct {
	db_queries.Querier
	once sync.Once
	do   func()
}

func (q *beforeFirstCount) CountAccountsWithPersonMember(ctx context.Context, db db_queries.DBTX) (int64, error) {
	q.once.Do(q.do)
	return q.Querier.CountAccountsWithPersonMember(ctx, db)
}

// An entry reads the organization, then counts the accounts. The first entry of all commits
// between the two here: the second one found no organization, and then counts the account of the
// organization. It joins it, and is not sent to a personal account.
func (s *IntegrationTestSuite) Test_EnterInstance_OrganizationCreatedBetweenTheTwoReadsIsJoined() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")

	var created *husonymdb.InstanceEntry
	var createdErr error
	queries := &beforeFirstCount{Querier: db_queries.New(), do: func() {
		created, createdErr = s.db.EnterInstance(s.ctx, first.ID, roles)
	}}
	interleaved := husonymdb.New(s.pgcontainer.DB, queries)

	entry, err := interleaved.EnterInstance(s.ctx, second.ID, roles)

	requireNoErrResp(t, created, createdErr)
	require.Equal(t, husonymdb.EntryCreated, created.Outcome)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryJoined, entry.Outcome)
	require.Equal(t, husonymdb.UUIDString(created.AccountId), husonymdb.UUIDString(entry.AccountId))
	require.Equal(t, int64(1), s.countAccounts(t))
	require.True(t, s.isMember(t, second.ID, created.AccountId))
	require.Equal(t, fakeViewer, roles.of(second.ID, created.AccountId))
	require.Equal(t, fakeAdmin, roles.of(first.ID, created.AccountId))
}
