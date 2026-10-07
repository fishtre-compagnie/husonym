package v1alpha1_usageservice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/usagestore"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata/userdatatest"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	anAccountId = "d5ef8fc7-4b2e-4f1f-8c9c-2a2a2a2a2a2a"
	aJobId      = "0b6f1d1e-7a43-4c36-9d6b-3f1b6c0f9a11"
)

// fakeStore keeps what the service hands it.
type fakeStore struct {
	started []usagestore.RunStart
	ended   []usagestore.RunEnd
	// running holds the runs the store has a running row for; CloseRun closes only those.
	running map[string]bool
	closed  []string
	closure usagestore.RunEnd
}

func (f *fakeStore) CloseRun(
	_ context.Context, runId string, status usagestore.Status, endedAt time.Time,
	rowsRead, rowsDiscarded, retries int64,
) error {
	if !f.running[runId] {
		return nil
	}
	delete(f.running, runId)
	f.closed = append(f.closed, runId)
	f.closure = usagestore.RunEnd{
		RunId: runId, Status: status, EndedAt: endedAt,
		RowsRead: rowsRead, RowsDiscarded: rowsDiscarded, Retries: retries,
	}
	return nil
}

func (f *fakeStore) RunStarted(_ context.Context, run usagestore.RunStart) error {
	f.started = append(f.started, run)
	return nil
}

func (f *fakeStore) RunEnded(_ context.Context, run usagestore.RunEnd) error {
	f.ended = append(f.ended, run)
	return nil
}

type fixture struct {
	svc     *Service
	querier *db_queries.MockQuerier
	store   *fakeStore
}

func newFixture(t *testing.T, workerOnly userdata.WorkerOnly) *fixture {
	t.Helper()
	querier := db_queries.NewMockQuerier(t)
	store := &fakeStore{}
	users := userdata.NewClient(userdatatest.Members{UserId: uuid.NewString()}, nil, testutil.NewFakeEELicense(testutil.WithIsValid()))
	return &fixture{
		svc: newService(
			&Config{WorkerOnly: workerOnly},
			husonymdb.New(husonymdb.NewMockDBTX(t), querier),
			users,
			store,
		),
		querier: querier,
		store:   store,
	}
}

// storesJob makes the database hold the job of the test.
func (f *fixture) storesJob(t *testing.T, options *pg_models.JobSourceOptions, jobType *mgmtv1alpha1.JobTypeConfig) {
	t.Helper()
	accountUuid, err := husonymdb.ToUuid(anAccountId)
	require.NoError(t, err)
	jobUuid, err := husonymdb.ToUuid(aJobId)
	require.NoError(t, err)
	config, err := json.Marshal(jobType)
	require.NoError(t, err)
	f.querier.On("GetJobById", mock.Anything, mock.Anything, jobUuid).Return(db_queries.HusonymApiJob{
		ID:                jobUuid,
		AccountID:         accountUuid,
		ConnectionOptions: options,
		JobtypeConfig:     config,
	}, nil)
}

func asWorker(ctx context.Context) context.Context {
	return auth_apikey.SetTokenData(ctx, &auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey})
}

func asAccountKey(ctx context.Context) context.Context {
	return auth_apikey.SetTokenData(ctx, &auth_apikey.TokenContextData{ApiKeyType: apikey.AccountApiKey})
}

var (
	began = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	ended = began.Add(time.Minute)
)

func started() *connect.Request[mgmtv1alpha1.RecordRunStartedRequest] {
	return connect.NewRequest(&mgmtv1alpha1.RecordRunStartedRequest{
		JobId: aJobId, RunId: "run-1", StartedAt: timestamppb.New(began),
	})
}

func finished(outcome mgmtv1alpha1.RunOutcome) *connect.Request[mgmtv1alpha1.RecordRunEndedRequest] {
	return connect.NewRequest(&mgmtv1alpha1.RecordRunEndedRequest{
		JobId: aJobId, RunId: "run-1", StartedAt: timestamppb.New(began), EndedAt: timestamppb.New(ended),
		Outcome: outcome, RowsRead: 120, RowsDiscarded: 3, Retries: 2,
	})
}

func syncOptions() *pg_models.JobSourceOptions {
	return &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{ConnectionId: "c"}}
}

// With authentication, what the worker alone calls turns every other caller away, before the
// job is read.
func TestRecordRunIsForTheWorkerAlone(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{IsAuthEnabled: true})

	for name, ctx := range map[string]context.Context{
		"a session":      context.Background(),
		"an account key": asAccountKey(context.Background()),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.RecordRunStarted(ctx, started())
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			_, err = f.svc.RecordRunEnded(ctx, finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		})
	}
	require.Empty(t, f.store.started)
	require.Empty(t, f.store.ended)
}

func TestRecordRunStartedThenEndedCarriesTheAccountAndTheKindOfTheJob(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{IsAuthEnabled: true})
	f.storesJob(t, syncOptions(), nil)
	ctx := asWorker(context.Background())

	_, err := f.svc.RecordRunStarted(ctx, started())
	require.NoError(t, err)
	require.Equal(t, []usagestore.RunStart{{
		RunId: "run-1", AccountId: anAccountId, JobId: aJobId, Kind: usagestore.JobKindSync, StartedAt: began,
	}}, f.store.started)

	_, err = f.svc.RecordRunEnded(ctx, finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED))
	require.NoError(t, err)
	require.Equal(t, []usagestore.RunEnd{{
		RunId: "run-1", AccountId: anAccountId, JobId: aJobId, Kind: usagestore.JobKindSync,
		StartedAt: began, EndedAt: ended, Status: usagestore.StatusFailed,
		RowsRead: 120, RowsDiscarded: 3, Retries: 2,
	}}, f.store.ended)
}

// A start that was never told does not lose the end: the store creates the row.
func TestRecordRunEndedAloneIsKept(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{IsAuthEnabled: true})
	f.storesJob(t, syncOptions(), nil)

	_, err := f.svc.RecordRunEnded(asWorker(context.Background()), finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED))
	require.NoError(t, err)
	require.Empty(t, f.store.started)
	require.Len(t, f.store.ended, 1)
	require.Equal(t, usagestore.StatusCompleted, f.store.ended[0].Status)
}

func TestRecordRunEndedMapsEveryOutcome(t *testing.T) {
	for outcome, status := range map[mgmtv1alpha1.RunOutcome]usagestore.Status{
		mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED: usagestore.StatusCompleted,
		mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED:    usagestore.StatusFailed,
		mgmtv1alpha1.RunOutcome_RUN_OUTCOME_CANCELED:  usagestore.StatusCanceled,
	} {
		t.Run(outcome.String(), func(t *testing.T) {
			f := newFixture(t, userdata.WorkerOnly{})
			f.storesJob(t, syncOptions(), nil)
			_, err := f.svc.RecordRunEnded(context.Background(), finished(outcome))
			require.NoError(t, err)
			require.Equal(t, status, f.store.ended[0].Status)
		})
	}

	t.Run("an outcome that says nothing is refused", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		_, err := f.svc.RecordRunEnded(context.Background(), finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_UNSPECIFIED))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.Empty(t, f.store.ended)
	})
}

// A job deleted since the run began leaves nothing to count, and is no error for the worker.
func TestRecordRunOfAnUnknownJobKeepsNothing(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{})
	jobUuid, err := husonymdb.ToUuid(aJobId)
	require.NoError(t, err)
	f.querier.On("GetJobById", mock.Anything, mock.Anything, jobUuid).
		Return(db_queries.HusonymApiJob{}, pgx.ErrNoRows)

	ctx := context.Background()
	resp, err := f.svc.RecordRunStarted(ctx, started())
	require.NoError(t, err)
	require.NotNil(t, resp.Msg)
	resp2, err := f.svc.RecordRunEnded(ctx, finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED))
	require.NoError(t, err)
	require.NotNil(t, resp2.Msg)
	require.Empty(t, f.store.started)
	require.Empty(t, f.store.ended)
}

// The end of a run whose job is gone still closes the row that exists for the run.
func TestRecordRunEndedOfAGoneJobClosesTheRowThatExists(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{})
	f.store.running = map[string]bool{"run-1": true}
	jobUuid, err := husonymdb.ToUuid(aJobId)
	require.NoError(t, err)
	f.querier.On("GetJobById", mock.Anything, mock.Anything, jobUuid).
		Return(db_queries.HusonymApiJob{}, pgx.ErrNoRows)

	_, err = f.svc.RecordRunEnded(context.Background(), finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED))

	require.NoError(t, err)
	require.Equal(t, []string{"run-1"}, f.store.closed)
	require.Equal(t, usagestore.RunEnd{
		RunId: "run-1", Status: usagestore.StatusFailed, EndedAt: ended,
		RowsRead: 120, RowsDiscarded: 3, Retries: 2,
	}, f.store.closure)
	require.Empty(t, f.store.ended, "no row is created for a job that is gone")
}

// With no row for the run, the end of a run whose job is gone keeps nothing and is no error.
func TestRecordRunEndedOfAGoneJobWithoutARowKeepsNothing(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{})
	jobUuid, err := husonymdb.ToUuid(aJobId)
	require.NoError(t, err)
	f.querier.On("GetJobById", mock.Anything, mock.Anything, jobUuid).
		Return(db_queries.HusonymApiJob{}, pgx.ErrNoRows)

	resp, err := f.svc.RecordRunEnded(context.Background(), finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED))

	require.NoError(t, err)
	require.NotNil(t, resp.Msg)
	require.Empty(t, f.store.closed)
	require.Empty(t, f.store.ended)
}

func TestRecordRunStartedGivesTheKindOfEachJob(t *testing.T) {
	piiDetect := &mgmtv1alpha1.JobTypeConfig{
		JobType: &mgmtv1alpha1.JobTypeConfig_PiiDetect{PiiDetect: &mgmtv1alpha1.JobTypeConfig_JobTypePiiDetect{}},
	}
	sync := &mgmtv1alpha1.JobTypeConfig{
		JobType: &mgmtv1alpha1.JobTypeConfig_Sync{Sync: &mgmtv1alpha1.JobTypeConfig_JobTypeSync{}},
	}
	for name, tc := range map[string]struct {
		options *pg_models.JobSourceOptions
		jobType *mgmtv1alpha1.JobTypeConfig
		want    usagestore.JobKind
	}{
		"a synchronization":       {syncOptions(), sync, usagestore.JobKindSync},
		"a job with no type":      {syncOptions(), nil, usagestore.JobKindSync},
		"a job with no source":    {nil, nil, usagestore.JobKindSync},
		"a generation":            {&pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}}, sync, usagestore.JobKindGenerate},
		"a generation by a model": {&pg_models.JobSourceOptions{AiGenerateOptions: &pg_models.AiGenerateSourceOptions{}}, sync, usagestore.JobKindAiGenerate},
		"a detection of PII":      {syncOptions(), piiDetect, usagestore.JobKindPiiDetect},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, userdata.WorkerOnly{})
			f.storesJob(t, tc.options, tc.jobType)
			_, err := f.svc.RecordRunStarted(context.Background(), started())
			require.NoError(t, err)
			require.Len(t, f.store.started, 1)
			require.Equal(t, tc.want, f.store.started[0].Kind)
		})
	}
}
