package v1alpha1_usageservice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"buf.build/go/protovalidate"
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
	"github.com/fishtre-compagnie/husonym/internal/telemetry"
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
	rowsRead, rowsDiscarded, retries, tablesUncounted int64, sourceVersionMajor string,
	told usagestore.RunError,
) error {
	if !f.running[runId] {
		return nil
	}
	delete(f.running, runId)
	f.closed = append(f.closed, runId)
	f.closure = usagestore.RunEnd{
		RunId: runId, Status: status, EndedAt: endedAt,
		RowsRead: rowsRead, RowsDiscarded: rowsDiscarded, Retries: retries,
		TablesUncounted: tablesUncounted, SourceVersionMajor: sourceVersionMajor,
		Error: told,
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
			nil,
			nil,
			nil,
			time.Now,
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
		TablesUncounted: 4, SourceVersionMajor: "16",
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
		TablesUncounted: 4, SourceVersionMajor: "16",
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

// The version of the source is one or two short numbers, or nothing: whatever else a worker
// would send is refused before the service is asked.
func TestRecordRunEndedRequestValidatesWhatTheWorkerAdds(t *testing.T) {
	validator, err := protovalidate.New()
	require.NoError(t, err)

	for _, version := range []string{"", "16", "8.0", "10.11", "999.999"} {
		t.Run("the version "+version+" is accepted", func(t *testing.T) {
			req := finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED).Msg
			req.SourceVersionMajor = version
			require.NoError(t, validator.Validate(req))
		})
	}
	for _, version := range []string{"16; drop", "8.0.36", "8.", ".0", "1000", "sixteen", "16\n", " 16"} {
		t.Run("the version "+version+" is refused", func(t *testing.T) {
			req := finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED).Msg
			req.SourceVersionMajor = version
			require.Error(t, validator.Validate(req))
		})
	}
	t.Run("a negative number of tables is refused", func(t *testing.T) {
		req := finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_COMPLETED).Msg
		req.TablesUncounted = -1
		require.Error(t, validator.Validate(req))
	})
}

// goneJob makes the database hold no job for the test, and the store a running row for its run.
func (f *fixture) goneJob(t *testing.T) {
	t.Helper()
	f.store.running = map[string]bool{"run-1": true}
	jobUuid, err := husonymdb.ToUuid(aJobId)
	require.NoError(t, err)
	f.querier.On("GetJobById", mock.Anything, mock.Anything, jobUuid).
		Return(db_queries.HusonymApiJob{}, pgx.ErrNoRows)
}

// The category and the step the worker tells reach the store under the names the table holds,
// whether the row is made or only closed.
func TestRecordRunEndedCarriesTheErrorOfTheRun(t *testing.T) {
	failed := func() *connect.Request[mgmtv1alpha1.RecordRunEndedRequest] {
		req := finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED)
		req.Msg.ErrorCategory = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED
		req.Msg.ErrorStep = mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_TABLE_SYNC
		return req
	}
	want := usagestore.RunError{Category: "constraint_violated", Step: "table_sync"}

	t.Run("for a job that is there", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		f.storesJob(t, syncOptions(), nil)
		_, err := f.svc.RecordRunEnded(context.Background(), failed())
		require.NoError(t, err)
		require.Len(t, f.store.ended, 1)
		require.Equal(t, want, f.store.ended[0].Error)
	})

	t.Run("for a job that is gone", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		f.goneJob(t)
		_, err := f.svc.RecordRunEnded(context.Background(), failed())
		require.NoError(t, err)
		require.Equal(t, []string{"run-1"}, f.store.closed)
		require.Equal(t, want, f.store.closure.Error)
	})
}

// A worker newer than the service may tell a category or a step the service does not know, and an
// older one tells none: neither is refused, for a refused report would lose the end of the run.
func TestRecordRunEndedKeepsWhatItDoesNotKnowAsOther(t *testing.T) {
	unknown := func() *connect.Request[mgmtv1alpha1.RecordRunEndedRequest] {
		req := finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED)
		req.Msg.ErrorCategory = mgmtv1alpha1.RunErrorCategory(42)
		req.Msg.ErrorStep = mgmtv1alpha1.RunErrorStep(99)
		return req
	}
	other := usagestore.RunError{Category: "other", Step: "other"}

	t.Run("numbers the service does not know are other", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		f.storesJob(t, syncOptions(), nil)
		_, err := f.svc.RecordRunEnded(context.Background(), unknown())
		require.NoError(t, err)
		require.Len(t, f.store.ended, 1)
		require.Equal(t, other, f.store.ended[0].Error)
	})

	t.Run("the same for a job that is gone", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		f.goneJob(t)
		_, err := f.svc.RecordRunEnded(context.Background(), unknown())
		require.NoError(t, err)
		require.Equal(t, other, f.store.closure.Error)
	})

	t.Run("a category told alone leaves the step untold", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		f.storesJob(t, syncOptions(), nil)
		req := finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED)
		req.Msg.ErrorCategory = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
		_, err := f.svc.RecordRunEnded(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, usagestore.RunError{Category: "timeout"}, f.store.ended[0].Error)
	})

	t.Run("nothing told is nothing kept", func(t *testing.T) {
		f := newFixture(t, userdata.WorkerOnly{})
		f.storesJob(t, syncOptions(), nil)
		_, err := f.svc.RecordRunEnded(context.Background(), finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED))
		require.NoError(t, err)
		require.Equal(t, usagestore.RunError{}, f.store.ended[0].Error)
	})

	t.Run("the message is valid with them, and without", func(t *testing.T) {
		validator, err := protovalidate.New()
		require.NoError(t, err)
		require.NoError(t, validator.Validate(unknown().Msg))
		require.NoError(t, validator.Validate(finished(mgmtv1alpha1.RunOutcome_RUN_OUTCOME_FAILED).Msg))
	})
}

// Every category and every step of the proto has a name the table allows, and none becomes other
// but other itself.
func TestToldErrorNamesEveryCategoryAndStepOfTheProto(t *testing.T) {
	for number := range mgmtv1alpha1.RunErrorCategory_name {
		category := mgmtv1alpha1.RunErrorCategory(number)
		told := toldError(&mgmtv1alpha1.RecordRunEndedRequest{ErrorCategory: category})
		switch category {
		case mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED:
			require.Empty(t, told.Category)
		case mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER:
			require.Equal(t, usagestore.ErrorCategoryOther, told.Category)
		default:
			require.NotEqual(t, usagestore.ErrorCategoryOther, told.Category, category)
			require.Contains(t, telemetry.ErrorCategories, string(told.Category))
		}
	}
	for number := range mgmtv1alpha1.RunErrorStep_name {
		step := mgmtv1alpha1.RunErrorStep(number)
		told := toldError(&mgmtv1alpha1.RecordRunEndedRequest{ErrorStep: step})
		switch step {
		case mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_UNSPECIFIED:
			require.Empty(t, told.Step)
		case mgmtv1alpha1.RunErrorStep_RUN_ERROR_STEP_OTHER:
			require.Equal(t, usagestore.ErrorStepOther, told.Step)
		default:
			require.NotEqual(t, usagestore.ErrorStepOther, told.Step, step)
			require.Contains(t, telemetry.ErrorSteps, string(told.Step))
		}
	}
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
		TablesUncounted: 4, SourceVersionMajor: "16",
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

// What each job is, is decided by usagestore.KindOfJob and tested there: here, that the service
// asks it rather than assume a synchronization.
func TestRecordRunStartedGivesTheKindOfTheJob(t *testing.T) {
	f := newFixture(t, userdata.WorkerOnly{})
	f.storesJob(t, &pg_models.JobSourceOptions{GenerateOptions: &pg_models.GenerateSourceOptions{}}, nil)

	_, err := f.svc.RecordRunStarted(context.Background(), started())

	require.NoError(t, err)
	require.Len(t, f.store.started, 1)
	require.Equal(t, usagestore.JobKindGenerate, f.store.started[0].Kind)
}
