package rbac

import (
	"context"
	"testing"
	"time"

	"github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// allowedTo is what each role may do, as the table of the rules names the roles: the actions
// listed, and no other.
var allowedTo = map[string][]rbac.Action{
	"account_admin": rbac.Actions(),
	"job_developer": {
		rbac.AccountAction_View,
		rbac.ConnectionAction_View, rbac.ConnectionAction_ViewSensitive, rbac.ConnectionAction_Create,
		rbac.ConnectionAction_Edit, rbac.ConnectionAction_Delete,
		rbac.JobAction_View, rbac.JobAction_Execute, rbac.JobAction_Create, rbac.JobAction_Edit, rbac.JobAction_Delete,
	},
	"job_executor": {rbac.AccountAction_View, rbac.ConnectionAction_View, rbac.JobAction_View, rbac.JobAction_Execute},
	"job_viewer":   {rbac.AccountAction_View, rbac.ConnectionAction_View, rbac.JobAction_View},
}

// modelOfRulesStoredPerAccount is the model a table holding eleven rules for each account is
// decided with.
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

type storedRow struct {
	Kind, V0, V1, V2, V3 string
	UpdatedAt            time.Time
}

func storedRows(ctx context.Context, t *testing.T, db *pgxpool.Pool) []storedRow {
	t.Helper()
	rows, err := db.Query(ctx,
		"SELECT p_type, v0, v1, v2, v3, updated_at FROM husonym_api.casbin_rule ORDER BY p_type, v0, v1, v2, v3")
	require.NoError(t, err)
	defer rows.Close()
	var stored []storedRow
	for rows.Next() {
		var row storedRow
		require.NoError(t, rows.Scan(&row.Kind, &row.V0, &row.V1, &row.V2, &row.V3, &row.UpdatedAt))
		stored = append(stored, row)
	}
	require.NoError(t, rows.Err())
	return stored
}

func storeRow(ctx context.Context, t *testing.T, db *pgxpool.Pool, kind string, values ...string) {
	t.Helper()
	padded := append(values, "", "", "", "")[:4]
	_, err := db.Exec(ctx,
		"INSERT INTO husonym_api.casbin_rule (p_type, v0, v1, v2, v3) VALUES ($1, $2, $3, $4, $5)",
		kind, padded[0], padded[1], padded[2], padded[3])
	require.NoError(t, err)
}

// storeRulesOf writes the eleven rules of an account, naming it.
func storeRulesOf(ctx context.Context, t *testing.T, db *pgxpool.Pool, accountId string) {
	t.Helper()
	a := "accounts/" + accountId
	for _, rule := range [][]string{
		{"account_admin", a, "*", "*"},
		{"job_developer", a, "jobs/*", "*"},
		{"job_developer", a, "connections/*", "*"},
		{"job_developer", a, a, "view"},
		{"job_executor", a, "jobs/*", "view"},
		{"job_executor", a, "connections/*", "view"},
		{"job_executor", a, a, "view"},
		{"job_executor", a, "jobs/*", "execute"},
		{"job_viewer", a, "jobs/*", "view"},
		{"job_viewer", a, "connections/*", "view"},
		{"job_viewer", a, a, "view"},
	} {
		storeRow(ctx, t, db, "p", rule...)
	}
}

// requireAccess holds a member to exactly the actions given.
func requireAccess(ctx context.Context, t *testing.T, service *rbac.Service, userId, accountId string, allowed []rbac.Action) {
	t.Helper()
	for _, action := range rbac.Actions() {
		got, err := service.Allowed(ctx, rbac.NewUser(userId), rbac.NewAccount(accountId), action)
		require.NoError(t, err)
		want := false
		for _, a := range allowed {
			want = want || a == action
		}
		require.Equal(t, want, got, "%s %s", action.Kind(), action)
	}
}

func TestRbacStoredRows(t *testing.T) {
	t.Parallel()
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	container := migratedDatabase(t.Context(), t)
	db := container.DB
	emptied := func(ctx context.Context, t *testing.T) {
		t.Helper()
		_, err := db.Exec(ctx, "TRUNCATE husonym_api.casbin_rule")
		require.NoError(t, err)
	}

	// A database that holds the eleven rules of each account and the roles of its members:
	// every member has the access those rows give, and nothing is written.
	t.Run("a database holding the rules of each account", func(t *testing.T) {
		ctx := t.Context()
		emptied(ctx, t)
		account, other := uuid.NewString(), uuid.NewString()
		storeRulesOf(ctx, t, db, account)
		storeRulesOf(ctx, t, db, other)
		members := map[string]string{}
		for role := range allowedTo {
			members[role] = uuid.NewString()
			storeRow(ctx, t, db, "g", "users/"+members[role], role, "accounts/"+account)
		}
		before := storedRows(ctx, t, db)
		require.Len(t, before, 26)

		service, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)

		for role, member := range members {
			requireAccess(ctx, t, service, member, account, allowedTo[role])
			requireAccess(ctx, t, service, member, other, nil)
		}
		requireAccess(ctx, t, service, uuid.NewString(), account, nil)
		require.Equal(t, before, storedRows(ctx, t, db), "reading the roles wrote to the table")
	})

	// The rows a change of role leaves are those a table of rules stored per account is
	// decided with: the member has the same access under them.
	t.Run("the roles written decide the same with rules stored per account", func(t *testing.T) {
		ctx := t.Context()
		emptied(ctx, t)
		account := uuid.NewString()
		storeRulesOf(ctx, t, db, account)
		service, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)

		promoted, removed := uuid.NewString(), uuid.NewString()
		a := rbac.NewAccount(account)
		require.NoError(t, service.SetRole(ctx, rbac.NewUser(promoted), a, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER))
		require.NoError(t, service.SetRole(ctx, rbac.NewUser(promoted), a, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_DEVELOPER))
		require.NoError(t, service.SetRole(ctx, rbac.NewUser(removed), a, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN))
		require.NoError(t, service.RemoveMember(ctx, rbac.NewUser(removed), a))

		m, err := model.NewModelFromString(modelOfRulesStoredPerAccount)
		require.NoError(t, err)
		decided, err := casbin.NewEnforcer(m)
		require.NoError(t, err)
		stored := storedRows(ctx, t, db)
		require.Len(t, stored, 12, "the eleven rules, and the one role left")
		for _, row := range stored {
			if row.Kind == "p" {
				_, err = decided.AddPolicy(row.V0, row.V1, row.V2, row.V3)
			} else {
				_, err = decided.AddGroupingPolicy(row.V0, row.V1, row.V2)
			}
			require.NoError(t, err)
		}
		for _, member := range []string{promoted, removed} {
			for _, action := range rbac.Actions() {
				object := map[string]string{
					"account": "accounts/" + account, "connection": "connections/" + uuid.NewString(), "job": "jobs/" + uuid.NewString(),
				}[action.Kind()]
				want, err := decided.Enforce("users/"+member, "accounts/"+account, object, action.String())
				require.NoError(t, err)
				got, err := service.Allowed(ctx, rbac.NewUser(member), a, action)
				require.NoError(t, err)
				require.Equal(t, want, got, "%s %s", action.Kind(), action)
			}
		}
		requireAccess(ctx, t, service, promoted, account, allowedTo["job_developer"])
	})

	// A database that holds nothing yet stores the roles given, and no rule.
	t.Run("a fresh database", func(t *testing.T) {
		ctx := t.Context()
		emptied(ctx, t)
		service, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)
		creator, account := uuid.NewString(), uuid.NewString()

		require.NoError(t, service.SetRole(ctx, rbac.NewUser(creator), rbac.NewAccount(account), mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN))

		requireAccess(ctx, t, service, creator, account, allowedTo["account_admin"])
		stored := storedRows(ctx, t, db)
		require.Len(t, stored, 1)
		require.Equal(t,
			[]string{"g", "users/" + creator, "account_admin", "accounts/" + account, ""},
			[]string{stored[0].Kind, stored[0].V0, stored[0].V1, stored[0].V2, stored[0].V3},
		)

		// An API that starts on those rows gives the same access.
		restarted, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)
		requireAccess(ctx, t, restarted, creator, account, allowedTo["account_admin"])
	})

	// A row that is not a role assignment does not keep the API from starting, nor the other
	// members from their roles.
	t.Run("a row that is no assignment", func(t *testing.T) {
		ctx := t.Context()
		emptied(ctx, t)
		member, account := uuid.NewString(), uuid.NewString()
		storeRow(ctx, t, db, "g", "users/"+uuid.NewString(), "account_admin")
		storeRow(ctx, t, db, "g", "users/"+member, "job_viewer", "accounts/"+account)

		service, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)
		requireAccess(ctx, t, service, member, account, allowedTo["job_viewer"])
	})

	// The API starts on a database that already holds accounts and members: the people of an
	// account where nobody has a role become its admins, the other accounts are left as they
	// are, and no rule is written.
	t.Run("a start on a database that holds accounts and members", func(t *testing.T) {
		ctx := t.Context()
		emptied(ctx, t)
		newAccount := func() string {
			id := uuid.NewString()
			_, err := db.Exec(ctx, "INSERT INTO husonym_api.accounts (id, account_type, account_slug) VALUES ($1, 1, $2)", id, "team-"+id)
			require.NoError(t, err)
			return id
		}
		newMember := func(account string, userType int) string {
			id := uuid.NewString()
			_, err := db.Exec(ctx, "INSERT INTO husonym_api.users (id, user_type) VALUES ($1, $2)", id, userType)
			require.NoError(t, err)
			_, err = db.Exec(ctx, "INSERT INTO husonym_api.account_user_associations (account_id, user_id) VALUES ($1, $2)", account, id)
			require.NoError(t, err)
			return id
		}
		withoutRoles, withRoles, empty := newAccount(), newAccount(), newAccount()
		first, second := newMember(withoutRoles, 0), newMember(withoutRoles, 0)
		apiKey := newMember(withoutRoles, 1)
		viewer, forgotten := newMember(withRoles, 0), newMember(withRoles, 0)
		storeRulesOf(ctx, t, db, withRoles)
		storeRow(ctx, t, db, "g", "users/"+viewer, "job_viewer", "accounts/"+withRoles)

		start := func() (*rbac.Service, int) {
			service, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
			require.NoError(t, err)
			granted, err := service.GrantAdminWhereNoRole(ctx, rbac.NewAccounts(db_queries.New(), db))
			require.NoError(t, err)
			return service, granted
		}
		service, granted := start()

		require.Equal(t, 2, granted)
		requireAccess(ctx, t, service, first, withoutRoles, allowedTo["account_admin"])
		requireAccess(ctx, t, service, second, withoutRoles, allowedTo["account_admin"])
		requireAccess(ctx, t, service, apiKey, withoutRoles, nil)
		requireAccess(ctx, t, service, viewer, withRoles, allowedTo["job_viewer"])
		requireAccess(ctx, t, service, forgotten, withRoles, nil)
		requireAccess(ctx, t, service, first, empty, nil)
		stored := storedRows(ctx, t, db)
		require.Len(t, stored, 11+1+2, "the rules that were there, the role that was there, the two admins")

		// A second start, as that of another instance, gives nothing more.
		service, granted = start()
		require.Zero(t, granted)
		require.Equal(t, stored, storedRows(ctx, t, db))
		requireAccess(ctx, t, service, first, withoutRoles, allowedTo["account_admin"])
	})

	// Another instance of the API learns of a change of role when it reads the table again,
	// which it does every ten seconds.
	t.Run("another instance sees a change of role", func(t *testing.T) {
		ctx := t.Context()
		emptied(ctx, t)
		here, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)
		there, err := rbac.New(ctx, db, testutil.GetTestLogger(t))
		require.NoError(t, err)
		member, account := rbac.NewUser(uuid.NewString()), rbac.NewAccount(uuid.NewString())
		seenThere := func(want bool) func() bool {
			return func() bool {
				allowed, err := there.Allowed(ctx, member, account, rbac.JobAction_View)
				return err == nil && allowed == want
			}
		}

		require.NoError(t, here.SetRole(ctx, member, account, mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER))
		require.NoError(t, here.Enforce(ctx, member, account, rbac.JobAction_View))
		require.Eventually(t, seenThere(true), 20*time.Second, 200*time.Millisecond)

		require.NoError(t, here.RemoveMember(ctx, member, account))
		require.Eventually(t, seenThere(false), 20*time.Second, 200*time.Millisecond)
	})
}
