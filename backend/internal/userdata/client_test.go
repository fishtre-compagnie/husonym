package userdata

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeUserService struct{ userId string }

func (f fakeUserService) GetUser(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetUserRequest],
) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: f.userId}), nil
}

func (fakeUserService) IsUserInAccount(
	context.Context,
	*connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{Ok: true}), nil
}

// The user built for a request carrying an account key is held to that key's scope — even when
// the RBAC, as without a license, would allow everything.
func Test_Client_GetUser_AccountKeyIsScoped(t *testing.T) {
	accountId := uuid.NewString()
	accountUuid, err := husonymdb.ToUuid(accountId)
	require.NoError(t, err)
	ctx := auth_apikey.SetTokenData(context.Background(), &auth_apikey.TokenContextData{
		ApiKeyType: apikey.AccountApiKey,
		ApiKey: &db_queries.HusonymApiAccountApiKey{
			AccountID:   accountUuid,
			Permissions: []string{"job:view"},
		},
	})

	client := NewClient(fakeUserService{userId: uuid.NewString()}, rbac.NewAllowAllClient(), nil)
	user, err := client.GetUser(ctx)
	require.NoError(t, err)

	job := NewWildcardDomainEntity(accountId)
	require.NoError(t, user.EnforceJob(ctx, job, rbac.JobAction_View))
	require.ErrorContains(t, user.EnforceJob(ctx, job, rbac.JobAction_Execute), "job:execute")
	sensitive, err := user.Connection(ctx, job, rbac.ConnectionAction_ViewSensitive)
	require.NoError(t, err)
	require.False(t, sensitive)
}
