package husonymdb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	fakeAdmin     = "admin"
	fakeViewer    = "viewer"
	fakeDeveloper = "developer"
)

// roleKey names a user in an account.
type roleKey struct {
	user    string
	account string
}

func newRoleKey(userId, accountId pgtype.UUID) roleKey {
	return roleKey{user: husonymdb.UUIDString(userId), account: husonymdb.UUIDString(accountId)}
}

// fakeRoles stands for the role store, and keeps its contract: one role per user and account,
// and a viewer role that is given only where none is held. It can be told to refuse every role,
// and to do something before each write, as a store that needs a connection would.
type fakeRoles struct {
	mu     sync.Mutex
	held   map[roleKey]string
	refuse error
	before func(ctx context.Context) error
}

var _ husonymdb.InstanceRoles = (*fakeRoles)(nil)

func (f *fakeRoles) grant(ctx context.Context, key roleKey, role string, replace bool) error {
	if f.before != nil {
		if err := f.before(ctx); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return f.refuse
	}
	if f.held == nil {
		f.held = map[roleKey]string{}
	}
	if _, ok := f.held[key]; ok && !replace {
		return nil
	}
	f.held[key] = role
	return nil
}

func (f *fakeRoles) GrantAdmin(ctx context.Context, userId, accountId pgtype.UUID) error {
	return f.grant(ctx, newRoleKey(userId, accountId), fakeAdmin, true)
}

func (f *fakeRoles) GrantViewerIfNone(ctx context.Context, userId, accountId pgtype.UUID) error {
	return f.grant(ctx, newRoleKey(userId, accountId), fakeViewer, false)
}

// set writes a role as something other than an entry would.
func (f *fakeRoles) set(userId, accountId pgtype.UUID, role string) {
	_ = f.grant(context.Background(), newRoleKey(userId, accountId), role, true)
}

// of gives the role a user holds in an account, or nothing.
func (f *fakeRoles) of(userId, accountId pgtype.UUID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held[newRoleKey(userId, accountId)]
}

// in gives the roles held in an account.
func (f *fakeRoles) in(accountId pgtype.UUID) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var roles []string
	for key, role := range f.held {
		if key.account == husonymdb.UUIDString(accountId) {
			roles = append(roles, role)
		}
	}
	return roles
}

func (f *fakeRoles) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.held)
}

func (s *IntegrationTestSuite) countAccounts(t testing.TB) int64 {
	t.Helper()
	count, err := s.db.Q.CountAccounts(s.ctx, s.db.Db)
	require.NoError(t, err)
	return count
}

func (s *IntegrationTestSuite) isMember(t testing.TB, userId, accountId pgtype.UUID) bool {
	t.Helper()
	count, err := s.db.Q.IsUserInAccount(s.ctx, s.db.Db, db_queries.IsUserInAccountParams{
		AccountId: accountId,
		UserId:    userId,
	})
	require.NoError(t, err)
	return count > 0
}

func (s *IntegrationTestSuite) requireNoOrganization(t testing.TB) {
	t.Helper()
	_, retained, err := s.db.GetInstanceOrganization(s.ctx)
	require.NoError(t, err)
	require.False(t, retained)
}

func (s *IntegrationTestSuite) requireOrganization(t testing.TB, accountId pgtype.UUID) {
	t.Helper()
	organization, retained, err := s.db.GetInstanceOrganization(s.ctx)
	require.NoError(t, err)
	require.True(t, retained)
	require.Equal(t, husonymdb.UUIDString(accountId), husonymdb.UUIDString(organization))
}

func (s *IntegrationTestSuite) Test_EnterInstance_FirstEntryCreatesTheOrganization() {
	t := s.T()
	roles := &fakeRoles{}
	user := s.setUser(t, s.ctx, "first")

	s.requireNoOrganization(t)

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryCreated, entry.Outcome)

	account, err := s.db.Q.GetAccount(s.ctx, s.db.Db, entry.AccountId)
	require.NoError(t, err)
	require.Equal(t, "organization", account.AccountSlug)
	require.Equal(t, husonymdb.AccountType_Team, husonymdb.AccountType(account.AccountType))

	s.requireOrganization(t, account.ID)
	require.True(t, s.isMember(t, user.ID, account.ID))
	require.Equal(t, int64(1), s.countAccounts(t), "the first entry creates no personal account")
	require.Equal(t, fakeAdmin, roles.of(user.ID, account.ID))
	require.Equal(t, 1, roles.count())
}

func (s *IntegrationTestSuite) Test_EnterInstance_SecondUserJoinsAsViewer() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles)
	requireNoErrResp(t, created, err)

	joined, err := s.db.EnterInstance(s.ctx, second.ID, roles)
	requireNoErrResp(t, joined, err)
	require.Equal(t, husonymdb.EntryJoined, joined.Outcome)
	require.Equal(t, husonymdb.UUIDString(created.AccountId), husonymdb.UUIDString(joined.AccountId))

	require.True(t, s.isMember(t, second.ID, created.AccountId))
	require.Equal(t, int64(1), s.countAccounts(t))
	require.Equal(t, fakeViewer, roles.of(second.ID, created.AccountId))
	require.Equal(t, fakeAdmin, roles.of(first.ID, created.AccountId))
}

func (s *IntegrationTestSuite) Test_EnterInstance_MemberReentryNeverChangesTheRoleHeld() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")
	third := s.setUser(t, s.ctx, "third")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles)
	requireNoErrResp(t, created, err)
	for _, user := range []*db_queries.HusonymApiUser{second, third} {
		_, err = s.db.EnterInstance(s.ctx, user.ID, roles)
		require.NoError(t, err)
	}
	// An administrator gave the third another role since.
	roles.set(third.ID, created.AccountId, fakeDeveloper)

	for _, user := range []*db_queries.HusonymApiUser{second, third} {
		again, err := s.db.EnterInstance(s.ctx, user.ID, roles)
		requireNoErrResp(t, again, err)
		require.Equal(t, husonymdb.EntryMember, again.Outcome)
		require.Equal(t, husonymdb.UUIDString(created.AccountId), husonymdb.UUIDString(again.AccountId))
	}

	require.Equal(t, fakeViewer, roles.of(second.ID, created.AccountId))
	require.Equal(t, fakeDeveloper, roles.of(third.ID, created.AccountId))
	require.Equal(t, int64(1), s.countAccounts(t))
}

func (s *IntegrationTestSuite) Test_EnterInstance_CreatorWhoReentersStaysAdministrator() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles)
	requireNoErrResp(t, created, err)

	again, err := s.db.EnterInstance(s.ctx, first.ID, roles)
	requireNoErrResp(t, again, err)
	require.Equal(t, husonymdb.EntryMember, again.Outcome)
	require.Equal(t, husonymdb.UUIDString(created.AccountId), husonymdb.UUIDString(again.AccountId))
	require.Equal(t, fakeAdmin, roles.of(first.ID, created.AccountId))
}

func (s *IntegrationTestSuite) Test_EnterInstance_MemberWithoutARoleIsGivenViewer() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles)
	requireNoErrResp(t, created, err)
	// A member whose role was never written, or was lost.
	require.NoError(t, s.db.Q.CreateAccountUserAssociation(s.ctx, s.db.Db, db_queries.CreateAccountUserAssociationParams{
		AccountID: created.AccountId,
		UserID:    second.ID,
	}))

	entry, err := s.db.EnterInstance(s.ctx, second.ID, roles)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryMember, entry.Outcome)
	require.Equal(t, fakeViewer, roles.of(second.ID, created.AccountId))
}

func (s *IntegrationTestSuite) Test_EnterInstance_AccountsPresentAndNoneRetainedIsPersonal() {
	t := s.T()
	roles := &fakeRoles{}
	earlier := s.setUser(t, s.ctx, "earlier")
	_, err := s.db.SetPersonalAccount(s.ctx, earlier.ID, nil)
	require.NoError(t, err)
	user := s.setUser(t, s.ctx, "newcomer")

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryPersonal, entry.Outcome)
	require.False(t, entry.AccountId.Valid)

	s.requireNoOrganization(t)
	require.Equal(t, int64(1), s.countAccounts(t))
	require.Zero(t, roles.count())
	accounts, err := s.db.Q.GetAccountsByUser(s.ctx, s.db.Db, user.ID)
	require.NoError(t, err)
	require.Empty(t, accounts)
}

func (s *IntegrationTestSuite) Test_EnterInstance_TwoFirstEntriesAtOnceCreateOneOrganization() {
	t := s.T()
	roles := &fakeRoles{}
	users := []*db_queries.HusonymApiUser{
		s.setUser(t, s.ctx, "one"),
		s.setUser(t, s.ctx, "two"),
	}

	entries := make([]*husonymdb.InstanceEntry, len(users))
	errs := make([]error, len(users))
	var wg sync.WaitGroup
	for i, user := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries[i], errs[i] = s.db.EnterInstance(s.ctx, user.ID, roles)
		}()
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	outcomes := []husonymdb.EntryOutcome{entries[0].Outcome, entries[1].Outcome}
	require.ElementsMatch(t, []husonymdb.EntryOutcome{husonymdb.EntryCreated, husonymdb.EntryJoined}, outcomes)
	require.Equal(t, husonymdb.UUIDString(entries[0].AccountId), husonymdb.UUIDString(entries[1].AccountId))
	require.Equal(t, int64(1), s.countAccounts(t))
	s.requireOrganization(t, entries[0].AccountId)

	// The one that created nothing may have left an administrator role for an account that
	// never existed: in the organization there is one administrator and one viewer.
	require.ElementsMatch(t, []string{fakeAdmin, fakeViewer}, roles.in(entries[0].AccountId))
	for i, user := range users {
		require.True(t, s.isMember(t, user.ID, entries[i].AccountId))
		want := fakeViewer
		if entries[i].Outcome == husonymdb.EntryCreated {
			want = fakeAdmin
		}
		require.Equal(t, want, roles.of(user.ID, entries[i].AccountId))
	}
}

// As many first entries at once as the pool has connections, with a role store that takes its
// connections from that pool: an entry that wrote a role while it holds the instance would wait
// for a connection the entries waiting for the instance hold.
func (s *IntegrationTestSuite) Test_EnterInstance_FirstEntriesAtOnceDoNotExhaustThePool() {
	t := s.T()
	const entries = 4

	config, err := pgxpool.ParseConfig(s.pgcontainer.URL)
	require.NoError(t, err)
	config.MaxConns = entries
	pool, err := pgxpool.NewWithConfig(s.ctx, config)
	require.NoError(t, err)
	defer pool.Close()
	db := husonymdb.New(pool, db_queries.New())

	roles := &fakeRoles{before: func(ctx context.Context) error {
		_, err := pool.Exec(ctx, "SELECT 1")
		return err
	}}
	users := make([]*db_queries.HusonymApiUser, entries)
	for i := range users {
		users[i] = s.setUser(t, s.ctx, uuid.NewString())
	}

	// Bounded, so that entries that wait for one another fail instead of hanging.
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	outcomes := make([]husonymdb.EntryOutcome, entries)
	errs := make([]error, entries)
	var wg sync.WaitGroup
	for i, user := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := db.EnterInstance(ctx, user.ID, roles)
			if err != nil {
				errs[i] = err
				return
			}
			outcomes[i] = entry.Outcome
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}

	created := 0
	for _, outcome := range outcomes {
		if outcome == husonymdb.EntryCreated {
			created++
		} else {
			require.Equal(t, husonymdb.EntryJoined, outcome)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, int64(1), s.countAccounts(t))
}

// An entry is made on every page load: once the organization is retained it must not wait for
// the row of the instance, which another connection holds here.
func (s *IntegrationTestSuite) Test_EnterInstance_RetainedOrganizationIsEnteredWithoutHoldingTheInstance() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")
	created, err := s.db.EnterInstance(s.ctx, first.ID, roles)
	requireNoErrResp(t, created, err)

	holder, err := s.pgcontainer.DB.Begin(s.ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(s.ctx) }()
	_, err = holder.Exec(s.ctx, "SELECT id FROM husonym_api.instance FOR UPDATE")
	require.NoError(t, err)

	// Bounded, so that an entry that waits for the instance fails instead of hanging.
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	member, err := s.db.EnterInstance(ctx, first.ID, roles)
	requireNoErrResp(t, member, err)
	require.Equal(t, husonymdb.EntryMember, member.Outcome)

	joined, err := s.db.EnterInstance(ctx, second.ID, roles)
	requireNoErrResp(t, joined, err)
	require.Equal(t, husonymdb.EntryJoined, joined.Outcome)
	require.True(t, s.isMember(t, second.ID, created.AccountId))
}

func (s *IntegrationTestSuite) Test_EnterInstance_RoleRefusedOnANewInstanceWritesNothing() {
	t := s.T()
	refused := errors.New("the roles are not writable")
	roles := &fakeRoles{refuse: refused}
	user := s.setUser(t, s.ctx, "first")

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles)
	requireErrResp(t, entry, err)
	require.ErrorIs(t, err, refused)

	s.requireNoOrganization(t)
	require.Equal(t, int64(0), s.countAccounts(t))
}

func (s *IntegrationTestSuite) Test_EnterInstance_RoleRefusedOnJoiningWritesNoMembership() {
	t := s.T()
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")
	created, err := s.db.EnterInstance(s.ctx, first.ID, &fakeRoles{})
	requireNoErrResp(t, created, err)

	refused := errors.New("the roles are not writable")
	entry, err := s.db.EnterInstance(s.ctx, second.ID, &fakeRoles{refuse: refused})
	requireErrResp(t, entry, err)
	require.ErrorIs(t, err, refused)

	require.False(t, s.isMember(t, second.ID, created.AccountId))
	s.requireOrganization(t, created.AccountId)
}

func (s *IntegrationTestSuite) Test_EnterInstance_RetainedAccountIsHeldByTheSchema() {
	t := s.T()
	user := s.setUser(t, s.ctx, "first")
	created, err := s.db.EnterInstance(s.ctx, user.ID, &fakeRoles{})
	requireNoErrResp(t, created, err)

	_, err = s.pgcontainer.DB.Exec(s.ctx, "DELETE FROM husonym_api.accounts WHERE id = $1", created.AccountId)
	require.Error(t, err, "the account an instance retains cannot be deleted")
	s.requireOrganization(t, created.AccountId)
}

// The schema holds the retained account, so it only goes missing once the constraint is gone:
// it is dropped here, in a schema this test alone uses.
func (s *IntegrationTestSuite) Test_EnterInstance_RetainedAccountMissingIsRefused() {
	t := s.T()
	roles := &fakeRoles{}
	user := s.setUser(t, s.ctx, "first")

	_, err := s.pgcontainer.DB.Exec(
		s.ctx,
		"ALTER TABLE husonym_api.instance DROP CONSTRAINT instance_organization_account_id_fkey",
	)
	require.NoError(t, err)
	gone := uuid.NewString()
	_, err = s.pgcontainer.DB.Exec(s.ctx, "UPDATE husonym_api.instance SET organization_account_id = $1", gone)
	require.NoError(t, err)

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles)
	requireErrResp(t, entry, err)
	require.ErrorIs(t, err, husonymdb.ErrInstanceOrganizationMissing)

	require.Equal(t, int64(0), s.countAccounts(t), "no second organization is created")
	require.Zero(t, roles.count())
	organization, retained, err := s.db.GetInstanceOrganization(s.ctx)
	require.NoError(t, err)
	require.True(t, retained)
	require.Equal(t, gone, husonymdb.UUIDString(organization))
}

func (s *IntegrationTestSuite) Test_DesignateInstanceOrganization_PersonalAccountIsConverted() {
	t := s.T()
	user := s.setUser(t, s.ctx, "owner")
	maxAllowed := int64(100)
	personal, err := s.db.SetPersonalAccount(s.ctx, user.ID, &maxAllowed)
	requireNoErrResp(t, personal, err)
	key, err := s.db.CreateAccountApikey(s.ctx, &husonymdb.CreateAccountApiKeyRequest{
		KeyName:           "ci",
		KeyValue:          "key-of-the-personal-account",
		AccountUuid:       personal.ID,
		CreatedByUserUuid: user.ID,
		ExpiresAt:         getFutureTs(t, 24*time.Hour),
	})
	requireNoErrResp(t, key, err)

	account, err := s.db.DesignateInstanceOrganization(s.ctx, user.ID, personal.ID, "acme")
	requireNoErrResp(t, account, err)

	require.Equal(t, husonymdb.UUIDString(personal.ID), husonymdb.UUIDString(account.ID))
	require.Equal(t, husonymdb.AccountType_Team, husonymdb.AccountType(account.AccountType))
	require.Equal(t, "acme", account.AccountSlug)
	s.requireOrganization(t, personal.ID)
	require.Equal(t, int64(1), s.countAccounts(t), "no personal account is made beside the organization")
	require.True(t, s.isMember(t, user.ID, personal.ID))

	resolved, err := s.db.Q.GetAccountApiKeyByKeyValue(s.ctx, s.db.Db, "key-of-the-personal-account")
	require.NoError(t, err)
	require.Equal(t, husonymdb.UUIDString(personal.ID), husonymdb.UUIDString(resolved.AccountID))
}

func (s *IntegrationTestSuite) Test_DesignateInstanceOrganization_PersonalAccountNameMustBeFree() {
	t := s.T()
	user := s.setUser(t, s.ctx, "owner")
	personal, err := s.db.SetPersonalAccount(s.ctx, user.ID, nil)
	requireNoErrResp(t, personal, err)
	_, err = s.db.CreateTeamAccount(s.ctx, user.ID, "acme", testutil.GetTestLogger(t))
	require.NoError(t, err)

	account, err := s.db.DesignateInstanceOrganization(s.ctx, user.ID, personal.ID, "ACME")
	requireErrResp(t, account, err)
	alreadyExists := husonymerrors.NewAlreadyExists("")
	require.ErrorAs(t, err, &alreadyExists)
	s.requireNoOrganization(t)

	unchanged, err := s.db.Q.GetAccount(s.ctx, s.db.Db, personal.ID)
	require.NoError(t, err)
	require.Equal(t, husonymdb.AccountType_Personal, husonymdb.AccountType(unchanged.AccountType))
}

func (s *IntegrationTestSuite) Test_DesignateInstanceOrganization_TeamAccountIgnoresTheName() {
	t := s.T()
	user := s.setUser(t, s.ctx, "owner")
	team, err := s.db.CreateTeamAccount(s.ctx, user.ID, "myteam", testutil.GetTestLogger(t))
	requireNoErrResp(t, team, err)

	account, err := s.db.DesignateInstanceOrganization(s.ctx, user.ID, team.ID, "another-name")
	requireNoErrResp(t, account, err)

	require.Equal(t, husonymdb.UUIDString(team.ID), husonymdb.UUIDString(account.ID))
	require.Equal(t, "myteam", account.AccountSlug)
	require.Equal(t, husonymdb.AccountType_Team, husonymdb.AccountType(account.AccountType))
	s.requireOrganization(t, team.ID)
}

func (s *IntegrationTestSuite) Test_DesignateInstanceOrganization_SecondIsRefused() {
	t := s.T()
	user := s.setUser(t, s.ctx, "owner")
	first, err := s.db.CreateTeamAccount(s.ctx, user.ID, "first", testutil.GetTestLogger(t))
	requireNoErrResp(t, first, err)
	second, err := s.db.CreateTeamAccount(s.ctx, user.ID, "second", testutil.GetTestLogger(t))
	requireNoErrResp(t, second, err)

	_, err = s.db.DesignateInstanceOrganization(s.ctx, user.ID, first.ID, "")
	require.NoError(t, err)

	for _, account := range []*db_queries.HusonymApiAccount{second, first} {
		again, err := s.db.DesignateInstanceOrganization(s.ctx, user.ID, account.ID, "")
		requireErrResp(t, again, err)
		require.ErrorIs(t, err, husonymdb.ErrInstanceOrganizationSet)
	}
	s.requireOrganization(t, first.ID)
}

func (s *IntegrationTestSuite) Test_DesignateInstanceOrganization_TwoAtOnceOneSucceeds() {
	t := s.T()
	users := []*db_queries.HusonymApiUser{
		s.setUser(t, s.ctx, "one"),
		s.setUser(t, s.ctx, "two"),
	}
	personals := make([]*db_queries.HusonymApiAccount, len(users))
	for i, user := range users {
		personal, err := s.db.SetPersonalAccount(s.ctx, user.ID, nil)
		requireNoErrResp(t, personal, err)
		personals[i] = personal
	}

	designated := make([]*db_queries.HusonymApiAccount, len(users))
	errs := make([]error, len(users))
	names := []string{"acme", "globex"}
	var wg sync.WaitGroup
	for i, user := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			designated[i], errs[i] = s.db.DesignateInstanceOrganization(s.ctx, user.ID, personals[i].ID, names[i])
		}()
	}
	wg.Wait()

	winner, loser := 0, 1
	if errs[0] != nil {
		winner, loser = 1, 0
	}
	require.NoError(t, errs[winner])
	require.ErrorIs(t, errs[loser], husonymdb.ErrInstanceOrganizationSet)
	require.Nil(t, designated[loser])
	s.requireOrganization(t, personals[winner].ID)

	// The one refused was not converted on the way.
	refused, err := s.db.Q.GetAccount(s.ctx, s.db.Db, personals[loser].ID)
	require.NoError(t, err)
	require.Equal(t, husonymdb.AccountType_Personal, husonymdb.AccountType(refused.AccountType))
	require.Equal(t, "personal", refused.AccountSlug)
}

func (s *IntegrationTestSuite) Test_DesignateInstanceOrganization_AccountOfAnotherIsRefused() {
	t := s.T()
	owner := s.setUser(t, s.ctx, "owner")
	stranger := s.setUser(t, s.ctx, "stranger")
	team, err := s.db.CreateTeamAccount(s.ctx, owner.ID, "myteam", testutil.GetTestLogger(t))
	requireNoErrResp(t, team, err)

	account, err := s.db.DesignateInstanceOrganization(s.ctx, stranger.ID, team.ID, "taken")
	requireErrResp(t, account, err)
	notFound := husonymerrors.NewNotFound("")
	require.ErrorAs(t, err, &notFound)
	s.requireNoOrganization(t)
}
