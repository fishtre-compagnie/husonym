package consistencykey

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const anAccountId = "d5ef8fc7-4b2e-4f1f-8c9c-2a2a2a2a2a2a"

func TestTheAccountsOwnKeyWinsOverTheVariable(t *testing.T) {
	accountKey := "the-accounts-key"
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Return(connect.NewResponse(&mgmtv1alpha1.GetAccountConsistencyKeyResponse{
			Key: &accountKey,
		}), nil)

	key, err := NewResolver(client, "the-deployments-key").ForAccount(context.Background(), anAccountId)
	require.NoError(t, err)
	require.Equal(t, "the-accounts-key", key)
}

// TestADeploymentWithItsVariableKeepsItsOutputs: the point of the cascade. An account that
// holds no setting derives from the variable, and no key is drawn behind its back — which
// would change every output it produces.
func TestADeploymentWithItsVariableKeepsItsOutputs(t *testing.T) {
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	var asked *mgmtv1alpha1.GetAccountConsistencyKeyRequest
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(*connect.Request[mgmtv1alpha1.GetAccountConsistencyKeyRequest])
			require.True(t, ok)
			asked = req.Msg
		}).
		Return(connect.NewResponse(&mgmtv1alpha1.GetAccountConsistencyKeyResponse{}), nil)

	key, err := NewResolver(client, "the-deployments-key").ForAccount(context.Background(), anAccountId)
	require.NoError(t, err)
	require.Equal(t, "the-deployments-key", key)
	require.False(t, asked.GetGenerateIfAbsent(), "nothing is drawn while the variable gives a key")
}

func TestWithoutAVariableTheAccountIsAskedForAKeyOfItsOwn(t *testing.T) {
	generated := "the-generated-key"
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	var asked *mgmtv1alpha1.GetAccountConsistencyKeyRequest
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(*connect.Request[mgmtv1alpha1.GetAccountConsistencyKeyRequest])
			require.True(t, ok)
			asked = req.Msg
		}).
		Return(connect.NewResponse(&mgmtv1alpha1.GetAccountConsistencyKeyResponse{
			Key: &generated,
		}), nil)

	key, err := NewResolver(client, "").ForAccount(context.Background(), anAccountId)
	require.NoError(t, err)
	require.Equal(t, "the-generated-key", key)
	require.True(t, asked.GetGenerateIfAbsent())
	require.Equal(t, anAccountId, asked.GetAccountId())
}

// TestAnApiWithoutAccountSettingsLeavesTheDeploymentAsItWas: no encryption password, or an
// API older than this worker. Runs keep to the variable, as they did before settings
// existed.
func TestAnApiWithoutAccountSettingsLeavesTheDeploymentAsItWas(t *testing.T) {
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Return(nil, connect.NewError(connect.CodeUnimplemented, errors.New("test: not wired")))

	key, err := NewResolver(client, "the-deployments-key").ForAccount(context.Background(), anAccountId)
	require.NoError(t, err)
	require.Equal(t, "the-deployments-key", key)
}

func TestNothingAnywhereGivesNoKeyRatherThanAnError(t *testing.T) {
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Return(nil, connect.NewError(connect.CodeUnimplemented, errors.New("test: not wired")))

	key, err := NewResolver(client, "").ForAccount(context.Background(), anAccountId)
	require.NoError(t, err)
	require.Empty(t, key)
}

// TestAnUnreachableApiFailsTheActivity: the engines derive different values from different
// keys, so one table that fell back to another key would break the foreign keys between
// the tables of the run. Failing means Temporal tries again.
func TestAnUnreachableApiFailsTheActivity(t *testing.T) {
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Return(nil, connect.NewError(connect.CodeUnavailable, errors.New("test: unreachable")))

	_, err := NewResolver(client, "the-deployments-key").ForAccount(context.Background(), anAccountId)
	require.Error(t, err)
	require.Contains(t, err.Error(), anAccountId)
}

// HasKey never draws a key: computing the plan of a run without running it leaves the
// account as it was.
func TestHasKeyDrawsNothing(t *testing.T) {
	client := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	client.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().
		Run(func(args mock.Arguments) {
			req, ok := args.Get(1).(*connect.Request[mgmtv1alpha1.GetAccountConsistencyKeyRequest])
			require.True(t, ok)
			require.False(t, req.Msg.GetGenerateIfAbsent())
		}).
		Return(connect.NewResponse(&mgmtv1alpha1.GetAccountConsistencyKeyResponse{}), nil)
	has, err := NewResolver(client, "").HasKey(context.Background(), anAccountId)
	require.NoError(t, err)
	require.True(t, has, "a run of the account would draw one")

	unimplemented := mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t)
	unimplemented.On("GetAccountConsistencyKey", mock.Anything, mock.Anything).
		Once().Return(nil, connect.NewError(connect.CodeUnimplemented, errors.New("no secret")))
	has, err = NewResolver(unimplemented, "").HasKey(context.Background(), anAccountId)
	require.NoError(t, err)
	require.False(t, has, "an API that keeps no secret gives no key")

	has, err = NewResolver(mgmtv1alpha1connect.NewMockAccountSettingServiceClient(t), "the-deployments-key").
		HasKey(context.Background(), anAccountId)
	require.NoError(t, err)
	require.True(t, has, "the variable gives one, and the API is not asked")
}
