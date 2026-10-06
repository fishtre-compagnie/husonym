package hooks_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/hooks"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	v1alpha1_accounthookservice "github.com/fishtre-compagnie/husonym/backend/services/mgmt/v1alpha1/account-hooks-service"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

const (
	theSecret = "s3cret-of-the-webhook"
	theMask   = "********"
)

func newID() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

func str(id pgtype.UUID) string { return husonymdb.UUIDString(id) }

// store is the database of the hooks, in memory: the queries the hooks use, and nothing else.
// Any other query panics.
type store struct {
	db_queries.Querier

	mu             sync.Mutex
	jobs           map[pgtype.UUID]pgtype.UUID // job -> account
	jobConnections map[pgtype.UUID]pgtype.UUID // job -> the connection it uses
	jobHooks       map[pgtype.UUID]db_queries.HusonymApiJobHook
	accountHooks   map[pgtype.UUID]db_queries.HusonymApiAccountHook
	writes         int
	clock          time.Time
	// beforeWrite runs before an update reaches the rows, as another caller's write would.
	beforeWrite func()
	// createFails is what the database answers a creation, when it refuses one.
	createFails error
}

func (s *store) aboutToWrite() {
	if s.beforeWrite != nil {
		s.beforeWrite()
	}
}

func newStore() *store {
	return &store{
		jobs:           map[pgtype.UUID]pgtype.UUID{},
		jobConnections: map[pgtype.UUID]pgtype.UUID{},
		jobHooks:       map[pgtype.UUID]db_queries.HusonymApiJobHook{},
		accountHooks:   map[pgtype.UUID]db_queries.HusonymApiAccountHook{},
		clock:          time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (s *store) now() pgtype.Timestamptz {
	s.clock = s.clock.Add(time.Second)
	return pgtype.Timestamptz{Time: s.clock, Valid: true}
}

// The refusals of the two constraints that keep a name to one hook.
var (
	errJobHookName     = &pgconn.PgError{Code: husonymdb.PqUniqueViolationCode, ConstraintName: "job_hooks_name_unique"}
	errAccountHookName = &pgconn.PgError{Code: husonymdb.PqUniqueViolationCode, ConstraintName: "account_hooks_name_unique"}
)

func (s *store) GetAccountIdFromJobId(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) (pgtype.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.jobs[id]
	if !ok {
		return pgtype.UUID{}, pgx.ErrNoRows
	}
	return account, nil
}

func (s *store) DoesJobHaveConnectionId(
	_ context.Context, _ db_queries.DBTX, arg db_queries.DoesJobHaveConnectionIdParams,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobConnections[arg.JobId] == arg.ConnectionId, nil
}

func (s *store) GetJobHookById(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) (db_queries.HusonymApiJobHook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hook, ok := s.jobHooks[id]
	if !ok {
		return db_queries.HusonymApiJobHook{}, pgx.ErrNoRows
	}
	return hook, nil
}

func (s *store) jobHooksWhere(keep func(db_queries.HusonymApiJobHook) bool) []db_queries.HusonymApiJobHook {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []db_queries.HusonymApiJobHook{}
	for _, hook := range s.jobHooks {
		if keep(hook) {
			out = append(out, hook)
		}
	}
	slices.SortFunc(out, func(a, b db_queries.HusonymApiJobHook) int {
		return cmp.Or(cmp.Compare(a.Priority, b.Priority), a.CreatedAt.Time.Compare(b.CreatedAt.Time))
	})
	return out
}

func (s *store) GetJobHooksByJob(_ context.Context, _ db_queries.DBTX, jobID pgtype.UUID) ([]db_queries.HusonymApiJobHook, error) {
	return s.jobHooksWhere(func(h db_queries.HusonymApiJobHook) bool { return h.JobID == jobID }), nil
}

func (s *store) GetActiveJobHooks(_ context.Context, _ db_queries.DBTX, jobID pgtype.UUID) ([]db_queries.HusonymApiJobHook, error) {
	return s.jobHooksWhere(func(h db_queries.HusonymApiJobHook) bool { return h.JobID == jobID && h.Enabled }), nil
}

func (s *store) GetActivePreSyncJobHooks(_ context.Context, _ db_queries.DBTX, jobID pgtype.UUID) ([]db_queries.HusonymApiJobHook, error) {
	return s.jobHooksWhere(func(h db_queries.HusonymApiJobHook) bool {
		return h.JobID == jobID && h.Enabled && h.HookTiming.String == "preSync"
	}), nil
}

func (s *store) GetActivePostSyncJobHooks(_ context.Context, _ db_queries.DBTX, jobID pgtype.UUID) ([]db_queries.HusonymApiJobHook, error) {
	return s.jobHooksWhere(func(h db_queries.HusonymApiJobHook) bool {
		return h.JobID == jobID && h.Enabled && h.HookTiming.String == "postSync"
	}), nil
}

func (s *store) IsJobHookNameAvailable(_ context.Context, _ db_queries.DBTX, arg db_queries.IsJobHookNameAvailableParams) (bool, error) {
	return len(s.jobHooksWhere(func(h db_queries.HusonymApiJobHook) bool {
		return h.JobID == arg.JobID && h.Name == arg.Name
	})) == 0, nil
}

func (s *store) CreateJobHook(_ context.Context, _ db_queries.DBTX, arg db_queries.CreateJobHookParams) (db_queries.HusonymApiJobHook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createFails != nil {
		return db_queries.HusonymApiJobHook{}, s.createFails
	}
	for _, existing := range s.jobHooks {
		if existing.JobID == arg.JobID && existing.Name == arg.Name {
			return db_queries.HusonymApiJobHook{}, errJobHookName
		}
	}
	s.writes++
	now := s.now()
	hook := db_queries.HusonymApiJobHook{
		ID: newID(), Name: arg.Name, Description: arg.Description, JobID: arg.JobID, Config: arg.Config,
		CreatedByUserID: arg.CreatedByUserID, UpdatedByUserID: arg.UpdatedByUserID,
		CreatedAt: now, UpdatedAt: now, Enabled: arg.Enabled, Priority: arg.Priority,
	}
	s.jobHooks[hook.ID] = hook
	return hook, nil
}

func (s *store) UpdateJobHook(_ context.Context, _ db_queries.DBTX, arg db_queries.UpdateJobHookParams) (db_queries.HusonymApiJobHook, error) {
	s.aboutToWrite()
	s.mu.Lock()
	defer s.mu.Unlock()
	hook, ok := s.jobHooks[arg.ID]
	if !ok {
		return db_queries.HusonymApiJobHook{}, pgx.ErrNoRows
	}
	for id, existing := range s.jobHooks {
		if id != arg.ID && existing.JobID == hook.JobID && existing.Name == arg.Name {
			return db_queries.HusonymApiJobHook{}, errJobHookName
		}
	}
	s.writes++
	hook.Name, hook.Description, hook.Config = arg.Name, arg.Description, arg.Config
	hook.Enabled, hook.Priority, hook.UpdatedByUserID = arg.Enabled, arg.Priority, arg.UpdatedByUserID
	s.jobHooks[arg.ID] = hook
	return hook, nil
}

func (s *store) SetJobHookEnabled(_ context.Context, _ db_queries.DBTX, arg db_queries.SetJobHookEnabledParams) (db_queries.HusonymApiJobHook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hook, ok := s.jobHooks[arg.ID]
	if !ok {
		return db_queries.HusonymApiJobHook{}, pgx.ErrNoRows
	}
	s.writes++
	hook.Enabled, hook.UpdatedByUserID = arg.Enabled, arg.UpdatedByUserID
	s.jobHooks[arg.ID] = hook
	return hook, nil
}

func (s *store) RemoveJobHookById(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	delete(s.jobHooks, id)
	return nil
}

func (s *store) GetAccountHookById(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) (db_queries.HusonymApiAccountHook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hook, ok := s.accountHooks[id]
	if !ok {
		return db_queries.HusonymApiAccountHook{}, pgx.ErrNoRows
	}
	return hook, nil
}

func (s *store) accountHooksWhere(keep func(db_queries.HusonymApiAccountHook) bool) []db_queries.HusonymApiAccountHook {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []db_queries.HusonymApiAccountHook{}
	for _, hook := range s.accountHooks {
		if keep(hook) {
			out = append(out, hook)
		}
	}
	slices.SortFunc(out, func(a, b db_queries.HusonymApiAccountHook) int {
		return a.CreatedAt.Time.Compare(b.CreatedAt.Time)
	})
	return out
}

func (s *store) GetAccountHooksByAccount(_ context.Context, _ db_queries.DBTX, accountID pgtype.UUID) ([]db_queries.HusonymApiAccountHook, error) {
	return s.accountHooksWhere(func(h db_queries.HusonymApiAccountHook) bool { return h.AccountID == accountID }), nil
}

func (s *store) GetActiveAccountHooksByEvent(
	_ context.Context, _ db_queries.DBTX, arg db_queries.GetActiveAccountHooksByEventParams,
) ([]db_queries.HusonymApiAccountHook, error) {
	return s.accountHooksWhere(func(h db_queries.HusonymApiAccountHook) bool {
		if h.AccountID != arg.AccountID || !h.Enabled {
			return false
		}
		return slices.ContainsFunc(h.Events, func(event int32) bool { return slices.Contains(arg.Events, event) })
	}), nil
}

func (s *store) IsAccountHookNameAvailable(_ context.Context, _ db_queries.DBTX, arg db_queries.IsAccountHookNameAvailableParams) (bool, error) {
	return len(s.accountHooksWhere(func(h db_queries.HusonymApiAccountHook) bool {
		return h.AccountID == arg.AccountID && h.Name == arg.Name
	})) == 0, nil
}

func (s *store) CreateAccountHook(_ context.Context, _ db_queries.DBTX, arg db_queries.CreateAccountHookParams) (db_queries.HusonymApiAccountHook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createFails != nil {
		return db_queries.HusonymApiAccountHook{}, s.createFails
	}
	for _, existing := range s.accountHooks {
		if existing.AccountID == arg.AccountID && existing.Name == arg.Name {
			return db_queries.HusonymApiAccountHook{}, errAccountHookName
		}
	}
	s.writes++
	now := s.now()
	hook := db_queries.HusonymApiAccountHook{
		ID: newID(), Name: arg.Name, Description: arg.Description, AccountID: arg.AccountID,
		Events: arg.Events, Config: arg.Config,
		CreatedByUserID: arg.CreatedByUserID, UpdatedByUserID: arg.UpdatedByUserID,
		CreatedAt: now, UpdatedAt: now, Enabled: arg.Enabled,
	}
	s.accountHooks[hook.ID] = hook
	return hook, nil
}

func (s *store) UpdateAccountHook(_ context.Context, _ db_queries.DBTX, arg db_queries.UpdateAccountHookParams) (db_queries.HusonymApiAccountHook, error) {
	s.aboutToWrite()
	s.mu.Lock()
	defer s.mu.Unlock()
	hook, ok := s.accountHooks[arg.ID]
	if !ok {
		return db_queries.HusonymApiAccountHook{}, pgx.ErrNoRows
	}
	for id, existing := range s.accountHooks {
		if id != arg.ID && existing.AccountID == hook.AccountID && existing.Name == arg.Name {
			return db_queries.HusonymApiAccountHook{}, errAccountHookName
		}
	}
	s.writes++
	hook.Name, hook.Description, hook.Events, hook.Config = arg.Name, arg.Description, arg.Events, arg.Config
	hook.Enabled, hook.UpdatedByUserID = arg.Enabled, arg.UpdatedByUserID
	s.accountHooks[arg.ID] = hook
	return hook, nil
}

func (s *store) SetAccountHookEnabled(_ context.Context, _ db_queries.DBTX, arg db_queries.SetAccountHookEnabledParams) (db_queries.HusonymApiAccountHook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hook, ok := s.accountHooks[arg.ID]
	if !ok {
		return db_queries.HusonymApiAccountHook{}, pgx.ErrNoRows
	}
	s.writes++
	hook.Enabled, hook.UpdatedByUserID = arg.Enabled, arg.UpdatedByUserID
	s.accountHooks[arg.ID] = hook
	return hook, nil
}

func (s *store) RemoveAccountHookById(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	delete(s.accountHooks, id)
	return nil
}

func (s *store) addJobHook(jobID pgtype.UUID, name, config, timing string, enabled bool, priority int32) pgtype.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	hook := db_queries.HusonymApiJobHook{
		ID: newID(), Name: name, Description: "a job hook", JobID: jobID, Config: []byte(config),
		CreatedByUserID: newID(), UpdatedByUserID: newID(), CreatedAt: now, UpdatedAt: now,
		Enabled: enabled, Priority: priority,
		HookTiming: pgtype.Text{String: timing, Valid: timing != ""},
	}
	s.jobHooks[hook.ID] = hook
	return hook.ID
}

func (s *store) addAccountHook(accountID pgtype.UUID, name, config string, enabled bool, events ...int32) pgtype.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	hook := db_queries.HusonymApiAccountHook{
		ID: newID(), Name: name, Description: "an account hook", AccountID: accountID,
		Events: events, Config: []byte(config),
		CreatedByUserID: newID(), UpdatedByUserID: newID(), CreatedAt: now, UpdatedAt: now, Enabled: enabled,
	}
	s.accountHooks[hook.ID] = hook
	return hook.ID
}

// people answers who the caller is and which accounts the caller belongs to.
type people struct {
	userID  string
	members map[string]bool
}

func (p *people) GetUser(
	context.Context, *connect.Request[mgmtv1alpha1.GetUserRequest],
) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: p.userID}), nil
}

func (p *people) IsUserInAccount(
	_ context.Context, req *connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{Ok: p.members[req.Msg.GetAccountId()]}), nil
}

// role is what the caller may do in the accounts the caller belongs to. It keeps the trail of
// what was asked, in order.
type role struct {
	grants map[string]bool
	trail  *[]string
	// fails is the failure to decide, when the roles cannot be read.
	fails error
}

func (r *role) Allowed(_ context.Context, _ rbac.User, _ rbac.Account, action rbac.Action) (bool, error) {
	asked := action.Kind() + ":" + action.String()
	*r.trail = append(*r.trail, asked)
	return r.grants[asked], r.fails
}

func (r *role) Enforce(ctx context.Context, user rbac.User, account rbac.Account, action rbac.Action) error {
	allowed, err := r.Allowed(ctx, user, account, action)
	if err != nil {
		return err
	}
	if !allowed {
		return husonymerrors.NewForbidden(
			fmt.Sprintf("user does not have permission to %s %s", action, action.Kind()),
		)
	}
	return nil
}

// papers is the license of the deployment. Asking it leaves "license" on the trail.
type papers struct {
	valid bool
	trail *[]string
}

func (p *papers) IsValid() bool {
	*p.trail = append(*p.trail, "license")
	return p.valid
}
func (*papers) ExpiresAt() time.Time    { return time.Time{} }
func (*papers) Limits() *license.Limits { return nil }

// HasFeature leaves nothing on the trail: a valid license allows every feature, an invalid one
// none.
func (p *papers) HasFeature(license.Feature) bool { return p.valid }

var (
	adminGrants = map[string]bool{
		"account:view": true, "account:edit": true,
		"job:view": true, "job:create": true, "job:edit": true, "job:execute": true, "job:delete": true,
	}
	viewerGrants = map[string]bool{"account:view": true, "job:view": true}
)

// world is two accounts, each with a job and hooks of both kinds, and one caller.
type world struct {
	store   *store
	people  *people
	role    *role
	papers  *papers
	trail   []string
	jobs    *hooks.JobService
	account *v1alpha1_accounthookservice.Service

	own, other place
}

// place is what one account holds.
type place struct {
	accountID  pgtype.UUID
	jobID      pgtype.UUID
	connection pgtype.UUID
	jobHook    pgtype.UUID // enabled, pre-sync
	jobHookOff pgtype.UUID // disabled, post-sync
	webhook    pgtype.UUID // enabled, listens to failed runs
	webhookOff pgtype.UUID // disabled, listens to every event
	slack      pgtype.UUID // enabled, of the retired kind
}

func sqlConfig(connection pgtype.UUID, timing string) string {
	return fmt.Sprintf(`{"sql":{"query":"select 1;","connectionId":%q,"timing":{%q:{}}}}`, str(connection), timing)
}

func webhookConfig(url string) string {
	return fmt.Sprintf(`{"webhook":{"url":%q,"secret":%q}}`, url, theSecret)
}

func (w *world) settle(prefix string) place {
	p := place{accountID: newID(), jobID: newID(), connection: newID()}
	w.store.jobs[p.jobID] = p.accountID
	w.store.jobConnections[p.jobID] = p.connection
	p.jobHook = w.store.addJobHook(p.jobID, prefix+"-pre", sqlConfig(p.connection, "preSync"), "preSync", true, 10)
	p.jobHookOff = w.store.addJobHook(p.jobID, prefix+"-post", sqlConfig(p.connection, "postSync"), "postSync", false, 20)
	p.webhook = w.store.addAccountHook(p.accountID, prefix+"-webhook", webhookConfig("https://example.com/hook"), true, 2)
	p.webhookOff = w.store.addAccountHook(p.accountID, prefix+"-webhook-off", webhookConfig("https://example.com/off"), false, 0)
	p.slack = w.store.addAccountHook(p.accountID, prefix+"-slack", `{"slack":{"channelId":"C0123"}}`, true, 0)
	return p
}

// newWorld is a world of a deployment with authentication on: the worker is who holds the
// worker's key.
func newWorld(t *testing.T) *world {
	t.Helper()
	return newWorldOf(t, userdata.WorkerOnly{IsAuthEnabled: true})
}

func newWorldOf(t *testing.T, workerOnly userdata.WorkerOnly) *world {
	t.Helper()
	w := &world{store: newStore()}
	w.people = &people{userID: uuid.NewString(), members: map[string]bool{}}
	w.role = &role{grants: adminGrants, trail: &w.trail}
	w.papers = &papers{valid: true, trail: &w.trail}
	w.own = w.settle("own")
	w.other = w.settle("other")
	w.people.members[str(w.own.accountID)] = true

	db := husonymdb.New(nil, w.store)
	users := userdata.NewClient(w.people, w.role, w.papers)
	w.jobs = hooks.NewJobService(db, users)
	w.account = v1alpha1_accounthookservice.New(hooks.NewAccountService(db, users, workerOnly))
	return w
}

// asKey gives a context that carries an API key of an account, scoped to the permissions
// named.
func asKey(ctx context.Context, accountID pgtype.UUID, permissions ...string) context.Context {
	return auth_apikey.SetTokenData(ctx, &auth_apikey.TokenContextData{
		ApiKeyType: apikey.AccountApiKey,
		ApiKey:     &db_queries.HusonymApiAccountApiKey{AccountID: accountID, Permissions: permissions},
	})
}

// asWorker gives a context that carries the worker's key.
func asWorker(ctx context.Context) context.Context {
	return auth_apikey.SetTokenData(ctx, &auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey})
}

func requireAnswer(t *testing.T, err error, code connect.Code, message string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, code, connect.CodeOf(err), "%v", err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	require.Equal(t, message, connectErr.Message())
}
