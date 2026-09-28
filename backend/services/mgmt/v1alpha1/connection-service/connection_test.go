package v1alpha1_connectionservice

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	sql_manager "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errNoSensitive = errors.New("test: cannot view sensitive")

// Checking a connection logs in with what it stores. Someone who may see a connection but
// not its secrets reads it masked, and would have the check log in with the mask — a failed
// login on the customer's database, reported as a connection that does not work. They are
// refused before anything is opened.
func Test_CheckConnectionConfigById_TakesViewSensitive(t *testing.T) {
	enforcer := userdata.NewMockEntityEnforcer(t)
	enforcer.On("EnforceConnection", mock.Anything, mock.Anything, rbac.ConnectionAction_View).Return(nil)
	enforcer.On("Connection", mock.Anything, mock.Anything, rbac.ConnectionAction_ViewSensitive).Return(false, nil)
	enforcer.On("EnforceConnection", mock.Anything, mock.Anything, rbac.ConnectionAction_ViewSensitive).
		Return(errNoSensitive)
	users := userdata.NewMockInterface(t)
	users.On("GetUser", mock.Anything).Return(&userdata.User{EntityEnforcer: enforcer}, nil)

	querier := db_queries.NewMockQuerier(t)
	querier.On("GetConnectionById", mock.Anything, mock.Anything, mock.Anything).Return(db_queries.HusonymApiConnection{
		ID:        pgtype.UUID{Bytes: uuid.New(), Valid: true},
		AccountID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Name:      "production",
		ConnectionConfig: &pg_models.ConnectionConfig{PgConfig: &pg_models.PostgresConnectionConfig{
			Connection: &pg_models.PostgresConnection{Host: "db.internal", User: "reader", Pass: "secret"},
		}},
	}, nil)
	sqlmanager := sql_manager.NewMockSqlManagerClient(t)

	svc := New(&Config{}, husonymdb.New(husonymdb.NewMockDBTX(t), querier), users, nil, nil, sqlmanager, nil)
	_, err := svc.CheckConnectionConfigById(context.Background(), connect.NewRequest(
		&mgmtv1alpha1.CheckConnectionConfigByIdRequest{Id: uuid.NewString()},
	))

	require.ErrorIs(t, err, errNoSensitive)
	sqlmanager.AssertNotCalled(t, "NewSqlConnection", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// A role is checked on MySQL and PostgreSQL only. On any other kind, a scope is refused rather
// than answered with no finding, which would say nothing is missing when nothing was checked.
func Test_checkConnection_ScopeOnAnUncheckedKind(t *testing.T) {
	scope := &mgmtv1alpha1.ConnectionCheckScope{Role: mgmtv1alpha1.ConnectionRole_CONNECTION_ROLE_DESTINATION}
	for name, config := range map[string]*mgmtv1alpha1.ConnectionConfig{
		"SQL Server": {Config: &mgmtv1alpha1.ConnectionConfig_MssqlConfig{MssqlConfig: &mgmtv1alpha1.MssqlConnectionConfig{}}},
		"MongoDB":    {Config: &mgmtv1alpha1.ConnectionConfig_MongoConfig{MongoConfig: &mgmtv1alpha1.MongoConnectionConfig{}}},
		"DynamoDB":   {Config: &mgmtv1alpha1.ConnectionConfig_DynamodbConfig{DynamodbConfig: &mgmtv1alpha1.DynamoDBConnectionConfig{}}},
	} {
		t.Run(name, func(t *testing.T) {
			// No connector: the refusal comes before anything is opened.
			svc := &Service{}
			_, err := svc.checkConnection(context.Background(), &mgmtv1alpha1.CheckConnectionConfigRequest{
				ConnectionConfig: config,
				Scope:            scope,
			}, "store")
			require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		})
	}
}
