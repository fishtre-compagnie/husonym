package licensegate

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// sourceQuery is a query the guard made, and the handle it made it through.
type sourceQuery struct {
	name   string
	handle db_queries.DBTX
}

// sourceStore is the database of the guard, in memory: the stored jobs as the listing gives
// them, and the queries the guard made, in order. Any other query panics.
type sourceStore struct {
	db_queries.Querier

	jobs    []db_queries.ListJobSourcesOfInstanceRow
	queries []sourceQuery
	// lockFails and listFails are what the database answers each of the two queries, when it
	// cannot.
	lockFails, listFails error
}

func (s *sourceStore) LockLicenseSources(_ context.Context, db db_queries.DBTX) error {
	s.queries = append(s.queries, sourceQuery{name: "lock", handle: db})
	return s.lockFails
}

func (s *sourceStore) ListJobSourcesOfInstance(_ context.Context, db db_queries.DBTX) ([]db_queries.ListJobSourcesOfInstanceRow, error) {
	s.queries = append(s.queries, sourceQuery{name: "list", handle: db})
	if s.listFails != nil {
		return nil, s.listFails
	}
	return s.jobs, nil
}

// add stores a job and gives it back with the id it was given.
func (s *sourceStore) add(job db_queries.ListJobSourcesOfInstanceRow) db_queries.ListJobSourcesOfInstanceRow {
	job.ID = newUuid()
	s.jobs = append(s.jobs, job)
	return job
}

func cappedAt(maxSources int) *testutil.FakeEELicense {
	return testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithLimits(&license.Limits{MaxSources: &maxSources}))
}

// guard runs the guard of the candidate under a license capped at maxSources.
func guard(store *sourceStore, maxSources int, candidate *SourceCandidate) error {
	check := NewJobGate(husonymdb.New(nil, store), cappedAt(maxSources)).SourceGuard(candidate)
	return check(context.Background(), &aHandle{})
}

func requireCapRefusal(t *testing.T, err error, message string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	var refusal *connect.Error
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, message, refusal.Message())
}

// newJobOn is a synchronization job being created on the connection.
func newJobOn(account pgtype.UUID, options *pg_models.JobSourceOptions, mappings ...*pg_models.JobMapping) *SourceCandidate {
	return &SourceCandidate{AccountId: account, ConnectionOptions: options, JobtypeConfig: []byte("{}"), Mappings: mappings}
}

// A license without a cap on sources asks nothing of the database: there is no guard to run,
// and no lock for the writes of the instance to queue on.
func Test_SourceGuard_WithoutACapThereIsNoGuard(t *testing.T) {
	maxJobs := 1
	for name, limits := range map[string]*license.Limits{
		"no limits":          nil,
		"limits without cap": {MaxJobs: &maxJobs},
	} {
		t.Run(name, func(t *testing.T) {
			store := &sourceStore{}
			lic := testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithLimits(limits))

			require.Nil(t, NewJobGate(husonymdb.New(&aHandle{}, store), lic).SourceGuard(newJobOn(accountA, postgres("pg"))))
			require.Empty(t, store.queries)
		})
	}
}

// The lock is what makes the count of one write hold until it is written: it is taken before
// the jobs are listed, and both go through the transaction of the write, not the pool.
func Test_SourceGuard_LocksThenListsThroughTheGivenHandle(t *testing.T) {
	pool, tx := &aHandle{}, &aHandle{}
	store := &sourceStore{}
	check := NewJobGate(husonymdb.New(pool, store), cappedAt(1)).SourceGuard(newJobOn(accountA, postgres("pg")))

	require.NoError(t, check(context.Background(), tx))
	require.Len(t, store.queries, 2)
	require.Equal(t, "lock", store.queries[0].name)
	require.Equal(t, "list", store.queries[1].name)
	for _, query := range store.queries {
		require.Same(t, tx, query.handle)
	}
}

func Test_SourceGuard_RefusesASourceBeyondTheCap(t *testing.T) {
	store := &sourceStore{}
	store.add(syncJob(t, accountA, postgres("pg")))

	// The cap is the instance's: the source of another account takes the room.
	err := guard(store, 1, newJobOn(accountB, postgres("pg")))
	requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 2; contact us to raise the limit")

	require.NoError(t, guard(store, 2, newJobOn(accountB, postgres("pg"))))
}

// A cap of zero is a cap: only its absence allows everything.
func Test_SourceGuard_ACapOfZeroRefusesTheFirstSource(t *testing.T) {
	err := guard(&sourceStore{}, 0, newJobOn(accountA, postgres("pg")))
	requireCapRefusal(t, err, "this license allows 0 source(s) and this change would bring the instance to 1; contact us to raise the limit")
}

// A write that adds no source is not refused, even on an instance a newer key left over its cap.
func Test_SourceGuard_AllowsWhatAddsNoSource(t *testing.T) {
	store := &sourceStore{}
	first := store.add(syncJob(t, accountA, postgres("pg")))
	store.add(syncJob(t, accountA, mysql("my"), "shop"))
	store.add(syncJob(t, accountB, postgres("other")))

	t.Run("a new job on a source already read", func(t *testing.T) {
		require.NoError(t, guard(store, 1, newJobOn(accountA, postgres("pg"))))
		require.NoError(t, guard(store, 1, newJobOn(accountA, mysql("my"), columnsOf("shop", "a")...)))
	})

	t.Run("a job that keeps its source", func(t *testing.T) {
		candidate := newJobOn(accountA, postgres("pg"))
		candidate.JobId = first.ID
		require.NoError(t, guard(store, 1, candidate))
	})

	t.Run("a job that gives its source up for one already read", func(t *testing.T) {
		candidate := newJobOn(accountA, mysql("my"), columnsOf("shop", "a")...)
		candidate.JobId = first.ID
		require.NoError(t, guard(store, 1, candidate))
	})

	t.Run("a job that is not a synchronization", func(t *testing.T) {
		detection := newJobOn(accountA, postgres("unread"))
		detection.JobtypeConfig = stored(t, &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
			PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
		}})
		require.NoError(t, guard(store, 1, detection))

		generation := newJobOn(accountA, &pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}})
		require.NoError(t, guard(store, 1, generation))
	})

	t.Run("but not one that adds a source", func(t *testing.T) {
		err := guard(store, 1, newJobOn(accountA, mysql("my"), columnsOf("crm", "a")...))
		requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 4; contact us to raise the limit")
	})
}

// The instance is counted with the candidate in place of the job as it is stored: its only
// reader leaving, a source no longer is one.
func Test_SourceGuard_AJobMovingFromTheSourceItAloneReadsKeepsTheCount(t *testing.T) {
	store := &sourceStore{}
	only := store.add(syncJob(t, accountA, postgres("pg")))
	moving := newJobOn(accountA, postgres("other"))
	moving.JobId = only.ID

	require.NoError(t, guard(store, 1, moving))

	// Over the cap the same move is refused: it gives the instance a source it does not have.
	overTheCap := &sourceStore{jobs: []db_queries.ListJobSourcesOfInstanceRow{only, syncJob(t, accountB, postgres("pg"))}}
	err := guard(overTheCap, 1, moving)
	requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 2; contact us to raise the limit")

	// With another reader of the source it leaves, the move adds one.
	store.add(syncJob(t, accountA, postgres("pg")))
	err = guard(store, 1, moving)
	requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 2; contact us to raise the limit")

	// A job being created has no stored sources to leave out, whatever the jobs listed.
	err = guard(store, 1, newJobOn(accountA, postgres("other")))
	requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 2; contact us to raise the limit")
}

// A MySQL job reads one source per database it maps: the candidate is counted on its mappings.
func Test_SourceGuard_CountsTheSchemasOfTheCandidate(t *testing.T) {
	store := &sourceStore{}
	job := store.add(syncJob(t, accountA, mysql("my"), "shop"))

	sameDatabase := newJobOn(accountA, mysql("my"), append(columnsOf("shop", "a", "b"), columnsOf("", "orphan")...)...)
	sameDatabase.JobId = job.ID
	require.NoError(t, guard(store, 1, sameDatabase))

	anotherDatabase := newJobOn(accountA, mysql("my"), append(columnsOf("shop", "a"), columnsOf("crm", "a", "b")...)...)
	anotherDatabase.JobId = job.ID
	err := guard(store, 1, anotherDatabase)
	requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 2; contact us to raise the limit")
}

// A write that names no job type leaves the job the one it has, and is counted with it.
func Test_SourceGuard_AWriteWithoutAJobTypeKeepsTheStoredOne(t *testing.T) {
	store := &sourceStore{}
	store.add(syncJob(t, accountA, postgres("pg")))
	detection := syncJob(t, accountA, postgres("unread"))
	detection.JobtypeConfig = stored(t, &mgmtv1alpha1.JobTypeConfig{JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{
		PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{},
	}})
	detection = store.add(detection)

	untyped := SourceCandidate{JobId: detection.ID, AccountId: accountA, ConnectionOptions: postgres("unread")}
	require.NoError(t, guard(store, 1, &untyped))

	synchronizing := untyped
	synchronizing.JobtypeConfig = []byte("{}")
	err := guard(store, 1, &synchronizing)
	requireCapRefusal(t, err, "this license allows 1 source(s) and this change would bring the instance to 2; contact us to raise the limit")
}

// The guard fails closed: a write whose sources cannot be counted is not made, and the failure
// is not passed off as a refusal of the license.
func Test_SourceGuard_ADatabaseFailureStopsTheWrite(t *testing.T) {
	down := errors.New("the database is down")
	for name, store := range map[string]*sourceStore{
		"the lock":    {lockFails: down},
		"the listing": {listFails: down},
	} {
		t.Run(name, func(t *testing.T) {
			err := guard(store, 10, newJobOn(accountA, postgres("pg")))
			require.ErrorIs(t, err, down)
			require.NotEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))
		})
	}
}
