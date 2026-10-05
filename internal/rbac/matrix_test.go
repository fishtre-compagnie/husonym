package rbac

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// roleNames are the roles as the table stores them.
var roleNames = []string{"account_admin", "job_developer", "job_executor", "job_viewer"}

// publishedMatrix is what each role may do, written out: one line per action, one answer per
// role, in the order of roleNames. It is the reference the rules are held to; a change of what
// a role may do has to be made here too.
var publishedMatrix = []struct {
	action  Action
	allowed [4]bool
}{
	{AccountAction_View, [4]bool{true, true, true, true}},
	{AccountAction_Edit, [4]bool{true, false, false, false}},
	{AccountAction_Create, [4]bool{true, false, false, false}},
	{AccountAction_Delete, [4]bool{true, false, false, false}},
	{ConnectionAction_View, [4]bool{true, true, true, true}},
	{ConnectionAction_ViewSensitive, [4]bool{true, true, false, false}},
	{ConnectionAction_Create, [4]bool{true, true, false, false}},
	{ConnectionAction_Edit, [4]bool{true, true, false, false}},
	{ConnectionAction_Delete, [4]bool{true, true, false, false}},
	{JobAction_View, [4]bool{true, true, true, true}},
	{JobAction_Execute, [4]bool{true, true, true, false}},
	{JobAction_Create, [4]bool{true, true, false, false}},
	{JobAction_Edit, [4]bool{true, true, false, false}},
	{JobAction_Delete, [4]bool{true, true, false, false}},
}

// The words of the actions and their kinds are those the table and the API keys name.
func Test_Actions_AreSpelledAsStored(t *testing.T) {
	spelled := map[string][]string{}
	for _, action := range Actions() {
		spelled[action.Kind()] = append(spelled[action.Kind()], action.String())
	}
	require.Equal(t, map[string][]string{
		"account":    {"view", "edit", "create", "delete"},
		"connection": {"view", "view_sensitive", "create", "edit", "delete"},
		"job":        {"view", "execute", "create", "edit", "delete"},
	}, spelled)

	var published []Action
	for _, line := range publishedMatrix {
		published = append(published, line.action)
	}
	require.Equal(t, Actions(), published, "the matrix answers for every action, and for no other")
}

// Each role may do exactly what the matrix says, in the account it is held in; without a role,
// or with a role held in another account, nothing is allowed.
func Test_Matrix_EqualsThePublishedTable(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	account, elsewhere := someAccount(), someAccount()

	for i, roleName := range roleNames {
		member := someone()
		role := highestRole([]string{roleName})
		require.NotEqual(t, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_UNSPECIFIED, role)
		require.NoError(t, service.SetRole(ctx, member, account, role))
		require.Contains(t, rows.stored(), assignment(member, roleName, account), "the role is stored as the table spells it")

		for _, line := range publishedMatrix {
			t.Run(roleName+" "+line.action.Kind()+" "+line.action.String(), func(t *testing.T) {
				allowed, err := service.Allowed(ctx, member, account, line.action)
				require.NoError(t, err)
				require.Equal(t, line.allowed[i], allowed)

				err = service.Enforce(ctx, member, account, line.action)
				if line.allowed[i] {
					require.NoError(t, err)
				} else {
					requireRefused(t, err, line.action)
				}

				allowed, err = service.Allowed(ctx, member, elsewhere, line.action)
				require.NoError(t, err)
				require.False(t, allowed, "a role in one account gives nothing in another")
			})
		}
	}

	stranger := someone()
	for _, line := range publishedMatrix {
		requireRefused(t, service.Enforce(ctx, stranger, account, line.action), line.action)
	}
}

func requireRefused(t *testing.T, err error, action Action) {
	t.Helper()
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	require.ErrorContains(t, err, "user does not have permission to "+action.String()+" "+action.Kind())
}

// The refusals read as the clients of the API know them.
func Test_Enforce_RefusalMessages(t *testing.T) {
	ctx := context.Background()
	service := serviceOn(t, &memoryRows{})
	for action, message := range map[Action]string{
		AccountAction_Edit:    "user does not have permission to edit account",
		AccountAction_View:    "user does not have permission to view account",
		JobAction_Create:      "user does not have permission to create job",
		JobAction_Execute:     "user does not have permission to execute job",
		JobAction_Delete:      "user does not have permission to delete job",
		JobAction_Edit:        "user does not have permission to edit job",
		JobAction_View:        "user does not have permission to view job",
		ConnectionAction_View: "user does not have permission to view connection",
	} {
		err := service.Enforce(ctx, someone(), someAccount(), action)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.EqualError(t, err, "permission_denied: "+message)
	}
}

// The rules are the same eleven in every account, written once.
func Test_Matrix_FixedRules(t *testing.T) {
	require.Equal(t, [][]string{
		{"account_admin", "*", "*", "*"},
		{"job_developer", "*", "jobs/*", "*"},
		{"job_developer", "*", "connections/*", "*"},
		{"job_developer", "*", "accounts/*", "view"},
		{"job_executor", "*", "jobs/*", "view"},
		{"job_executor", "*", "connections/*", "view"},
		{"job_executor", "*", "accounts/*", "view"},
		{"job_executor", "*", "jobs/*", "execute"},
		{"job_viewer", "*", "jobs/*", "view"},
		{"job_viewer", "*", "connections/*", "view"},
		{"job_viewer", "*", "accounts/*", "view"},
	}, fixedRules())
}

// rulesStoredPerAccount is how a database may hold the rules: eleven rows for each account,
// naming it, decided under the model below.
func rulesStoredPerAccount(account Account) [][]string {
	a := account.stored()
	return [][]string{
		{"p", "account_admin", a, "*", "*"},
		{"p", "job_developer", a, "jobs/*", "*"},
		{"p", "job_developer", a, "connections/*", "*"},
		{"p", "job_developer", a, a, "view"},
		{"p", "job_executor", a, "jobs/*", "view"},
		{"p", "job_executor", a, "connections/*", "view"},
		{"p", "job_executor", a, a, "view"},
		{"p", "job_executor", a, "jobs/*", "execute"},
		{"p", "job_viewer", a, "jobs/*", "view"},
		{"p", "job_viewer", a, "connections/*", "view"},
		{"p", "job_viewer", a, a, "view"},
	}
}

const modelOfRulesStoredPerAccount = `
[request_definition]
r = sub, dom, obj, act

[policy_definition]
p = sub, dom, obj, act

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && r.dom == p.dom && keyMatch(r.obj, p.obj) && keyMatch(r.act, p.act)
`

// decidedByStoredRows decides from every row of a table, rules included, under the model the
// rules stored per account are decided with.
func decidedByStoredRows(t *testing.T, rows [][]string) *casbin.Enforcer {
	t.Helper()
	m, err := model.NewModelFromString(modelOfRulesStoredPerAccount)
	require.NoError(t, err)
	decided, err := casbin.NewEnforcer(m)
	require.NoError(t, err)
	for _, row := range rows {
		switch row[0] {
		case "p":
			_, err = decided.AddPolicy(row[1:])
		case "g":
			_, err = decided.AddGroupingPolicy(row[1:])
		}
		require.NoError(t, err)
	}
	return decided
}

// A database that holds the eleven rules of each account, and the roles of its members, gives
// every member the access those rows give — for one object as for all of a kind, for an action
// nobody named yet, in the account of the role and in another — and is not written to.
func Test_Matrix_DecidesAsTheStoredRules(t *testing.T) {
	ctx := context.Background()
	accounts := []Account{someAccount(), someAccount()}
	members := map[string]User{"nobody": someone()}
	rows := &memoryRows{}
	for _, account := range accounts {
		rows.rows = append(rows.rows, rulesStoredPerAccount(account)...)
	}
	for _, roleName := range roleNames {
		members[roleName] = someone()
		rows.write(assignment(members[roleName], roleName, accounts[0])...)
	}
	// Somebody holds one role in each account.
	members["two accounts"] = someone()
	rows.write(assignment(members["two accounts"], "job_viewer", accounts[0])...)
	rows.write(assignment(members["two accounts"], "account_admin", accounts[1])...)
	before := rows.stored()

	stored := decidedByStoredRows(t, before)
	service := serviceOn(t, rows)

	actions := append(Actions(), AccountAction("archive"), ConnectionAction("archive"), JobAction("archive"))
	for name, member := range members {
		for _, account := range accounts {
			for _, action := range actions {
				objects := map[string][]string{
					"account":    {account.stored()},
					"connection": {"connections/*", "connections/5d1c7f0a-3a52-4d0e-9a0e-0d1f6c2b7a11"},
					"job":        {"jobs/*", "jobs/0b6f1c1e-8f4b-4f6e-b0a3-6a1d3c9e2f47"},
				}[action.Kind()]

				allowed, err := service.Allowed(ctx, member, account, action)
				require.NoError(t, err)
				for _, object := range objects {
					want, err := stored.Enforce(member.stored(), account.stored(), object, action.String())
					require.NoError(t, err)
					require.Equal(t, want, allowed, "%s, %s %s on %s", name, action.Kind(), action, object)
				}
			}
		}
	}

	require.Equal(t, before, rows.stored())
	require.Zero(t, rows.writes, "reading the roles writes nothing")
}

// On a database that holds nothing yet, a role given is what is stored, and nothing else.
func Test_Service_OnAnEmptyTable(t *testing.T) {
	ctx := context.Background()
	rows := &memoryRows{}
	service := serviceOn(t, rows)
	creator, account := someone(), someAccount()

	require.NoError(t, service.SetRole(ctx, creator, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN))

	require.NoError(t, service.Enforce(ctx, creator, account, AccountAction_Edit))
	require.Equal(t, [][]string{assignment(creator, "account_admin", account)}, rows.stored())
}
