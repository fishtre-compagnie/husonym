package rbac

import (
	"context"
	"errors"
	"sync"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/rbac/enforcer"
	"github.com/stretchr/testify/require"
)

const (
	admin     = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN
	developer = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER
	viewer    = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER
	executor  = mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_EXECUTOR
)

// Each role is stored under its word, and read back from it; a word that is no role is no role.
func Test_Role_TranslatesBothWays(t *testing.T) {
	for role, word := range map[mgmtv1alpha1.AccountRole]string{
		admin: "account_admin", developer: "job_developer", executor: "job_executor", viewer: "job_viewer",
	} {
		stored, ok := roleWord(role)
		require.True(t, ok)
		require.Equal(t, word, stored)
		require.Equal(t, role, highestRole([]string{word}))
	}
	_, ok := roleWord(mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED)
	require.False(t, ok)
	_, ok = roleWord(mgmtv1alpha1.AccountRole(42))
	require.False(t, ok)
	require.Equal(t, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, highestRole([]string{"owner"}))
	require.Equal(t, developer, highestRole([]string{"job_viewer", "job_developer", "job_executor"}))
}

// A member given a role holds that role and no other: the one held before is taken away.
func Test_SetRole_ReplacesTheRoleHeld(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()

	require.NoError(t, service.SetRole(ctx, member, account, admin))
	require.NoError(t, service.SetRole(ctx, member, account, viewer))
	require.NoError(t, service.SetRole(ctx, member, account, viewer), "the role already held is given again")

	require.Equal(t, [][]string{assignment(member, "job_viewer", account)}, rows.stored())
	requireRefused(t, service.Enforce(ctx, member, account, JobAction_Execute), JobAction_Execute)
	require.Equal(t, map[User]mgmtv1alpha1.AccountRole{member: viewer}, service.Roles([]User{member}, account))
}

// A change of role the table refuses leaves the member the role held before, in the table and
// in memory: never none.
func Test_SetRole_NeverLeavesTheMemberWithoutARole(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()
	require.NoError(t, service.SetRole(ctx, member, account, developer))

	down := errors.New("the database is down")
	rows.fail(down, nil)
	require.ErrorIs(t, service.SetRole(ctx, member, account, viewer), down)

	require.NoError(t, service.Enforce(ctx, member, account, JobAction_Create), "the role held before is lost")
	require.Equal(t, [][]string{assignment(member, "job_developer", account)}, rows.stored())

	// Another instance, which reads the table, sees that role too.
	rows.fail(nil, nil)
	require.NoError(t, service.enforcer.LoadPolicy())
	require.Equal(t, map[User]mgmtv1alpha1.AccountRole{member: developer}, service.Roles([]User{member}, account))

	// Asked again once the table answers, the change is made.
	require.NoError(t, service.SetRole(ctx, member, account, viewer))
	require.Equal(t, [][]string{assignment(member, "job_viewer", account)}, rows.stored())
}

// A role the table took and that could not be read back is told as such: it is stored, and
// held here once the roles are read again. Until then the member holds the role held before.
func Test_SetRole_TellsARoleStoredAndNotReadBack(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()
	require.NoError(t, service.SetRole(ctx, member, account, developer))

	down := errors.New("the database is down")
	rows.fail(nil, down)
	err := service.SetRole(ctx, member, account, viewer)
	require.ErrorIs(t, err, enforcer.ErrNotReadBack)
	require.ErrorIs(t, err, down)

	require.Equal(t, [][]string{assignment(member, "job_viewer", account)}, rows.stored())
	require.NoError(t, service.Enforce(ctx, member, account, JobAction_Create))

	rows.fail(nil, nil)
	require.NoError(t, service.enforcer.LoadPolicy())
	requireRefused(t, service.Enforce(ctx, member, account, JobAction_Create), JobAction_Create)
	require.NoError(t, service.Enforce(ctx, member, account, JobAction_View))
}

// A role another instance gave moments ago, which this one has not read yet, is taken away with
// the others.
func Test_SetRole_RemovesARoleThisInstanceDidNotKnow(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()
	rows.write(assignment(member, "account_admin", account)...)

	require.NoError(t, service.SetRole(ctx, member, account, viewer))

	require.Equal(t, [][]string{assignment(member, "job_viewer", account)}, rows.stored())
	require.NoError(t, service.enforcer.LoadPolicy())
	requireRefused(t, service.Enforce(ctx, member, account, AccountAction_Edit), AccountAction_Edit)
}

// A role assigned in every account, by a row made by hand, gives nothing in any account.
func Test_Service_ARoleAssignedToEveryAccountGivesNothing(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	everywhere, underAccounts := someone(), someone()
	rows.write("g", everywhere.stored(), "account_admin", "*")
	rows.write("g", underAccounts.stored(), "account_admin", "accounts/*")
	service := serviceOn(t, rows)

	for _, user := range []User{everywhere, underAccounts} {
		for _, action := range Actions() {
			requireRefused(t, service.Enforce(ctx, user, someAccount(), action), action)
		}
	}
}

// The role asked for is in the table once the change returns, even when this instance believed
// the member already held it: another instance had given the member another role since, which
// this one had not read yet.
func Test_SetRole_StoresTheRoleThisInstanceBelievedHeld(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()
	require.NoError(t, service.SetRole(ctx, member, account, developer))
	// Elsewhere, the member is made a viewer.
	rows.erase(assignment(member, "job_developer", account)...)
	rows.write(assignment(member, "job_viewer", account)...)

	require.NoError(t, service.SetRole(ctx, member, account, developer))

	require.Equal(t, [][]string{assignment(member, "job_developer", account)}, rows.stored())
	require.NoError(t, service.enforcer.LoadPolicy())
	require.NoError(t, service.Enforce(ctx, member, account, JobAction_Create))
}

// Two changes of the role of one member made at once leave the member one of the two roles
// asked for, in the table and in memory: never both, never none.
func Test_SetRole_TwoChangesAtOnceLeaveOneOfTheRoles(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	account := someAccount()
	asked := map[mgmtv1alpha1.AccountRole]string{executor: "job_executor", viewer: "job_viewer"}

	for range 1500 {
		member := someone()
		require.NoError(t, service.SetRole(ctx, member, account, admin))
		var wg sync.WaitGroup
		for role := range asked {
			wg.Go(func() { require.NoError(t, service.SetRole(ctx, member, account, role)) })
		}
		wg.Wait()

		held := service.Roles([]User{member}, account)[member]
		require.Contains(t, asked, held)
		require.Equal(t, [][]string{assignment(member, asked[held], account)}, rows.storedFor(member))
		require.NoError(t, service.RemoveMember(ctx, member, account))
	}
}

// A role that is none is refused as a mistake of the caller, before anything is touched.
func Test_SetRole_RejectsARoleThatIsNone(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, account := someone(), someAccount()
	require.NoError(t, service.SetRole(ctx, member, account, admin))
	writes := rows.writes

	for _, role := range []mgmtv1alpha1.AccountRole{mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, 42} {
		err := service.SetRole(ctx, member, account, role)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	}
	require.Equal(t, writes, rows.writes)
	require.NoError(t, service.Enforce(ctx, member, account, AccountAction_Edit))
}

// A member removed holds nothing any more, whatever the table held; one that held nothing is
// removed all the same.
func Test_RemoveMember(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	member, other, account, elsewhere := someone(), someone(), someAccount(), someAccount()
	require.NoError(t, service.SetRole(ctx, member, account, admin))
	require.NoError(t, service.SetRole(ctx, member, elsewhere, viewer))
	require.NoError(t, service.SetRole(ctx, other, account, viewer))
	rows.write(assignment(member, "job_executor", account)...)

	require.NoError(t, service.RemoveMember(ctx, member, account))
	require.NoError(t, service.RemoveMember(ctx, someone(), account))

	requireRefused(t, service.Enforce(ctx, member, account, AccountAction_View), AccountAction_View)
	require.ElementsMatch(t, [][]string{
		assignment(member, "job_viewer", elsewhere),
		assignment(other, "job_viewer", account),
	}, rows.stored())
	require.NoError(t, service.Enforce(ctx, member, elsewhere, JobAction_View))
}

// The roles of the members of an account: one each; none for who has none; the highest for who
// has several, which is what they may do.
func Test_Roles(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	account, elsewhere := someAccount(), someAccount()
	one, several, none, unknown := someone(), someone(), someone(), someone()
	rows.write(assignment(several, "job_viewer", account)...)
	rows.write(assignment(several, "job_developer", account)...)
	rows.write(assignment(several, "job_executor", account)...)
	rows.write(assignment(none, "account_admin", elsewhere)...)
	rows.write(assignment(unknown, "owner", account)...)
	service := serviceOn(t, rows)
	require.NoError(t, service.SetRole(ctx, one, account, executor))

	require.Equal(t,
		map[User]mgmtv1alpha1.AccountRole{one: executor, several: developer},
		service.Roles([]User{one, several, none, unknown}, account),
	)
}

type accountsOfTest struct {
	members map[Account][]User
	failing error
	asked   []Account
}

func (a *accountsOfTest) Accounts(context.Context) ([]Account, error) {
	if a.failing != nil {
		return nil, a.failing
	}
	accounts := make([]Account, 0, len(a.members))
	for account := range a.members {
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (a *accountsOfTest) HumanMembers(_ context.Context, account Account) ([]User, error) {
	a.asked = append(a.asked, account)
	return a.members[account], nil
}

// At the start of the API, the people of an account where nobody has a role become its admins;
// an account where somebody has one is left alone, members without a role included.
func Test_GrantAdminWhereNoRole(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	withoutRoles, withARole, empty := someAccount(), someAccount(), someAccount()
	first, second, holder, forgotten := someone(), someone(), someone(), someone()
	require.NoError(t, service.SetRole(ctx, holder, withARole, viewer))
	accounts := &accountsOfTest{members: map[Account][]User{
		withoutRoles: {first, second},
		withARole:    {holder, forgotten},
		empty:        nil,
	}}

	granted, err := service.GrantAdminWhereNoRole(ctx, accounts)
	require.NoError(t, err)
	require.Equal(t, 2, granted)
	require.ElementsMatch(t, [][]string{
		assignment(holder, "job_viewer", withARole),
		assignment(first, "account_admin", withoutRoles),
		assignment(second, "account_admin", withoutRoles),
	}, rows.stored())
	require.NoError(t, service.Enforce(ctx, first, withoutRoles, AccountAction_Edit))
	requireRefused(t, service.Enforce(ctx, forgotten, withARole, AccountAction_View), AccountAction_View)
	require.NotContains(t, accounts.asked, withARole, "the members of an account that has roles are not read")

	// Done again, it gives nothing more.
	granted, err = service.GrantAdminWhereNoRole(ctx, accounts)
	require.NoError(t, err)
	require.Zero(t, granted)

	accounts.failing = errors.New("the database is down")
	_, err = service.GrantAdminWhereNoRole(ctx, accounts)
	require.ErrorIs(t, err, accounts.failing)
}

// Checks, changes of roles and reloads made together do not trip over one another.
func Test_Service_UnderConcurrentUse(t *testing.T) {
	ctx := context.Background()
	service := serviceOn(t, &memoryRows{})
	account := someAccount()
	roles := []mgmtv1alpha1.AccountRole{admin, developer, executor, viewer}

	var wg sync.WaitGroup
	for i := range 8 {
		member := someone()
		wg.Go(func() {
			for n := range 50 {
				require.NoError(t, service.SetRole(ctx, member, account, roles[(i+n)%len(roles)]))
				// Whatever the role of the moment, the member has one.
				require.NoError(t, service.Enforce(ctx, member, account, JobAction_View))
				service.Roles([]User{member}, account)
			}
			require.NoError(t, service.RemoveMember(ctx, member, account))
			requireRefused(t, service.Enforce(ctx, member, account, JobAction_View), JobAction_View)
		})
	}
	wg.Go(func() {
		for range 50 {
			require.NoError(t, service.enforcer.LoadPolicy())
		}
	})
	wg.Wait()
}
