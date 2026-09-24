package jobhooks

import (
	"context"
	"errors"
	"testing"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errNoExecute = errors.New("test: cannot execute")

// A hook is SQL the next run executes: a caller who may create and edit jobs but not run them
// must not plant one.
func Test_CreateJobHook_TakesExecute(t *testing.T) {
	enforcer := userdata.NewMockEntityEnforcer(t)
	enforcer.On("EnforceJob", mock.Anything, mock.Anything, rbac.JobAction_Execute).Return(errNoExecute)
	enforcer.On("EnforceJob", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).Return(&userdata.User{EntityEnforcer: enforcer}, nil)

	querier := db_queries.NewMockQuerier(t)
	querier.On("GetAccountIdFromJobId", mock.Anything, mock.Anything, mock.Anything).
		Return(pgtype.UUID{Bytes: uuid.New(), Valid: true}, nil)

	svc := New(husonymdb.New(husonymdb.NewMockDBTX(t), querier), users, WithEnabled())
	_, err := svc.CreateJobHook(context.Background(), &mgmtv1alpha1.CreateJobHookRequest{
		JobId: uuid.NewString(),
		Hook:  &mgmtv1alpha1.NewJobHook{Name: "h", Enabled: true},
	})
	require.ErrorIs(t, err, errNoExecute)
	querier.AssertNotCalled(t, "CreateJobHook", mock.Anything, mock.Anything, mock.Anything)
}
