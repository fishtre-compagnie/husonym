package v1alpha1_jobservice

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/temporal/clientmanager"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errNoExecute = errors.New("test: cannot execute")

// A caller who may create and edit jobs but not run them — which API key scopes make possible
// for the first time — must not get a job to write to its destination another way.
func notAllowedToExecute(t *testing.T) (*Service, *db_queries.MockQuerier) {
	t.Helper()
	svc, querier, _ := notAllowedToExecuteWith(t, nil)
	return svc, querier
}

func notAllowedToExecuteWith(
	t *testing.T,
	temporal *clientmanager.MockInterface,
) (*Service, *db_queries.MockQuerier, *userdata.MockEntityEnforcer) {
	t.Helper()
	enforcer := userdata.NewMockEntityEnforcer(t)
	enforcer.On("EnforceJob", mock.Anything, mock.Anything, rbac.JobAction_Execute).Return(errNoExecute).Maybe()
	enforcer.On("EnforceJob", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).Return(&userdata.User{EntityEnforcer: enforcer}, nil)

	querier := db_queries.NewMockQuerier(t)
	jobId := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	querier.On("GetJobById", mock.Anything, mock.Anything, mock.Anything).
		Return(db_queries.HusonymApiJob{
			ID:                jobId,
			AccountID:         pgtype.UUID{Bytes: uuid.New(), Valid: true},
			ConnectionOptions: &pg_models.JobSourceOptions{PostgresOptions: &pg_models.PostgresSourceOptions{}},
		}, nil).Maybe()
	querier.On("GetJobConnectionDestinations", mock.Anything, mock.Anything, mock.Anything).
		Return([]db_queries.HusonymApiJobDestinationConnectionAssociation{}, nil).Maybe()

	var manager clientmanager.Interface
	if temporal != nil {
		manager = temporal
	}
	svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), querier), manager, nil, nil, nil, users, nil)
	return svc, querier, enforcer
}

func Test_CreateJob_RunningItTakesExecute(t *testing.T) {
	for name, req := range map[string]*mgmtv1alpha1.CreateJobRequest{
		"with a first run":        {AccountId: uuid.NewString(), JobName: "j", InitiateJobRun: true},
		"with an active schedule": {AccountId: uuid.NewString(), JobName: "j", CronSchedule: ptr("* * * * *")},
	} {
		t.Run(name, func(t *testing.T) {
			svc, querier := notAllowedToExecute(t)
			_, err := svc.CreateJob(context.Background(), connect.NewRequest(req))
			require.ErrorIs(t, err, errNoExecute)
			querier.AssertNotCalled(t, "CreateJob", mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func Test_UpdateJobSchedule_TakesExecute(t *testing.T) {
	svc, querier := notAllowedToExecute(t)
	_, err := svc.UpdateJobSchedule(context.Background(), connect.NewRequest(&mgmtv1alpha1.UpdateJobScheduleRequest{
		Id: uuid.NewString(), CronSchedule: ptr("* * * * *"),
	}))
	require.ErrorIs(t, err, errNoExecute)
	querier.AssertNotCalled(t, "UpdateJobSchedule", mock.Anything, mock.Anything, mock.Anything)
}

func Test_PauseJob_ResumingTakesExecute(t *testing.T) {
	svc, _ := notAllowedToExecute(t)
	_, err := svc.PauseJob(context.Background(), connect.NewRequest(&mgmtv1alpha1.PauseJobRequest{
		Id: uuid.NewString(), Pause: false,
	}))
	require.ErrorIs(t, err, errNoExecute)
}

func ptr[T any](v T) *T { return &v }

// What does not make a job run stays open without job:execute: creating a job that waits, and
// pausing — which must always be possible, to stop what runs.
func Test_CreateJob_WithoutARunTakesNoExecute(t *testing.T) {
	svc, _, enforcer := notAllowedToExecuteWith(t, nil)
	// Past the gate the handler needs what this test does not stand up; only whether it asked
	// for job:execute matters here.
	func() {
		defer func() { _ = recover() }()
		_, err := svc.CreateJob(context.Background(), connect.NewRequest(&mgmtv1alpha1.CreateJobRequest{
			AccountId: uuid.NewString(), JobName: "j",
		}))
		require.NotErrorIs(t, err, errNoExecute)
	}()
	enforcer.AssertNotCalled(t, "EnforceJob", mock.Anything, mock.Anything, rbac.JobAction_Execute)
}

func Test_PauseJob_PausingTakesNoExecute(t *testing.T) {
	temporal := clientmanager.NewMockInterface(t)
	temporal.On("PauseSchedule", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	svc, _, enforcer := notAllowedToExecuteWith(t, temporal)

	_, err := svc.PauseJob(context.Background(), connect.NewRequest(&mgmtv1alpha1.PauseJobRequest{
		Id: uuid.NewString(), Pause: true,
	}))
	require.NoError(t, err)
	enforcer.AssertNotCalled(t, "EnforceJob", mock.Anything, mock.Anything, rbac.JobAction_Execute)
}
