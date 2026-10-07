package licensegate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	job_util "github.com/fishtre-compagnie/husonym/internal/job"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// usageStore is the database of the usage, in memory: what the gate reads of a job, and the
// queries that tell what an account holds. Each of them answers for the account it is asked
// about, as the real ones do.
type usageStore struct {
	*gateStore

	connections  []db_queries.HusonymApiConnection
	accountHooks []db_queries.HusonymApiAccountHook
	apiKeys      []db_queries.HusonymApiAccountApiKey
	providers    map[pgtype.UUID][]byte
	members      map[pgtype.UUID][]pgtype.UUID
	// down is what the database answers the query of that name, when it does not answer it.
	down map[string]error
}

func newUsageStore() *usageStore {
	return &usageStore{
		gateStore: newGateStore(),
		providers: map[pgtype.UUID][]byte{},
		members:   map[pgtype.UUID][]pgtype.UUID{},
		down:      map[string]error{},
	}
}

// usageQueries are the queries the usage is read with.
var usageQueries = []string{
	"ListJobSourcesOfInstance", "GetConnectionsByAccount", "GetJobsByAccount", "GetActiveJobHooks",
	"GetUserDefinedTransformerById", "GetAccountHooksByAccount", "GetAccountApiKeys", "GetAccountOidcProvider",
	"GetAccountUsers",
}

// The schemas are given for the engines that have databases only, as the query computes them.
func (s *usageStore) ListJobSourcesOfInstance(context.Context, db_queries.DBTX) ([]db_queries.ListJobSourcesOfInstanceRow, error) {
	rows := make([]db_queries.ListJobSourcesOfInstanceRow, 0, len(s.jobs))
	for _, job := range s.jobs {
		row := db_queries.ListJobSourcesOfInstanceRow{
			ID: job.ID, AccountID: job.AccountID, ConnectionOptions: job.ConnectionOptions, JobtypeConfig: job.JobtypeConfig,
		}
		if job.ConnectionOptions.MysqlOptions != nil || job.ConnectionOptions.MongoDbOptions != nil {
			row.Schemas = schemasOf(job.Mappings)
		}
		rows = append(rows, row)
	}
	return rows, s.down["ListJobSourcesOfInstance"]
}

func (s *usageStore) GetJobsByAccount(_ context.Context, _ db_queries.DBTX, accountId pgtype.UUID) ([]db_queries.HusonymApiJob, error) {
	var jobs []db_queries.HusonymApiJob
	for _, job := range s.jobs {
		if job.AccountID == accountId {
			jobs = append(jobs, job)
		}
	}
	return jobs, s.down["GetJobsByAccount"]
}

func (s *usageStore) GetActiveJobHooks(ctx context.Context, db db_queries.DBTX, jobId pgtype.UUID) ([]db_queries.HusonymApiJobHook, error) {
	if err := s.down["GetActiveJobHooks"]; err != nil {
		return nil, err
	}
	return s.gateStore.GetActiveJobHooks(ctx, db, jobId)
}

func (s *usageStore) GetUserDefinedTransformerById(ctx context.Context, db db_queries.DBTX, id pgtype.UUID) (db_queries.HusonymApiTransformer, error) {
	if err := s.down["GetUserDefinedTransformerById"]; err != nil {
		return db_queries.HusonymApiTransformer{}, err
	}
	return s.gateStore.GetUserDefinedTransformerById(ctx, db, id)
}

func (s *usageStore) GetConnectionsByAccount(_ context.Context, _ db_queries.DBTX, accountId pgtype.UUID) ([]db_queries.HusonymApiConnection, error) {
	var connections []db_queries.HusonymApiConnection
	for _, connection := range s.connections {
		if connection.AccountID == accountId {
			connections = append(connections, connection)
		}
	}
	return connections, s.down["GetConnectionsByAccount"]
}

func (s *usageStore) GetAccountHooksByAccount(_ context.Context, _ db_queries.DBTX, accountId pgtype.UUID) ([]db_queries.HusonymApiAccountHook, error) {
	var hooks []db_queries.HusonymApiAccountHook
	for _, hook := range s.accountHooks {
		if hook.AccountID == accountId {
			hooks = append(hooks, hook)
		}
	}
	return hooks, s.down["GetAccountHooksByAccount"]
}

func (s *usageStore) GetAccountApiKeys(_ context.Context, _ db_queries.DBTX, accountId pgtype.UUID) ([]db_queries.HusonymApiAccountApiKey, error) {
	var keys []db_queries.HusonymApiAccountApiKey
	for _, key := range s.apiKeys {
		if key.AccountID == accountId {
			keys = append(keys, key)
		}
	}
	return keys, s.down["GetAccountApiKeys"]
}

func (s *usageStore) GetAccountOidcProvider(_ context.Context, _ db_queries.DBTX, accountId pgtype.UUID) ([]byte, error) {
	if err := s.down["GetAccountOidcProvider"]; err != nil {
		return nil, err
	}
	provider, ok := s.providers[accountId]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return provider, nil
}

func (s *usageStore) GetAccountUsers(_ context.Context, _ db_queries.DBTX, accountId pgtype.UUID) ([]pgtype.UUID, error) {
	return s.members[accountId], s.down["GetAccountUsers"]
}

// rolesHeld is the roles the members hold, whatever the account: a test has one account with
// members.
type rolesHeld map[rbac.User]mgmtv1alpha1.AccountRole

func (r rolesHeld) Roles(users []rbac.User, _ rbac.Account) map[rbac.User]mgmtv1alpha1.AccountRole {
	held := map[rbac.User]mgmtv1alpha1.AccountRole{}
	for _, user := range users {
		if role, ok := r[user]; ok {
			held[user] = role
		}
	}
	return held
}

func newUsageReader(store *usageStore, roles Roles) *UsageReader {
	return NewUsageReader(husonymdb.New(nil, store), roles)
}

func mustUuid(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	parsed, err := husonymdb.ToUuid(id)
	require.NoError(t, err)
	return parsed
}

// addConnection stores a connection of the account and gives its id.
func (s *usageStore) addConnection(t *testing.T, accountId, name string) string {
	t.Helper()
	id := newUuid()
	s.connections = append(s.connections, db_queries.HusonymApiConnection{ID: id, AccountID: mustUuid(t, accountId), Name: name})
	return husonymdb.UUIDString(id)
}

// addJobOf stores a job of the account that uses no licensed feature but what change gives it.
func (s *usageStore) addJobOf(t *testing.T, accountId string, change func(job *mgmtv1alpha1.Job)) string {
	t.Helper()
	job := aJob()
	job.AccountId = accountId
	if change != nil {
		change(job)
	}
	id := s.addJob(t, job)
	if job.GetJobType() != nil {
		jobType, err := json.Marshal(job.GetJobType())
		require.NoError(t, err)
		s.alter(t, id, func(job *db_queries.HusonymApiJob) { job.JobtypeConfig = jobType })
	}
	return id
}

// alter changes the row of a stored job.
func (s *usageStore) alter(t *testing.T, jobId string, change func(job *db_queries.HusonymApiJob)) {
	t.Helper()
	id := mustUuid(t, jobId)
	job := s.jobs[id]
	change(&job)
	s.jobs[id] = job
}

func postgresOn(connectionId string) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Postgres{
		Postgres: &mgmtv1alpha1.PostgresSourceConnectionOptions{ConnectionId: connectionId},
	}})
}

func mysqlOn(connectionId string) *mgmtv1alpha1.JobSource {
	return sourceWith(&mgmtv1alpha1.JobSourceOptions{Config: &mgmtv1alpha1.JobSourceOptions_Mysql{
		Mysql: &mgmtv1alpha1.MysqlSourceConnectionOptions{ConnectionId: connectionId},
	}})
}

func columnsIn(schemas ...string) []*mgmtv1alpha1.JobMapping {
	mappings := make([]*mgmtv1alpha1.JobMapping, 0, len(schemas))
	for _, schema := range schemas {
		mappings = append(mappings, &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: "t", Column: "c",
			Transformer: &mgmtv1alpha1.JobMappingTransformer{Config: passthrough()},
		})
	}
	return mappings
}

// The count is the instance's, the list is the account's: a source of another account adds one
// to the count and nothing to the list.
func Test_UsageReader_CountsTheInstanceAndListsTheAccount(t *testing.T) {
	store := newUsageStore()
	account, other := uuid.NewString(), uuid.NewString()

	postgres := store.addConnection(t, account, "orders")
	mysql := store.addConnection(t, account, "billing")
	store.addConnection(t, account, "read by no job")
	// A connection that was deleted since: the job still names it.
	gone := uuid.NewString()
	// The connection of another account, which the account's job has no business naming.
	foreign := store.addConnection(t, other, "theirs")

	store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(postgres) })
	store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(postgres) })
	store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) {
		job.Source = mysqlOn(mysql)
		job.Mappings = columnsIn("sales", "ledger", "sales")
	})
	store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(gone) })
	store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(foreign) })
	store.addJobOf(t, other, func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(foreign) })
	store.addJobOf(t, other, func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(uuid.NewString()) })

	usage, err := newUsageReader(store, rolesHeld{}).Of(t.Context(), account)
	require.NoError(t, err)

	require.Equal(t, 7, usage.SourcesInInstance, "five of the account, two of the other")
	want := []AccountSource{
		{ConnectionId: postgres, ConnectionName: "orders"},
		{ConnectionId: mysql, ConnectionName: "billing", Database: "ledger"},
		{ConnectionId: mysql, ConnectionName: "billing", Database: "sales"},
		{ConnectionId: gone},
		// Not named: the name of a connection is its account's.
		{ConnectionId: foreign},
	}
	require.ElementsMatch(t, want, usage.SourcesInAccount)

	// In the order SourcesOf gives them: by connection, then by database.
	listed := make([]Source, 0, len(usage.SourcesInAccount))
	for _, source := range usage.SourcesInAccount {
		listed = append(listed, Source{AccountId: account, ConnectionId: source.ConnectionId, Database: source.Database})
	}
	rows, err := store.ListJobSourcesOfInstance(t.Context(), nil)
	require.NoError(t, err)
	var ordered []Source
	for _, source := range SourcesOf(rows) {
		if source.AccountId == account {
			ordered = append(ordered, source)
		}
	}
	require.Equal(t, ordered, listed)
}

func Test_UsageReader_AnAccountWithoutJobs(t *testing.T) {
	store := newUsageStore()
	store.addJobOf(t, uuid.NewString(), func(job *mgmtv1alpha1.Job) { job.Source = postgresOn(uuid.NewString()) })

	usage, err := newUsageReader(store, rolesHeld{}).Of(t.Context(), uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, 1, usage.SourcesInInstance)
	require.Empty(t, usage.SourcesInAccount)
	require.Empty(t, usage.FeaturesInUse)
}

func Test_UsageReader_FeaturesInUse(t *testing.T) {
	where := "id > 10"
	scheduled := func(cron pgtype.Text) func(t *testing.T, store *usageStore, account string) Roles {
		return func(t *testing.T, store *usageStore, account string) Roles {
			id := store.addJobOf(t, account, nil)
			store.alter(t, id, func(job *db_queries.HusonymApiJob) { job.CronSchedule = cron })
			return nil
		}
	}
	member := func(t *testing.T, store *usageStore, account string) rbac.User {
		id := newUuid()
		accountUuid := mustUuid(t, account)
		store.members[accountUuid] = append(store.members[accountUuid], id)
		return rbac.NewPgUser(id)
	}

	cases := []struct {
		name string
		// arrange gives the account what the case is about, and the roles its members hold if
		// it has members.
		arrange func(t *testing.T, store *usageStore, account string) Roles
		want    []license.Feature
	}{
		{
			name: "a job that uses no feature",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.addJobOf(t, account, nil)
				return nil
			},
		},
		{
			name: "a job with an enabled hook",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.addHook(store.addJobOf(t, account, nil), true)
				return nil
			},
			want: []license.Feature{license.FeatureJobHooks},
		},
		{
			name: "a job whose only hook is disabled",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.addHook(store.addJobOf(t, account, nil), false)
				return nil
			},
		},
		{
			name: "what several jobs use, each feature once, in the order of the features",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Source = postgresWhere(&where) })
				store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) {
					job.Source = postgresWhere(&where)
					job.Mappings = mappingWith(transformJavascript())
				})
				store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Mappings = mappingWith(piiText()) })
				store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) {
					job.JobType = &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
						PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
					}}
				})
				return nil
			},
			want: []license.Feature{
				license.FeaturePiiText, license.FeaturePiiDetection, license.FeatureCustomTransformers, license.FeatureSubsetting,
			},
		},
		{
			name: "a user-defined transformer of the account that stores a PII text",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				id := store.addTransformer(t, account, piiText())
				store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Mappings = mappingWith(userDefined(id)) })
				return nil
			},
			want: []license.Feature{license.FeaturePiiText, license.FeatureCustomTransformers},
		},
		{
			name: "a user-defined transformer that was deleted is still a custom transformer, and no error",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Mappings = mappingWith(userDefined(uuid.NewString())) })
				return nil
			},
			want: []license.Feature{license.FeatureCustomTransformers},
		},
		{
			name:    "a job with a schedule of its own",
			arrange: scheduled(pgtype.Text{String: "0 3 * * *", Valid: true}),
			want:    []license.Feature{license.FeatureScheduling},
		},
		{name: "a job with the schedule of an unscheduled job", arrange: scheduled(pgtype.Text{String: job_util.UnscheduledCron, Valid: true})},
		{name: "a job with an empty schedule", arrange: scheduled(pgtype.Text{String: "", Valid: true})},
		{name: "a job with no schedule", arrange: scheduled(pgtype.Text{})},
		{
			name: "an enabled account hook",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.accountHooks = append(store.accountHooks,
					db_queries.HusonymApiAccountHook{ID: newUuid(), AccountID: mustUuid(t, account), Enabled: false},
					db_queries.HusonymApiAccountHook{ID: newUuid(), AccountID: mustUuid(t, account), Enabled: true},
				)
				return nil
			},
			want: []license.Feature{license.FeatureAccountHooks},
		},
		{
			name: "an account hook that is disabled",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.accountHooks = append(store.accountHooks,
					db_queries.HusonymApiAccountHook{ID: newUuid(), AccountID: mustUuid(t, account), Enabled: false},
				)
				return nil
			},
		},
		{
			name: "an account API key",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.apiKeys = append(store.apiKeys, db_queries.HusonymApiAccountApiKey{ID: newUuid(), AccountID: mustUuid(t, account)})
				return nil
			},
			want: []license.Feature{license.FeatureApiKeys},
		},
		{
			name: "a declared identity provider",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				store.providers[mustUuid(t, account)] = []byte(`{"issuer":"https://issuer.example"}`)
				return nil
			},
			want: []license.Feature{license.FeatureSso},
		},
		{
			name: "members who are all administrators, or hold no role",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				admin := member(t, store, account)
				member(t, store, account)
				return rolesHeld{admin: mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN}
			},
		},
		{
			name: "a member with a role other than administrator",
			arrange: func(t *testing.T, store *usageStore, account string) Roles {
				admin, viewer := member(t, store, account), member(t, store, account)
				return rolesHeld{
					admin:  mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_ADMIN,
					viewer: mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER,
				}
			},
			want: []license.Feature{license.FeatureRbac},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newUsageStore()
			account, other := uuid.NewString(), uuid.NewString()
			roles := tc.arrange(t, store, account)
			if roles == nil {
				roles = rolesHeld{}
			}

			usage, err := newUsageReader(store, roles).Of(t.Context(), account)
			require.NoError(t, err)
			require.Equal(t, tc.want, usage.FeaturesInUse)

			// What one account uses says nothing about another.
			usage, err = newUsageReader(store, rolesHeld{}).Of(t.Context(), other)
			require.NoError(t, err)
			require.Empty(t, usage.FeaturesInUse)

			// What the account uses by itself is the part that no job tells, and it is told
			// without reading a job.
			var byItself []license.Feature
			for _, feature := range tc.want {
				switch feature {
				case license.FeatureAccountHooks, license.FeatureApiKeys, license.FeatureSso, license.FeatureRbac:
					byItself = append(byItself, feature)
				}
			}
			store.down["GetJobsByAccount"] = errors.New("the jobs are not read")
			store.down["ListJobSourcesOfInstance"] = errors.New("the sources are not read")
			own, err := newUsageReader(store, roles).AccountFeatures(t.Context(), account)
			require.NoError(t, err)
			require.Equal(t, byItself, own)
		})
	}
}

// One job that cannot be read does not hide what the others use, nor its own schedule, which
// is read from its row.
func Test_UsageReader_SkipsAJobThatCannotBeRead(t *testing.T) {
	store := newUsageStore()
	account := uuid.NewString()
	where := "id > 10"
	store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Source = postgresWhere(&where) })
	unreadable := store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) { job.Mappings = mappingWith(transformJavascript()) })
	store.alter(t, unreadable, func(job *db_queries.HusonymApiJob) {
		job.JobtypeConfig = []byte("{not a job type")
		job.CronSchedule = pgtype.Text{String: "0 3 * * *", Valid: true}
	})

	usage, err := newUsageReader(store, rolesHeld{}).Of(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, []license.Feature{license.FeatureSubsetting, license.FeatureScheduling}, usage.FeaturesInUse)
}

// A question the database does not answer is an error, whichever it is: a usage is never
// guessed, and an identity provider that could not be read is not one the account lacks.
func Test_UsageReader_ADatabaseThatDoesNotAnswer(t *testing.T) {
	for _, query := range usageQueries {
		t.Run(query, func(t *testing.T) {
			store := newUsageStore()
			account := uuid.NewString()
			transformer := store.addTransformer(t, account, transformJavascript())
			store.addHook(store.addJobOf(t, account, func(job *mgmtv1alpha1.Job) {
				job.Mappings = mappingWith(userDefined(transformer))
			}), true)
			down := errors.New("the database is down")
			store.down[query] = down

			_, err := newUsageReader(store, rolesHeld{}).Of(t.Context(), account)
			require.ErrorIs(t, err, down)
		})
	}
}

func Test_UsageReader_AnAccountIdThatIsNone(t *testing.T) {
	_, err := newUsageReader(newUsageStore(), rolesHeld{}).Of(t.Context(), "not-an-id")
	require.Error(t, err)
}
