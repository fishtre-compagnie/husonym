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
	"github.com/stretchr/testify/require"
)

// roleGiven is one call of the role setter.
type roleGiven struct {
	user    string
	account string
	admin   bool
}

// fakeRoles stands for the access control: it records the roles given, and answers whether a
// role is held from them. It can be told to refuse every role.
type fakeRoles struct {
	mu     sync.Mutex
	given  []roleGiven
	refuse error
}

func (f *fakeRoles) set(_ context.Context, userId, accountId pgtype.UUID, admin bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return f.refuse
	}
	f.given = append(f.given, roleGiven{
		user:    husonymdb.UUIDString(userId),
		account: husonymdb.UUIDString(accountId),
		admin:   admin,
	})
	return nil
}

func (f *fakeRoles) has(_ context.Context, userId, accountId pgtype.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, role := range f.given {
		if role.user == husonymdb.UUIDString(userId) && role.account == husonymdb.UUIDString(accountId) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRoles) calls() []roleGiven {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]roleGiven(nil), f.given...)
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

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryCreated, entry.Outcome)

	account, err := s.db.Q.GetAccount(s.ctx, s.db.Db, entry.AccountId)
	require.NoError(t, err)
	require.Equal(t, "organization", account.AccountSlug)
	require.Equal(t, husonymdb.AccountType_Team, husonymdb.AccountType(account.AccountType))

	s.requireOrganization(t, account.ID)
	require.True(t, s.isMember(t, user.ID, account.ID))
	require.Equal(t, int64(1), s.countAccounts(t), "the first entry creates no personal account")
	require.Equal(t, []roleGiven{
		{user: husonymdb.UUIDString(user.ID), account: husonymdb.UUIDString(account.ID), admin: true},
	}, roles.calls())
}

func (s *IntegrationTestSuite) Test_EnterInstance_SecondUserJoinsAsViewer() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles.set, roles.has)
	requireNoErrResp(t, created, err)

	joined, err := s.db.EnterInstance(s.ctx, second.ID, roles.set, roles.has)
	requireNoErrResp(t, joined, err)
	require.Equal(t, husonymdb.EntryJoined, joined.Outcome)
	require.Equal(t, husonymdb.UUIDString(created.AccountId), husonymdb.UUIDString(joined.AccountId))

	require.True(t, s.isMember(t, second.ID, created.AccountId))
	require.Equal(t, int64(1), s.countAccounts(t))
	require.Equal(t, roleGiven{
		user: husonymdb.UUIDString(second.ID), account: husonymdb.UUIDString(created.AccountId), admin: false,
	}, roles.calls()[1])
}

func (s *IntegrationTestSuite) Test_EnterInstance_MemberWithARoleIsLeftAsIs() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles.set, roles.has)
	requireNoErrResp(t, created, err)
	_, err = s.db.EnterInstance(s.ctx, second.ID, roles.set, roles.has)
	require.NoError(t, err)
	before := roles.calls()

	for _, user := range []db_queries.HusonymApiUser{*first, *second} {
		again, err := s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
		requireNoErrResp(t, again, err)
		require.Equal(t, husonymdb.EntryMember, again.Outcome)
		require.Equal(t, husonymdb.UUIDString(created.AccountId), husonymdb.UUIDString(again.AccountId))
	}

	require.Equal(t, before, roles.calls(), "a member that holds a role is given none")
	require.Equal(t, int64(1), s.countAccounts(t))
}

func (s *IntegrationTestSuite) Test_EnterInstance_MemberWithoutARoleIsGivenViewer() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")

	created, err := s.db.EnterInstance(s.ctx, first.ID, roles.set, roles.has)
	requireNoErrResp(t, created, err)
	// A member whose role was never written, or was lost.
	require.NoError(t, s.db.Q.CreateAccountUserAssociation(s.ctx, s.db.Db, db_queries.CreateAccountUserAssociationParams{
		AccountID: created.AccountId,
		UserID:    second.ID,
	}))

	entry, err := s.db.EnterInstance(s.ctx, second.ID, roles.set, roles.has)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryMember, entry.Outcome)
	require.Equal(t, roleGiven{
		user: husonymdb.UUIDString(second.ID), account: husonymdb.UUIDString(created.AccountId), admin: false,
	}, roles.calls()[1])
}

func (s *IntegrationTestSuite) Test_EnterInstance_AccountsPresentAndNoneRetainedIsPersonal() {
	t := s.T()
	roles := &fakeRoles{}
	earlier := s.setUser(t, s.ctx, "earlier")
	_, err := s.db.SetPersonalAccount(s.ctx, earlier.ID, nil)
	require.NoError(t, err)
	user := s.setUser(t, s.ctx, "newcomer")

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
	requireNoErrResp(t, entry, err)
	require.Equal(t, husonymdb.EntryPersonal, entry.Outcome)
	require.False(t, entry.AccountId.Valid)

	s.requireNoOrganization(t)
	require.Equal(t, int64(1), s.countAccounts(t))
	require.Empty(t, roles.calls())
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
			entries[i], errs[i] = s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
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

	admins := 0
	for _, role := range roles.calls() {
		if role.admin {
			admins++
		}
	}
	require.Len(t, roles.calls(), 2)
	require.Equal(t, 1, admins, "one administrator, one viewer")
	for i, user := range users {
		require.True(t, s.isMember(t, user.ID, entries[i].AccountId))
	}
}

func (s *IntegrationTestSuite) Test_EnterInstance_RoleRefusedOnANewInstanceWritesNothing() {
	t := s.T()
	refused := errors.New("the roles are not writable")
	roles := &fakeRoles{refuse: refused}
	user := s.setUser(t, s.ctx, "first")

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
	requireErrResp(t, entry, err)
	require.ErrorIs(t, err, refused)

	s.requireNoOrganization(t)
	require.Equal(t, int64(0), s.countAccounts(t))
}

func (s *IntegrationTestSuite) Test_EnterInstance_RoleRefusedOnJoiningWritesNoMembership() {
	t := s.T()
	roles := &fakeRoles{}
	first := s.setUser(t, s.ctx, "first")
	second := s.setUser(t, s.ctx, "second")
	created, err := s.db.EnterInstance(s.ctx, first.ID, roles.set, roles.has)
	requireNoErrResp(t, created, err)

	refused := errors.New("the roles are not writable")
	entry, err := s.db.EnterInstance(s.ctx, second.ID, (&fakeRoles{refuse: refused}).set, roles.has)
	requireErrResp(t, entry, err)
	require.ErrorIs(t, err, refused)

	require.False(t, s.isMember(t, second.ID, created.AccountId))
	s.requireOrganization(t, created.AccountId)
}

func (s *IntegrationTestSuite) Test_EnterInstance_RetainedAccountIsHeldByTheSchema() {
	t := s.T()
	roles := &fakeRoles{}
	user := s.setUser(t, s.ctx, "first")
	created, err := s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
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

	entry, err := s.db.EnterInstance(s.ctx, user.ID, roles.set, roles.has)
	requireErrResp(t, entry, err)
	require.ErrorIs(t, err, husonymdb.ErrInstanceOrganizationMissing)

	require.Equal(t, int64(0), s.countAccounts(t), "no second organization is created")
	require.Empty(t, roles.calls())
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
