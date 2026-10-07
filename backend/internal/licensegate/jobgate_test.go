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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// gateStore is the database of the gate, in memory: the queries the gate uses, and nothing
// else. Any other query panics.
type gateStore struct {
	db_queries.Querier

	jobs         map[pgtype.UUID]db_queries.HusonymApiJob
	hooks        map[pgtype.UUID][]db_queries.HusonymApiJobHook // by job
	transformers map[pgtype.UUID]db_queries.HusonymApiTransformer
	// fails is what the database answers every query, when it is down.
	fails error
}

func newGateStore() *gateStore {
	return &gateStore{
		jobs:         map[pgtype.UUID]db_queries.HusonymApiJob{},
		hooks:        map[pgtype.UUID][]db_queries.HusonymApiJobHook{},
		transformers: map[pgtype.UUID]db_queries.HusonymApiTransformer{},
	}
}

func (s *gateStore) GetJobById(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) (db_queries.HusonymApiJob, error) {
	if s.fails != nil {
		return db_queries.HusonymApiJob{}, s.fails
	}
	job, ok := s.jobs[id]
	if !ok {
		return db_queries.HusonymApiJob{}, pgx.ErrNoRows
	}
	return job, nil
}

func (s *gateStore) GetJobConnectionDestinations(
	context.Context, db_queries.DBTX, pgtype.UUID,
) ([]db_queries.HusonymApiJobDestinationConnectionAssociation, error) {
	return nil, s.fails
}

// The enabled hooks only, as the query selects them.
func (s *gateStore) GetActiveJobHooks(_ context.Context, _ db_queries.DBTX, jobID pgtype.UUID) ([]db_queries.HusonymApiJobHook, error) {
	if s.fails != nil {
		return nil, s.fails
	}
	var active []db_queries.HusonymApiJobHook
	for _, hook := range s.hooks[jobID] {
		if hook.Enabled {
			active = append(active, hook)
		}
	}
	return active, nil
}

func (s *gateStore) GetUserDefinedTransformerById(_ context.Context, _ db_queries.DBTX, id pgtype.UUID) (db_queries.HusonymApiTransformer, error) {
	if s.fails != nil {
		return db_queries.HusonymApiTransformer{}, s.fails
	}
	transformer, ok := s.transformers[id]
	if !ok {
		return db_queries.HusonymApiTransformer{}, pgx.ErrNoRows
	}
	return transformer, nil
}

func newUuid() pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.New(), Valid: true}
}

func (s *gateStore) addHook(jobId string, enabled bool) {
	id, _ := husonymdb.ToUuid(jobId)
	s.hooks[id] = append(s.hooks[id], db_queries.HusonymApiJobHook{ID: newUuid(), JobID: id, Enabled: enabled})
}

// addTransformer stores a user-defined transformer of the account and gives its id.
func (s *gateStore) addTransformer(t *testing.T, accountId string, config *mgmtv1alpha1.TransformerConfig) string {
	t.Helper()
	account, err := husonymdb.ToUuid(accountId)
	require.NoError(t, err)
	stored := &pg_models.TransformerConfig{}
	require.NoError(t, stored.FromTransformerConfigDto(config))
	id := newUuid()
	s.transformers[id] = db_queries.HusonymApiTransformer{ID: id, AccountID: account, TransformerConfig: stored}
	return husonymdb.UUIDString(id)
}

// addJob stores the job as the job service would, and gives its id.
func (s *gateStore) addJob(t *testing.T, job *mgmtv1alpha1.Job) string {
	t.Helper()
	account, err := husonymdb.ToUuid(job.GetAccountId())
	require.NoError(t, err)
	options := &pg_models.JobSourceOptions{}
	require.NoError(t, options.FromDto(job.GetSource().GetOptions()))
	mappings := make([]*pg_models.JobMapping, 0, len(job.GetMappings()))
	for _, mapping := range job.GetMappings() {
		stored := &pg_models.JobMapping{}
		require.NoError(t, stored.FromDto(mapping))
		mappings = append(mappings, stored)
	}
	id := newUuid()
	s.jobs[id] = db_queries.HusonymApiJob{ID: id, AccountID: account, ConnectionOptions: options, Mappings: mappings}
	return husonymdb.UUIDString(id)
}

func newGate(store *gateStore, lic license.EEInterface) *JobGate {
	return NewJobGate(husonymdb.New(nil, store), lic)
}

// aJob is a job of a new account that uses no licensed feature.
func aJob() *mgmtv1alpha1.Job {
	return &mgmtv1alpha1.Job{
		Id:        uuid.NewString(),
		AccountId: uuid.NewString(),
		Source:    postgresWhere(nil),
		Mappings:  mappingWith(passthrough()),
	}
}

func licenseWith(features ...license.Feature) *testutil.FakeEELicense {
	return testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(features...))
}

func requireRefusal(t *testing.T, err error, features string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err), "%v", err)
	var refusal *connect.Error
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "this job uses features the license does not include: "+features, refusal.Message())
}

func Test_JobGate_Check_NothingMissing(t *testing.T) {
	t.Run("a job that uses no feature, under a license that includes none", func(t *testing.T) {
		require.NoError(t, newGate(newGateStore(), licenseWith()).Check(context.Background(), aJob()))
	})

	t.Run("a job whose every feature the license includes", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		job.Source = postgresWhere(ptr("id > 10"))
		job.Mappings = mappingWith(transformJavascript())
		store.addHook(job.GetId(), true)

		gate := newGate(store, licenseWith(
			license.FeatureSubsetting, license.FeatureCustomTransformers, license.FeatureJobHooks,
		))
		require.NoError(t, gate.Check(context.Background(), job))
	})
}

func Test_JobGate_Check_RefusesWhatIsMissing(t *testing.T) {
	job := aJob()
	job.Source = postgresWhere(ptr("id > 10"))
	job.Mappings = mappingWith(transformJavascript())

	t.Run("one feature", func(t *testing.T) {
		gate := newGate(newGateStore(), licenseWith(license.FeatureCustomTransformers))
		requireRefusal(t, gate.Check(context.Background(), job), "subsetting")
	})

	t.Run("every one of them is named", func(t *testing.T) {
		gate := newGate(newGateStore(), licenseWith())
		requireRefusal(t, gate.Check(context.Background(), job), "custom_transformers, subsetting")
	})
}

func Test_JobGate_Check_Hooks(t *testing.T) {
	t.Run("an enabled hook uses the feature", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		store.addHook(job.GetId(), false)
		store.addHook(job.GetId(), true)

		requireRefusal(t, newGate(store, licenseWith()).Check(context.Background(), job), "job_hooks")
	})

	t.Run("disabled hooks alone do not", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		store.addHook(job.GetId(), false)
		store.addHook(job.GetId(), false)

		require.NoError(t, newGate(store, licenseWith()).Check(context.Background(), job))
	})

	t.Run("the hooks of another job do not", func(t *testing.T) {
		store := newGateStore()
		store.addHook(uuid.NewString(), true)

		require.NoError(t, newGate(store, licenseWith()).Check(context.Background(), aJob()))
	})
}

func Test_JobGate_Check_UserDefinedTransformers(t *testing.T) {
	t.Run("one that stores a PII text uses pii_text", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		job.Mappings = mappingWith(userDefined(store.addTransformer(t, job.GetAccountId(), piiText())))

		gate := newGate(store, licenseWith(license.FeatureCustomTransformers))
		requireRefusal(t, gate.Check(context.Background(), job), "pii_text")
	})

	t.Run("one that stores something else does not", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		job.Mappings = mappingWith(userDefined(store.addTransformer(t, job.GetAccountId(), passthrough())))

		gate := newGate(store, licenseWith(license.FeatureCustomTransformers))
		require.NoError(t, gate.Check(context.Background(), job))
	})

	t.Run("a deleted one is still a custom transformer, and nothing more", func(t *testing.T) {
		job := aJob()
		job.Mappings = mappingWith(userDefined(uuid.NewString()))

		require.NoError(t, newGate(newGateStore(), licenseWith(license.FeatureCustomTransformers)).
			Check(context.Background(), job))
		requireRefusal(t, newGate(newGateStore(), licenseWith()).Check(context.Background(), job),
			"custom_transformers")
	})

	t.Run("one of another account is not read", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		job.Mappings = mappingWith(userDefined(store.addTransformer(t, uuid.NewString(), piiText())))

		gate := newGate(store, licenseWith(license.FeatureCustomTransformers))
		require.NoError(t, gate.Check(context.Background(), job))
	})
}

// A database that fails is not a refusal: the caller must be able to tell the two apart.
func Test_JobGate_Check_ADatabaseErrorIsNotARefusal(t *testing.T) {
	down := errors.New("the database is down")

	t.Run("reading the hooks", func(t *testing.T) {
		store := newGateStore()
		store.fails = down

		err := newGate(store, licenseWith()).Check(context.Background(), aJob())
		require.ErrorIs(t, err, down)
		require.NotEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("reading a transformer", func(t *testing.T) {
		store := newGateStore()
		job := aJob()
		job.Mappings = mappingWith(userDefined(store.addTransformer(t, job.GetAccountId(), piiText())))
		failing := &transformersDown{gateStore: store, fails: down}

		err := NewJobGate(husonymdb.New(nil, failing), licenseWith()).Check(context.Background(), job)
		require.ErrorIs(t, err, down)
		require.NotEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})
}

// transformersDown is a store whose transformers alone cannot be read.
type transformersDown struct {
	*gateStore
	fails error
}

func (s *transformersDown) GetUserDefinedTransformerById(context.Context, db_queries.DBTX, pgtype.UUID) (db_queries.HusonymApiTransformer, error) {
	return db_queries.HusonymApiTransformer{}, s.fails
}

func Test_JobGate_CheckStored(t *testing.T) {
	subsetting := aJob()
	subsetting.Source = postgresWhere(ptr("id > 10"))

	t.Run("checks the job as it is stored", func(t *testing.T) {
		store := newGateStore()
		jobId := store.addJob(t, subsetting)
		store.addHook(jobId, true)

		err := newGate(store, licenseWith()).CheckStored(context.Background(), subsetting.GetAccountId(), jobId)
		requireRefusal(t, err, "job_hooks, subsetting")

		allowed := newGate(store, licenseWith(license.FeatureJobHooks, license.FeatureSubsetting))
		require.NoError(t, allowed.CheckStored(context.Background(), subsetting.GetAccountId(), jobId))
	})

	t.Run("a job that does not exist is not found", func(t *testing.T) {
		err := newGate(newGateStore(), licenseWith()).
			CheckStored(context.Background(), uuid.NewString(), uuid.NewString())
		require.ErrorIs(t, err, ErrJobNotFound)
	})

	t.Run("a job of another account is not found", func(t *testing.T) {
		store := newGateStore()
		jobId := store.addJob(t, subsetting)

		err := newGate(store, licenseWith()).CheckStored(context.Background(), uuid.NewString(), jobId)
		require.ErrorIs(t, err, ErrJobNotFound)
	})

	t.Run("a database error is neither", func(t *testing.T) {
		store := newGateStore()
		jobId := store.addJob(t, subsetting)
		store.fails = errors.New("the database is down")

		err := newGate(store, licenseWith()).CheckStored(context.Background(), subsetting.GetAccountId(), jobId)
		require.ErrorIs(t, err, store.fails)
		require.NotErrorIs(t, err, ErrJobNotFound)
		require.NotEqual(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})
}
