// Package userdatatest builds users for the tests of the services, under a license of the
// test's choosing.
package userdatatest

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// NewUser is a person who is a member of every account, on an instance running under eelicense,
// whose access to the entities of an account is answered by enforcer.
func NewUser(t testing.TB, eelicense license.EEInterface, enforcer userdata.EntityEnforcer) *userdata.User {
	t.Helper()
	user, err := userdata.NewClient(Members{UserId: uuid.NewString()}, nil, eelicense).
		GetUser(context.Background())
	require.NoError(t, err)
	user.EntityEnforcer = enforcer
	return user
}

// Members answers for the user account service: the caller is UserId, and a member of every
// account.
type Members struct{ UserId string }

func (m Members) GetUser(
	context.Context, *connect.Request[mgmtv1alpha1.GetUserRequest],
) (*connect.Response[mgmtv1alpha1.GetUserResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.GetUserResponse{UserId: m.UserId}), nil
}

func (Members) IsUserInAccount(
	context.Context, *connect.Request[mgmtv1alpha1.IsUserInAccountRequest],
) (*connect.Response[mgmtv1alpha1.IsUserInAccountResponse], error) {
	return connect.NewResponse(&mgmtv1alpha1.IsUserInAccountResponse{Ok: true}), nil
}
