package accounthooks

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	ee_slack "github.com/fishtre-compagnie/husonym/internal/ee/slack"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// The message of an event is the worker's to send: a person allowed to edit the account, from
// a session, does not post one in its name. The Slack mock holds no expectation: a message
// sent fails the test.
func Test_SendSlackMessage_IsTheWorkers(t *testing.T) {
	enforcer := userdata.NewMockEntityEnforcer(t)
	enforcer.On("EnforceAccount", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).Return(&userdata.User{EntityEnforcer: enforcer}, nil)

	querier := db_queries.NewMockQuerier(t)
	querier.On("GetAccountHookById", mock.Anything, mock.Anything, mock.Anything).Return(db_queries.HusonymApiAccountHook{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		AccountID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Enabled:   true,
		Config:    []byte(`{"slack":{"channelId":"C123"}}`),
	}, nil)

	svc := New(husonymdb.New(husonymdb.NewMockDBTX(t), querier), users,
		WithSlackClient(ee_slack.NewMockInterface(t)),
		WithWorkerOnly(userdata.WorkerOnly{IsAuthEnabled: true}),
	)
	_, err := svc.SendSlackMessage(context.Background(), &mgmtv1alpha1.SendSlackMessageRequest{
		AccountHookId: uuid.NewString(),
	})
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}
