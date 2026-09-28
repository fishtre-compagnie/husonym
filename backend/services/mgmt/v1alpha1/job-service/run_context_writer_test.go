package v1alpha1_jobservice

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type oneUser struct{}

func (oneUser) GetUser(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetUserRequest],
) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: uuid.NewString()}), nil
}

func (oneUser) IsUserInAccount(
	context.Context,
	*connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{Ok: true}), nil
}

// The context of a run is what the run executes: only the worker writes it. With its own key,
// that key alone; without one, an API key, never the session of a person; without
// authentication, anyone, since nothing is told apart.
func Test_mayWriteRunContext(t *testing.T) {
	caller := func(t *testing.T, keyType *apikey.ApiKeyType) *userdata.User {
		t.Helper()
		ctx := context.Background()
		if keyType != nil {
			ctx = auth_apikey.SetTokenData(ctx, &auth_apikey.TokenContextData{ApiKeyType: *keyType})
		}
		user, err := userdata.NewClient(oneUser{}, rbac.NewAllowAllClient(), nil).GetUser(ctx)
		require.NoError(t, err)
		return user
	}
	worker, account := apikey.WorkerApiKey, apikey.AccountApiKey

	for name, tc := range map[string]struct {
		cfg     Config
		allowed map[string]bool
	}{
		"without authentication": {
			cfg:     Config{},
			allowed: map[string]bool{"worker key": true, "account key": true, "session": true},
		},
		"authentication, no worker key": {
			cfg:     Config{IsAuthEnabled: true},
			allowed: map[string]bool{"worker key": true, "account key": true, "session": false},
		},
		"authentication and a worker key": {
			cfg:     Config{IsAuthEnabled: true, HasWorkerApiKeys: true},
			allowed: map[string]bool{"worker key": true, "account key": false, "session": false},
		},
		"cloud": {
			cfg:     Config{IsAuthEnabled: true, IsHusonymCloud: true},
			allowed: map[string]bool{"worker key": true, "account key": false, "session": false},
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &Service{cfg: &tc.cfg}
			for who, keyType := range map[string]*apikey.ApiKeyType{
				"worker key": &worker, "account key": &account, "session": nil,
			} {
				err := svc.mayWriteRunContext(caller(t, keyType))
				if tc.allowed[who] {
					require.NoError(t, err, who)
				} else {
					require.Error(t, err, who)
				}
			}
		})
	}
}
